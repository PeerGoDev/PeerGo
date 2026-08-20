# PeerGo 首版生产部署

本文描述单机首版应用编排。PostgreSQL、NATS JetStream、HTTPS 入口、DNS、
Prometheus/告警与备份仍由运维环境负责，不能用开发 Compose 替代。

## 1. 发布基线

生产服务器只从明确提交部署，不直接复制开发目录：

```bash
git clone --branch dev https://github.com/PeerGoDev/PeerGo.git
cd PeerGo
git rev-parse HEAD
```

记录提交 SHA，并保留上一版本的镜像、环境文件、数据库快照和入口配置。正式切流前不要
删除 RousiPro。

## 2. 外部依赖

准备以下资源：

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

## 3. 生产配置

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

构建运行镜像：

```bash
make production-build
```

这两个命令不代表可以切流。首次迁移先执行
[Rousi 三包生产迁移](./rousi-cutover.md)，取得 `ready_to_activate=true`。

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
