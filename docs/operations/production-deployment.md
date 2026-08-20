# PeerGo 首版生产部署

本文提供两种明确模式：默认 `cluster` 保持 PostgreSQL/NATS 全链路 TLS 与多副本；
`single-server` 面向当前 RousiPro 主机，复用 1Panel PostgreSQL，并由 PeerGo 编排一个
不暴露宿主机端口的单节点 NATS。两种模式的公网 Web/Tracker 都必须经过 HTTPS 入口。

## 1. 发布基线

生产服务器只从明确提交部署，不直接复制开发目录：

```bash
install -d /opt/peergo
git clone --branch dev --single-branch \
  https://github.com/PeerGoDev/PeerGo.git /opt/peergo/app
cd /opt/peergo/app
git rev-parse HEAD
```

代码只放在 `/opt/peergo/app`。三包放 `/opt/peergo/input`，运行数据分别落到
`/opt/peergo/storage`、`tracker`、`audit`、`nats` 和 `cutovers`；更新代码不会覆盖数据。
记录提交 SHA，并保留上一版本镜像、环境文件、数据库快照和入口配置。正式切流前不要删除 RousiPro。

## 2. 外部依赖

`cluster` 模式准备以下资源：

1. Core、Privacy Vault、Tracker Ledger 三个独立 PostgreSQL 数据库、owner 和运行角色；
   生产连接必须使用 TLS。另建一个临时空 PostgreSQL 数据库作为 Rousi 只读源库，恢复
   owner 与迁移读取角色分离。
2. 启用 TLS 和 JetStream 的 NATS 集群。生产默认按三副本 stream/durable 验证漂移；
   各 publisher、consumer、provisioner 使用独立 `.creds`。
3. Web 与 Tracker 两个 HTTPS 入口。Web 反代宿主机 `127.0.0.1:8080`；Tracker 反代
   `127.0.0.1:8083`。不得转发 `9093` 指标端口。
4. 本地对象存储卷及其快照。首版默认
   `filesystem:/var/lib/peergo/objects`，原图、三档 WebP 和 `.torrent` 都进入该卷。
5. SMTP、SPF、DKIM、DMARC，以及仅私网可达的邮件 Relay HTTPS 入口。
6. 三库 PITR、对象卷、Tracker WAL/签名快照、Audit 日志和密钥材料的恢复演练。

当前单机首版使用 `single-server`。它仍保持三个目标 database、一个隔离源 database 和五个
独立角色，但可共用已经运行的 PostgreSQL 物理实例。NATS 使用单副本 JetStream；这是明确的
单机故障域，不伪装成高可用。内部明文只允许以下固定 Docker DNS 端点：
`postgresql:5432`、`peergo-nats:4222`、`vault-api:8081`、`audit-sink:8082`、
`settlement-control-api:8085` 和
`email-relay:8086/internal/v1/deliveries/transactional`。普通 `cluster` 配置不会获得这些例外。

## 3. 生产配置

### 3.1 当前单机主机一键准备

确认现有 PostgreSQL 容器名仍为 `1Panel-postgresql-kXaY`，然后执行：

```bash
cd /opt/peergo/app
make single-server-bootstrap
```

脚本是幂等的：创建缺失目录、随机密钥、专用 Docker 网络、数据库和角色；已有 PeerGo 数据库
owner 不符合预期时会停止，不会 drop、truncate 或重置 1Panel PostgreSQL。它把现有 PostgreSQL
额外接入 `peergo-single` 网络并仅在该网络登记别名 `postgresql`，原 `1panel-network` 不变。

非默认容器、根目录或域名通过环境变量显式传入：

```bash
PEERGO_SINGLE_SERVER_POSTGRES_CONTAINER='1Panel-postgresql-kXaY' \
PEERGO_SINGLE_SERVER_ROOT='/opt/peergo' \
PEERGO_SINGLE_SERVER_PUBLIC_ORIGIN='https://rousi.pro' \
  make single-server-bootstrap
```

旧站仍占用宿主机端口时，不要停止旧站来迁就 PeerGo。通过 bootstrap 覆盖为 PeerGo
分配并记录一组临时 loopback 端口；容器私网地址不会改变。例如 RousiPro 并行验收使用：

```bash
PEERGO_BOOTSTRAP_WEB_HOST_PORT=18080 \
PEERGO_BOOTSTRAP_VAULT_HOST_PORT=18081 \
PEERGO_BOOTSTRAP_TRACKER_HOST_PORT=18083 \
PEERGO_BOOTSTRAP_TRACKER_METRICS_HOST_PORT=19093 \
  make single-server-bootstrap
```

正式切流时让 HTTPS 入口改为反代这些端口即可，不必再把它们换回 8080/8083。脚本还会
拒绝 PeerGo 自身重复或越界的宿主机端口。单机 NATS 的文件上限为 200 GB；bootstrap
把低流量 H&R stream 限为 10 GiB，`production-ready` 会在启动前校验全部 stream 的
最大预留合计至少保留 10 GB 余量。集群配置仍使用 `.env.example` 中的独立容量规划。

可在首次执行时同时提供 SMTP；未提供则 `.env.production` 明确保留 `CHANGE_ME`，
`make production-up` 会拒绝启动：

```bash
PEERGO_BOOTSTRAP_SMTP_HOST='smtp.example.net' \
PEERGO_BOOTSTRAP_SMTP_USERNAME='account' \
PEERGO_BOOTSTRAP_SMTP_PASSWORD='app-password' \
PEERGO_BOOTSTRAP_SMTP_FROM_ADDRESS='noreply@rousi.pro' \
  make single-server-bootstrap
```

脚本生成 `/opt/peergo/app/.env.production`（mode `0600`）和
`/opt/peergo/secrets/peergo-single-server-nats.creds`，不会把密钥提交 Git。重复执行会复用已有
数据库密码、应用密钥、Tracker 签名密钥、NATS 密码和已经填写的 SMTP 配置。

### 3.2 集群模式手工准备

```bash
cp .env.example .env.production
chmod 600 .env.production
```

至少替换以下配置，不得保留示例域名或开发密钥：

- 三个目标数据库 URL，以及临时源库的 restore/read-only URL；
- Vault、Tracker passkey、session、WebAuthn、审计和服务间令牌；
- Web/Tracker canonical HTTPS 域名和可信 Origin；
- Tracker Ed25519 签名私钥、key ID 与对应 trusted public key；
- NATS URLs、根 CA 和 `/run/secrets` 下的各身份凭据；
- SMTP/Relay；
- `PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT`：正式 Tracker 记账切换所在的 UTC
  整点，不能填写旧站历史时间。

`PEERGO_SECRET_DIR` 必须是宿主机绝对路径，目录和文件只允许运维账户读取。

只检查变量展开和 Compose 结构：

```bash
make production-config
```

这一步允许 SMTP 和 Tracker 切换时刻尚未填写，便于先构建和执行迁移。真正启动前
`make production-up` 还会运行 readiness 检查，要求真实 SMTP 和精确的
`PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT=YYYY-MM-DDTHH:00:00Z`。

构建运行镜像：

```bash
make production-build
```

这两个命令不代表可以切流。首次迁移先执行
[Rousi 三包生产迁移](./rousi-cutover.md)，取得 `ready_to_activate=true`。

三包迁移的 WebP 阶段默认使用 4 个协作处理器，并按 CPU 数量分配 libvips 线程；至少
8 核且内存、磁盘吞吐充足的单机可在 `.env.production` 设置
`PEERGO_IMAGE_DERIVATIVE_CONCURRENCY=8`。支持范围为 1–16，调高不会并行化需要保持顺序的
数据库账本与验收阶段。

## 4. 非公开启动

迁移验收通过后启动全套服务；此时 Web/Tracker 仍只绑定宿主机 loopback，公网入口尚未切换：

```bash
make production-up
make production-status
```

把一个已经迁移且能正常登录的账号设为首位管理员：

```bash
make production-admin USERNAME=admin
```

管理员身份不会根据旧等级、用户 ID 或用户名自动继承。撤销命令为：

```bash
make production-admin-revoke USERNAME=admin
```

如果不建立临时公网预览入口，可通过 loopback Web API 签发上线所需的首版策略：

```bash
make production-policy-bootstrap \
  USERNAME=admin \
  CONFIRM_PEERGO_PRODUCTION_POLICIES=APPLY_PEERGO_PRODUCTION_POLICIES
```

命令只接受 `single-server` 的 loopback Web 监听器，并在终端隐式读取管理员密码；密码、
会话 cookie 与 CSRF token 均不会写入参数、环境或磁盘。它沿用当前注册设置，仅把模式改为
`invite`；成员自行签发邀请是否开放保持原值。若时间线上尚无对应版本，则签发：

- 新人考核：30 天内累计 50 GiB 计费上传并累计 72 小时有效做种；
- H&R：签发一条参数归零的 `disabled` 关闭基线，上线后只能由管理员审阅真实 Tracker
  数据并显式签发启用版本。

两个不可变策略至少提前 5 分钟签发，命令会等待其生效并确认 H&R 已投递。迁移账号不会被
追溯纳入新人考核；只有策略生效后完成注册的新账号会创建考核记录。命令可安全重跑，已有
启用中或待生效版本不会被默认值覆盖。激活检查要求 H&R 时间线存在，不要求 H&R 开启。

随后在后台核对注册模式、新人考核、分类、Tracker 客户端规则、上传限制、优惠、H&R、
分享率、做种奖励、经验等级、邮件和站点展示。向真实外部邮箱发送测试邮件并确认收到。

最后运行只读激活检查：

```bash
make production-activation-check
```

任何失败都应修复配置或政策，不能绕过。

## 5. 入口与真实客户端验收

先使用运维 hosts/DNS 定向验证新 Web 和旧 Tracker 域名，不开放普通用户。至少覆盖：

- 迁移账号登录、图片、种子详情和 `.torrent` 下载；
- qBittorrent、Transmission 和 libtorrent；
- 历史 `/tracker/{passkey}/announce|scrape`；
- `started`、regular、`completed`、`stopped`、IPv4/IPv6；
- Web 可用性不依赖 Tracker 热路径。

低峰维护窗口内一次切换旧 Tracker 域名到 PeerGo。当前没有两套账本增量桥接，不能让
RousiPro 与 PeerGo 长时间同时计费。

## 6. 运行与回滚

上线首日持续观察 Tracker 结果码/p99、WAL 未确认比例、JetStream consumer lag、
Settlement/Core 水位、账本守恒、数据库与对象卷容量。前 24 小时不改全站优惠、H&R、
客户端限制或结算策略。

停止应用但保留卷：

```bash
make production-down
```

不得使用 `docker compose down -v`。发生计费、事件完整性或广泛客户端兼容故障时，先停止
PeerGo Tracker 与消费者并保存 WAL/JetStream/三库现场，再把 Web 和 Tracker 路由整体
切回保留的 RousiPro；禁止两侧继续同时计费。
