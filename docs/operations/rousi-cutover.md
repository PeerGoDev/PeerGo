# RousiPro 三包生产迁移

正式输入始终只有三个不可变文件：

1. `rousi_YYYYMMDDhhmmss.sql.gz`
2. `torrents.zip`
3. `uploads.zip`

缺失种子候选、preflight、acceptance 和摘要均由脚本在私有运行目录生成，不是第四个迁移
输入。头像及无种子引用的上传图片不迁移。

## 1. 切换前准备

1. 完成 RousiPro、三套 PeerGo 数据库、对象目录和入口配置备份，取得可追踪的备份引用。
2. 停止旧站注册、资料、上传、购买和后台写入；停止旧 Tracker 计费后再导出最终三包。
3. 准备空的 Core、Vault、Tracker Ledger 和临时 Rousi 源数据库。目标库可以包含当前
   PeerGo schema/default policy，但不得已有用户或种子业务数据。
4. `.env.production` 中：
   - `PEERGO_LEGACY_RESTORE_DATABASE_URL` 使用临时源库 owner；
   - `PEERGO_LEGACY_SOURCE_DATABASE_URL` 使用同一源库只读角色；
   - 三个目标 URL 互不相同；
   - 对象存储、Vault/passkey 密钥和 Tracker 签名密钥已经固定。
   在 restore 前为 owner 配置对该只读角色的默认 `SELECT` 权限，并把只读角色设为
   `default_transaction_read_only=on`；否则新恢复的表不会自动对读取角色开放，preflight 会按失败处理。
5. 保留三包原件，只把绝对路径传给脚本；脚本拒绝符号链接。

## 2. Prepare：只恢复隔离源库

```bash
make rousi-restore-production-prepare \
  CONFIRM_ROUSI_PRODUCTION=PREPARE_ROUSI_PRODUCTION \
  ROUSI_BACKUP_REFERENCE='backup-2026-08-20T2200+08' \
  ROUSI_WRITES_STOPPED_AT='2026-08-20T22:00:00+08:00' \
  ROUSI_DUMP='/opt/peergo/input/rousi_20260820220000.sql.gz' \
  ROUSI_TORRENTS='/opt/peergo/input/torrents.zip' \
  ROUSI_UPLOADS='/opt/peergo/input/uploads.zip'
```

该阶段会：

- 在宿主机和 cutover 容器内分别校验 gzip/ZIP 与 SHA-256；
- 根据 SQL dump SHA-256 生成稳定 run ID；
- 把 dump 恢复到明确的空临时源库；
- 对 SQL 引用的 `.torrent` 做物理清单检查；
- 不导入 PeerGo 用户、余额、种子或图片。

运行证据位于：

```text
/opt/peergo/cutovers/<run-id>/
```

上述是 `bootstrap-single-server.sh` 写入的默认位置；集群模式可通过
`PEERGO_PRODUCTION_CUTOVER_ROOT` 使用其它绝对路径。

若存在物理缺失，会生成 `torrent-exclusions.candidate.tsv` 与对应 `.sha256`。逐行核对
旧 SQL ID、对象键和缺失原因；脚本不会像本地演练一样自动批准。

## 3. Apply：正式导入和验收

没有缺失种子时省略最后一个确认变量。有候选时必须填写 prepare 输出的完整值：

```bash
make rousi-restore-production-apply \
  CONFIRM_ROUSI_PRODUCTION=APPLY_ROUSI_PRODUCTION \
  ROUSI_BACKUP_REFERENCE='backup-2026-08-20T2200+08' \
  ROUSI_WRITES_STOPPED_AT='2026-08-20T22:00:00+08:00' \
  ROUSI_OPERATOR_REFERENCE='change/peergo-cutover-001' \
  CONFIRM_ROUSI_MISSING_TORRENTS='APPROVE:<candidate-sha256>' \
  ROUSI_DUMP='/opt/peergo/input/rousi_20260820220000.sql.gz' \
  ROUSI_TORRENTS='/opt/peergo/input/torrents.zip' \
  ROUSI_UPLOADS='/opt/peergo/input/uploads.zip'
```

Apply 按固定顺序执行：

1. Core/Vault/Tracker Ledger migration；
2. 正式只读 preflight；
3. 用户、状态、累计签到、整数魔力值、经验、VIP/限制；
4. 勋章定义/持有/佩戴/权益与工作组；
5. 用户绑定的盒子 IP/CIDR 与不可变迁移凭证；
6. 种子元数据、原始 `.torrent`、文件树、价格和已购买权益；
7. 收藏、已领取邀请关系以及后宫/邀请奖励历史合计凭证；
8. 种子引用原图与三档 WebP；
9. 全量 read-back reconciliation；
10. 正常 1x Settlement 基线、保种组收益时间线与 Tracker allowlist 投影；
11. 三份签名 Tracker 快照；
12. acceptance 与 `ready_to_activate=true`。

迁入的首版盒子政策固定为盒子不限速、优惠与 VIP 权益正常生效；优惠结算后上传按
`0.5x` 计入、下载按 `2x` 计费。普通线路继续沿用旧站
`seedbox.non_seedbox_max_speed`（旧站的 Mbps 按原算法换算），VIP 继续豁免速度观察。免费与
VIP 免费下载结算为零后仍为零，H&R 和做种证据始终使用原始流量。盒子地址只保留在
Core/Tracker 控制面，不随 announce 事件发送给 Settlement。

阶段可按相同三包安全续跑；run ID、候选批准和 preflight 都绑定三包摘要。任何输入变化都会
创建新 run，不能混用旧证据。

查看状态：

```bash
make rousi-restore-production-status \
  ROUSI_DUMP='/opt/peergo/input/rousi_20260820220000.sql.gz' \
  ROUSI_TORRENTS='/opt/peergo/input/torrents.zip' \
  ROUSI_UPLOADS='/opt/peergo/input/uploads.zip'
```

只有运行目录中的 `ready-to-activate.env` 明确包含当前验收版本：

```text
schema=peergo.rousi-production-ready.v2
acceptance_schema=peergo.legacy-cutover-acceptance.v8
ready_to_activate=true
```

才可进入非公开启动。旧版 `ready-to-activate.env` 不包含盒子或个人状态验收，必须用当前代码续跑
同一组三包，不能作为切流依据。

### 3.1 已完成切换后的个人状态补录

如果站点在收藏/邀请迁移工具发布前已经完成同一 run，可使用限定补录入口，避免重新扫描
23 GiB 图片或重建衍生图：

```bash
make rousi-restore-production-personal-state \
  CONFIRM_ROUSI_PRODUCTION=RECONCILE_ROUSI_PERSONAL_STATE \
  ROUSI_DUMP='/opt/peergo/input/rousi_20260820220000.sql.gz' \
  ROUSI_TORRENTS='/opt/peergo/input/torrents.zip' \
  ROUSI_UPLOADS='/opt/peergo/input/uploads.zip'
```

该入口只允许已有 `reconciled` run，重哈希 SQL dump 并复用原始三包 manifest；随后连续执行
两次幂等导入、一次只读验证和完整 migration status gate。成功凭证写入
`personal-state-reconciled.env`。该流程恢复每名用户的可用邀请期初值和完整邀请码历史；只把
切换时仍有效且未领取的旧 token 以 SHA-256 摘要写入注册凭证，已领取/过期 token 与旧邀请
邮箱都不会保留。它不会生成魔力值交易：后宫及一次性邀请奖励已包含在用户期初余额中，
只保存精确历史合计，防止二次发放。

## 4. 启动、管理员和最终激活

```bash
make production-up
make production-status
make production-admin USERNAME=admin
```

直接切换且不开放临时预览入口时，先通过 loopback 管理 API 签发首版邀请注册、新人考核与
默认关闭的 H&R 基线策略（密码仅在终端隐式输入）：

```bash
make production-policy-bootstrap \
  USERNAME=admin \
  CONFIRM_PEERGO_PRODUCTION_POLICIES=APPLY_PEERGO_PRODUCTION_POLICIES
```

登录后台核对政策和邮件，并设置
`PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT` 为实际 Tracker 切换 UTC 整点。重启受影响
服务前同时确认 `PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY=45m` 与
`PEERGO_SETTLEMENT_SEEDING_EVIDENCE_MAX_INTERVAL_CREDIT=35m`，避免消费者积压时提前封账，
然后执行：

```bash
make production-activation-check
```

随后先做站务 hosts/DNS 定向验证，再在维护窗口一次切换 Web 与旧 Tracker 域名。Tracker
反向代理必须使用 `proxy_set_header X-Forwarded-For $remote_addr;` 覆盖用户输入，禁止使用会
保留用户伪造链的 `$proxy_add_x_forwarded_for`；PeerGo 只接受 bootstrap 记录的直接代理网关。

## 5. 回滚边界

出现以下任一情况立即停止扩流或整体回滚：

- 基础设施类 Tracker 错误超过 0.1% 且持续 2 分钟；
- 成功 announce p99 超过 100ms 且持续 5 分钟；
- WAL 未确认超过容量 50%，或总量超过 80%；
- JetStream、Settlement、Core 水位停止推进；
- 事件丢失、重复入账或账本不守恒；
- 同一客户端族大量 `invalid_request/client_not_allowed`；
- 迁移种子大量 `torrent_not_registered`。

回滚时先停止 PeerGo Tracker 和消费者并保存故障现场，再把两个入口整体切回旧站。不要删除
PeerGo 数据、WAL 或三个最终压缩包，也不要把 PeerGo 新账目直接反灌 RousiPro。
