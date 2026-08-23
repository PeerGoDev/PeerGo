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
   `127.0.0.1:8083`。Tracker 入口必须覆盖写入单值
   `X-Forwarded-For $remote_addr`，不能沿用客户端传入的链；不得转发 `9093` 指标端口。
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
脚本还会把该网络的精确 bridge gateway `/32`（启用 IPv6 时另含 `/128`）写入
`PEERGO_TRACKER_TRUSTED_PROXY_CIDRS`。不要把它扩大成整个 Docker 私网。

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
- Tracker HTTPS 入口的精确代理 CIDR；只有入口覆盖写入的单值 `X-Forwarded-For` 才会被采信；
- Tracker Ed25519 签名私钥、key ID 与对应 trusted public key；
- NATS URLs、根 CA 和 `/run/secrets` 下的各身份凭据；
- SMTP/Relay；
- `PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT`：正式 Tracker 记账切换所在的 UTC
  整点，不能填写旧站历史时间。
- `PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY=45m` 与
  `PEERGO_SETTLEMENT_SEEDING_EVIDENCE_MAX_INTERVAL_CREDIT=35m`：前者等待有序
  announce 水位越过小时边界，后者拒绝把超过 Tracker peer 生命周期的断档推定为连续做种；
  closure 必须大于等于 credit。
- `PEERGO_TRACKER_ANNOUNCE_PRODUCER_ID=tracker-primary`：单个逻辑 Tracker 所有者的
  稳定小写标识；每次进程启动会另建 UUIDv7 epoch，不能把随机容器 ID 写进该值。
- Settlement 存储保留默认值：终态 outbox/work `72h`、过期 session `48h`、raw/流量/
  做种 source 明细 `720h`、异常与违规速度证据 `4320h`；`production-ready` 会拒绝低于
  数据库保护下限或高到重新造成无界增长的配置。

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
[Rousi 三包生产迁移](./rousi-cutover.md)，取得当前 v2 `ready_to_activate=true`；验收同时要求
旧站用户绑定盒子规则已经迁入，并由签名 Tracker 运行策略证明盒子不限速、上传 `0.5x`、
下载 `2x`，且优惠和 VIP 权益先于盒子倍率结算；普通线路速度阈值继续沿用旧站设置。

三包迁移的 WebP 阶段默认使用 4 个协作处理器，并按 CPU 数量分配 libvips 线程；至少
8 核且内存、磁盘吞吐充足的单机可在 `.env.production` 设置
`PEERGO_IMAGE_DERIVATIVE_CONCURRENCY=8`。支持范围为 1–16，调高不会并行化需要保持顺序的
数据库账本与验收阶段。

策略结算默认在一个 `settlement-policy-worker` 进程内使用 4 条固定执行通道；每条原始区间
仍通过 PostgreSQL 租约和 `FOR UPDATE SKIP LOCKED` 唯一领取，不会通过增加容器复制账目。
上线追赶积压时可把 `PEERGO_SETTLEMENT_POLICY_CONCURRENCY` 调为 4–16，支持范围为 1–32；
16 通道只适用于至少 16 个逻辑 CPU 且 PostgreSQL 有明确余量的单机，
每次调整后只重建该 worker，并同时观察待结算数量、数据库连接与不可变账本一致性，不能靠
清队列、跳过事件或手工补余额来缩短水位。

Tracker 原始流量入口使用单个有序 Settlement 消费实例；`PEERGO_SETTLEMENT_BATCH_SIZE=64`
表示最多 64 条连续 stream sequence 在一个 PostgreSQL 事务内顺序落账，不代表 64 个并行
counter 处理器。变更该值时先停止 `settlement-ingest`，再运行 `tracker-migrate` 和
`settlement-announce-consumer-init` 原位同步 durable 的 `MaxAckPending/MaxRequestBatch`，最后只
重建 `settlement-ingest`。Provisioner 只允许这两个批量上限改变，过滤条件、起点、ACK 策略
或重试语义发生漂移仍会拒绝启动。
进程重启时若 durable 仍有未确认消息，runtime 会先等待一个现有 consumer `AckWait` 窗口，
让旧连接遗留的投递重新进入最早序列，再开始抓取新批次；这不会重建 consumer、跳过事件或
放宽 PostgreSQL 的连续序列不变量。

旧 v1 H&R 曾为每个普通 announce 区间创建工作行；新路由只保留完成评估、待评估完成后的
短暂竞态行和已有义务的后续区间。升级后保持 `settlement-hnr-worker` 停止，执行下面的可续跑
清理，再恢复 worker；命令只把能够证明不存在义务的旧工作标记为终态，不删除原始证据：

```bash
make production-hnr-work-reconcile \
  CONFIRM_PEERGO_HNR_RECONCILE=RECONCILE_IRRELEVANT_HNR_WORK
```

`settlement-storage-maintenance` 随生产 Compose 常驻，默认每 15 秒按 retention 索引、每类最多
处理 10,000 行，不运行全表 `DELETE`。升级后它会渐进清理历史 v1 payload、已发布 outbox、终态
work、冗余 swarm snapshot entries/chunks/inbox/headers 和已经汇总的 30 天前流量明细；小时
证据引用的 snapshot header 与每个路由 epoch 的最新水位保留。未发布事件、未结算 work、
做种证据尚未闭合的 raw interval、活跃 H&R 需要的区间以及永久 UTC 日汇总均不会删除。有异常
待核对的做种窗口会把 source 和选中快照明细随异常保留 180 天，补偿工具不会读到残缺依据。
上线前保留数据库快照；上线后同时观察该进程日志、`pg_stat_user_tables.n_dead_tup`、autovacuum
时间和 Tracker Ledger 卷使用量。普通 `VACUUM` 可按数据库运维策略执行，禁止在线运行会长时
锁表并复制整表的 `VACUUM FULL`。DELETE 后物理文件不会立即缩小；目标是 autovacuum 回收并
复用页，使磁盘在保留窗口高水位附近稳定，而不是持续线性增长。
快照 entry 是最高速表，容量规划须满足 `batch_size / cleanup_interval >= 活跃 swarm 数 /
snapshot_interval`；默认 `10000 / 15s` 可跟上约 20,000 个活跃 swarm 的 30 秒完整快照，超过时
进程会持续输出 `batch saturated` 警告，并在每个仍然受 10,000 行上限保护的事务后以 1 秒
间隔继续追赶，清空积压后自动恢复配置的稳态间隔。若长期无法退出饱和状态，应先降低快照
频率或扩展专用 Ledger，而不是无限放大单次删除事务。

流量 outbox 与 Core 流量投影默认也各自在单进程内使用 4 条固定通道。对应参数为
`PEERGO_SETTLEMENT_TRAFFIC_OUTBOX_CONCURRENCY` 和 `PEERGO_CORE_TRAFFIC_CONCURRENCY`，支持范围
均为 1–32。Core durable 只允许把 `MaxAckPending` 原位同步为后一参数；不会删除或重建
消费者，其他配置漂移仍会阻止启动。调高后必须同时确认策略积压、outbox 积压和 Core
投影水位都在下降，并复核事件计数与流量账本不变量。

三个值一起变更后，不要直接只重建 projector。使用下面的受控命令：它先停止 Core
projector，再由独立 provisioner 原位更新 durable 的 `MaxAckPending`，最后重建三段 worker；
消息游标和已确认位置均会保留。

```bash
make production-traffic-pipeline-reconfigure
```

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

若真实运行数据证明默认请求预算阻断大量正常保种客户端，可由主机操作员克隆最新不可变
Tracker 政策并只调整两级请求预算。该命令保留 announce、客户端、盒子规则和流量倍率，
仍校验指定管理员的有效授权并写入审计记录；重复执行相同目标不会新增版本。

```bash
make production-tracker-rate-policy \
  USERNAME=admin \
  REASON='依据线上限流样本恢复正常保种重连' \
  CONFIRM_PEERGO_TRACKER_RATE_POLICY=APPLY_TRACKER_RATE_POLICY
```

默认目标为每用户 `600/分钟、突发 1200`，每来源地址 `5000/分钟、突发 10000`。签发后由
现有快照发布器生成签名版本，Tracker 热加载完成后应在后台核对“已配置版本”和“实际加载
版本”一致。

不要只根据总 `rate_limited` 比例猜测该放宽哪一级。Tracker 另提供不含用户、地址或种子
标识的低基数指标 `peergo_tracker_rate_limited_total`，用下面的 PromQL 分开观察用户预算和
共享出口地址预算；只有连续窗口明确集中于同一 `scope` 时，才审阅对应参数：

```promql
sum by (scope, address_family) (
  rate(peergo_tracker_rate_limited_total{action="announce"}[5m])
)
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

### 历史做种奖励补偿预演

早期 `seeding.evidence.v1` 窗口若在 Settlement 追赶 Tracker 流时过早关闭，会留下
`late_announce_interval`，但不可回写既有证据或余额。修复遵循参考 Tracker 的三条边界：

- IP 及 IPv4/IPv6 只用于来源限流，不进入用户/种子/客户端会话的计费身份；
- 同一用户与种子的多客户端重叠区间先求并集，不能叠加做种时长；
- 相邻 announce 超过 35 分钟不视为连续做种，且预演必须固定无缺口的终态流序列水位。

先生成只读审批材料：

```bash
make production-seeding-reward-compensation-preview
```

命令在 Core 与 Tracker 上分别使用 `REPEATABLE READ READ ONLY`，复用历史时刻的不可变
奖励政策、VIP/勋章/等级权益、种子元数据和 swarm 快照，再调用正式结算使用的同一计算器。
它保留 v1 关闭时已经采用的全部来源区间，只并入关闭水位之后到达且不超过 35 分钟的区间；
不会拿当前 v2 规则重解释或追回已经结算的历史奖励。
它只写 `/opt/peergo/compensations/` 下权限为 `0600` 的 JSONL，不修改证据、魔力值、经验或
outbox。JSONL 含内部用户标识，不得复制到工单、聊天或公开日志；日常输出只保留聚合数量、
总差额、文件路径与 SHA-256。

预演完成后应先保存并人工核对文件 SHA-256、正向差额总量、最大单用户小时差额及受影响
窗口。确认后，把终端实际打印的值逐字填入（示例值不可直接使用）：

```bash
make production-seeding-reward-compensation-apply \
  ARTIFACT='/opt/peergo/compensations/seeding-reward-compensation-preview-YYYYMMDDTHHMMSSZ.jsonl' \
  APPROVE_SHA256='<64 位小写 SHA-256>' \
  OPERATOR_REFERENCE='incident:late-evidence-20260821' \
  CONFIRM_PEERGO_SEEDING_REWARD_COMPENSATION='APPLY:<同一个 SHA-256>'
```

应用器只接受补偿目录内权限严格为 `0600` 的普通文件，并在任何写操作前重新解析及哈希
全部 JSONL。每条正向差额通过正式 economy/progression 写入口，在同一 Core 事务中追加
平衡魔力值交易、经验条目和补偿收据；不覆盖原证据、原计算或旧账。制品审批、逐条收据和
完成证明均不可变，稳定 `source_reference` 使中断后的重跑只跳过已经入账的记录。处理采用
小批次事务，避免长时间占用做种铸币账户；日志只输出聚合进度，不输出用户标识。

若未得到明确的 SHA-256 审批，不得运行应用命令，也不得用 SQL 直接修改余额。
