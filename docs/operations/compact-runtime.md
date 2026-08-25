# PeerGo 单机紧凑运行布局

`PEERGO_RUNTIME_LAYOUT=compact` 是单机生产默认值。宿主机复用 PostgreSQL 和
OpenResty 时，PeerGo 只保留六个常驻容器：

1. `peergo-api`：Core API、Settlement Control API、Email Relay；
2. `peergo-worker`：22 个有序 worker/projector；
3. `vault-api`：隐私与凭据隔离边界；
4. `tracker`：高并发 Tracker 与 WAL 边界；
5. `peergo-nats`：JetStream；
6. `audit-sink`：只追加审计边界。

数据库迁移、stream/consumer 初始化、preflight、snapshot bootstrap 和
`web-release` 都是一次性任务，不是常驻容器。Web 构建仍使用独立镜像，但发布任务把
静态文件写入 `PEERGO_WEB_ROOT` 后立即退出；OpenResty 直接提供 SPA 和长缓存哈希资源，
只把 `/api/`、`/rss/` 转发给 `PEERGO_API_HOST_PORT`，把 `/tracker/` 转发给 Tracker。
`make production-up` 验证成功后会移除这些已完成的任务容器；数据库 migration、NATS stream
和 durable 状态不会随容器移除。

## 监管与故障语义

紧凑布局没有用 shell 或第三方 supervisor。`peergo-runtime-supervisor` 以 Go 原生方式：

- 为每个子进程创建独立进程组，转发 `SIGTERM` 并在超时后清理；
- 对异常退出执行有上限的指数退避，连续失败达到阈值时让整个容器失败并由 Docker 重启；
- 在 `:8099/livez`、`:8099/readyz` 和 `:8099/components` 暴露容器内健康状态；
- API readiness 还会实际探测 Core、Settlement Control 和 Email Relay，而不是只检查 PID；
- 保持原有二进制、数据库事务、JetStream durable 和有序消费边界不变。

这次合并减少的是容器与运维对象数量，不会假装把 22 个有独立一致性职责的进程变成一个
线程，也不会删除消费位置或合并数据库权限。后续只有在各模块导出可共享连接池的 runner
后，才应继续减少数据库连接；容器合并本身不能作为放松账本边界的理由。

## 单机配置与切换

当前 RousiPro 主机使用：

```dotenv
PEERGO_RUNTIME_LAYOUT=compact
PEERGO_API_HOST_PORT=18084
PEERGO_WEB_ROOT=/opt/1panel/www/sites/rousi.pro/index
PEERGO_DOCKER_LOG_MAX_SIZE=100m
PEERGO_DOCKER_LOG_MAX_FILES=3
```

`scripts/production-compose.sh` 根据 `PEERGO_RUNTIME_LAYOUT` 只启用一个 runtime profile，
避免紧凑和拆分布局同时运行。生产切换顺序为：

```bash
make production-ready
make production-build
./scripts/production-compose.sh up -d --wait
```

静态文件发布完成且 `peergo-api` 健康后，安装并校验
`deploy/openresty/rousi.pro.conf`，再 reload OpenResty。配置文件把 API 指向
`127.0.0.1:18084`，Tracker 继续使用 `127.0.0.1:18083`。必须先备份 1Panel 当前 vhost，
并在 reload 前于容器内执行 `openresty -t`。

验证紧凑容器后，再停止并移除旧的 `split-runtime` 容器；不要删除数据库、对象、Tracker、
NATS 或审计卷。

## 回滚

旧布局保留在 `split-runtime` profile。需要回滚时先停止紧凑的 API/worker，恢复 OpenResty
旧 vhost，把环境改为：

```dotenv
PEERGO_RUNTIME_LAYOUT=split
```

然后重新执行 `make production-up`。两个布局使用相同镜像、数据卷、数据库 schema 和 durable，
因此不需要数据回滚；严禁同时启用两个 profile，以免相同 consumer 并发争用和 DNS alias
产生歧义。
