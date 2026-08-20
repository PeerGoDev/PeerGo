import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  HardDriveIcon,
  ImagesIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  UserRoundIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { Progress } from "~/components/ui/progress"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import {
  storageOperationsQueryOptions,
  type StorageOperationsOverview,
} from "~/features/staff/api/operations.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffSettingsValueTable } from "~/features/staff/components/staff-settings-value-table"
import { MetricCard } from "~/shared/components/metric-card"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function StaffStorageSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="operations.monitor.read"
      pageHeader={{
        title: "图片与存储",
        description: "查看当前本地或对象存储方式、上传限制及迁移状态。",
      }}
    >
      {() => <StorageSettingsContent />}
    </StaffAccessGate>
  )
}

function StorageSettingsContent() {
  const storage = useQuery(storageOperationsQueryOptions())
  if (storage.isPending) return <StorageSettingsSkeleton />
  if (storage.isError || !storage.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>图片与存储状态暂时无法读取</AlertTitle>
          <AlertDescription>
            请检查 Core 数据库与后台会话后重试。
          </AlertDescription>
        </Alert>
        <Button
          variant="outline"
          className="w-fit"
          onClick={() => void storage.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </StaffPageFrame>
    )
  }

  const { runtime, inventory, image_derivatives: derivatives } = storage.data
  const hasMigrationIssue = BigInt(inventory.failed_migration_items) > 0n
  const hasDerivativeIssue = BigInt(derivatives.dead) > 0n
  const derivativeBacklog =
    BigInt(derivatives.pending) +
    BigInt(derivatives.processing) +
    BigInt(derivatives.retrying)

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">图片与存储</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            当前生效配置和数据库对象库存；不会显示目录路径、桶名或访问凭据。
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={storage.isFetching}
          onClick={() => void storage.refetch()}
        >
          <RefreshCwIcon
            data-icon="inline-start"
            className={storage.isFetching ? "animate-spin" : undefined}
          />
          刷新
        </Button>
      </header>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <MetricCard
          title="当前存储"
          value={runtime.driver === "filesystem" ? "本地存储" : "对象存储"}
          description={runtime.backend_id}
          icon={<HardDriveIcon />}
          tone="primary"
        />
        <MetricCard
          title="种子文件"
          value={formatInteger(inventory.torrent_objects)}
          description={formatBytes(inventory.torrent_bytes)}
          icon={<HardDriveIcon />}
          tone="default"
        />
        <MetricCard
          title="种子截图"
          value={formatInteger(inventory.screenshot_objects)}
          description={formatBytes(inventory.screenshot_bytes)}
          icon={<ImagesIcon />}
          tone="default"
        />
        <MetricCard
          title="用户头像"
          value={formatInteger(inventory.avatar_objects)}
          description={formatBytes(inventory.avatar_bytes)}
          icon={<UserRoundIcon />}
          tone="default"
        />
        <MetricCard
          title="WebP 派生图"
          value={formatInteger(derivatives.ready)}
          description={`${formatInteger(derivatives.output_objects)} 个对象 · ${formatBytes(derivatives.output_bytes)}`}
          icon={<ImagesIcon />}
          tone={hasDerivativeIssue ? "warning" : "default"}
        />
      </div>

      {hasMigrationIssue ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>存在存储迁移失败项</AlertTitle>
          <AlertDescription>
            当前有 {formatInteger(inventory.failed_migration_items)}{" "}
            个对象需要核对；源文件尚未因此自动删除。
          </AlertDescription>
        </Alert>
      ) : (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>存储迁移没有失败项</AlertTitle>
          <AlertDescription>
            切换存储时会先复制、逐个回读校验，再切换读取位置；源端保留期结束后仍需显式批准清理。
          </AlertDescription>
        </Alert>
      )}

      <div className="grid gap-4 xl:grid-cols-2">
        <Card className="gap-0 py-0">
          <CardHeader className="p-6 pb-4">
            <CardTitle>上传限制</CardTitle>
            <CardDescription>后端实际执行的单文件大小上限</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <StaffSettingsValueTable
              rows={[
                {
                  label: "原始 .torrent",
                  value: formatBytes(runtime.torrent_upload_max_bytes),
                },
                {
                  label: "单张种子截图",
                  value: formatBytes(runtime.screenshot_max_bytes),
                },
                {
                  label: "用户头像",
                  value: formatBytes(runtime.avatar_max_bytes),
                },
              ]}
            />
          </CardContent>
        </Card>

        <Card className="gap-0 py-0">
          <CardHeader className="p-6 pb-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <CardTitle>存储位置与迁移</CardTitle>
                <CardDescription>已校验位置和当前迁移健康度</CardDescription>
              </div>
              <Badge variant={hasMigrationIssue ? "destructive" : "outline"}>
                {hasMigrationIssue ? "需要处理" : "正常"}
              </Badge>
            </div>
          </CardHeader>
          <CardContent className="p-0">
            <StaffSettingsValueTable
              rows={[
                {
                  label: "当前后端上的首选种子",
                  value: formatInteger(inventory.preferred_on_active_backend),
                },
                {
                  label: "其他后端的已校验副本",
                  value: formatInteger(inventory.verified_on_other_backends),
                },
                {
                  label: "进行中的迁移",
                  value: formatInteger(inventory.active_migrations),
                },
                {
                  label: "失败的迁移对象",
                  value: formatInteger(inventory.failed_migration_items),
                },
              ]}
            />
          </CardContent>
        </Card>

        <Card className="gap-0 py-0">
          <CardHeader className="p-6 pb-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <CardTitle>图片派生处理</CardTitle>
                <CardDescription>
                  原图保留，前台优先读取经过回读校验的 WebP
                </CardDescription>
              </div>
              <Badge variant={hasDerivativeIssue ? "destructive" : "outline"}>
                {hasDerivativeIssue
                  ? `${formatInteger(derivatives.dead)} 个失败`
                  : derivativeBacklog > 0n
                    ? `处理中 ${formatInteger(derivativeBacklog.toString())}`
                    : "正常"}
              </Badge>
            </div>
          </CardHeader>
          <CardContent className="p-0">
            <StaffSettingsValueTable
              rows={[
                {
                  label: "处理策略",
                  value: "三档 WebP · 缩略 / 展示 / 大图",
                },
                {
                  label: "源图片对象",
                  value: formatInteger(derivatives.source_objects),
                },
                {
                  label: "待处理 / 处理中 / 重试",
                  value: `${formatInteger(derivatives.pending)} / ${formatInteger(derivatives.processing)} / ${formatInteger(derivatives.retrying)}`,
                },
                {
                  label: "已完成 / 失败",
                  value: `${formatInteger(derivatives.ready)} / ${formatInteger(derivatives.dead)}`,
                },
                {
                  label: "最早待处理",
                  value: derivatives.oldest_pending_at
                    ? formatCompactDateTime(derivatives.oldest_pending_at)
                    : "无积压",
                },
              ]}
            />
          </CardContent>
        </Card>

        <StorageMigrationCard migrations={storage.data.migrations} />
      </div>

      <Alert variant={hasDerivativeIssue ? "destructive" : "default"}>
        {hasDerivativeIssue ? <CircleAlertIcon /> : <ShieldCheckIcon />}
        <AlertTitle>
          {hasDerivativeIssue ? "存在无法生成的派生图" : "原图与派生图相互独立"}
        </AlertTitle>
        <AlertDescription>
          新截图原图限制为 2 MiB；旧站图片按迁移专用上限导入。libvips 生成三档
          WebP 并回读校验，失败时前台自动回退原图，不会删除唯一副本。
          {derivatives.last_error_code
            ? ` 最近错误：${derivatives.last_error_code}${derivatives.last_error_at ? `（${formatCompactDateTime(derivatives.last_error_at)}）` : ""}。`
            : ""}
        </AlertDescription>
      </Alert>
    </StaffPageFrame>
  )
}

type StorageMigration = StorageOperationsOverview["migrations"][number]

const storageKindLabels: Record<string, string> = {
  torrent: ".torrent",
  torrent_screenshot: "截图原图",
  avatar: "头像",
  image_derivative: "WebP",
}

const storageStatusLabels: Record<string, string> = {
  copying: "复制校验中",
  ready_for_cutover: "等待切读",
  retaining: "源端保留期",
  cleaning: "清理中",
  completed: "已完成",
  cancelled: "已取消",
}

function StorageMigrationCard({
  migrations,
}: {
  migrations: StorageMigration[]
}) {
  return (
    <Card className="gap-0 py-0 xl:col-span-2">
      <CardHeader className="p-6 pb-4">
        <CardTitle>统一存储迁移</CardTitle>
        <CardDescription>
          四类不可变对象共用一份强类型清单；最近显示 10 次迁移
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        {migrations.length === 0 ? (
          <div className="border-t px-6 py-10 text-center text-sm text-muted-foreground">
            尚未创建统一存储迁移。当前文件仍按生效后端正常读取。
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>迁移与范围</TableHead>
                <TableHead>方向</TableHead>
                <TableHead>进度</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>保留 / 清理</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {migrations.map((migration) => {
                const total = BigInt(migration.total_items)
                const finished =
                  BigInt(migration.verified_items) +
                  BigInt(migration.deleted_items)
                const progress =
                  total === 0n ? 100 : Number((finished * 100n) / total)
                const failed = BigInt(migration.failed_items) > 0n
                return (
                  <TableRow key={migration.id}>
                    <TableCell className="min-w-64 align-top">
                      <p className="font-mono text-xs text-muted-foreground">
                        {migration.id}
                      </p>
                      <div className="mt-2 flex flex-wrap gap-1">
                        {migration.object_kinds.map((kind) => (
                          <Badge key={kind} variant="outline">
                            {storageKindLabels[kind] ?? kind}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell className="min-w-48 align-top text-xs">
                      <p>{migration.source_backend_id}</p>
                      <p className="my-1 text-muted-foreground">↓</p>
                      <p>{migration.destination_backend_id}</p>
                    </TableCell>
                    <TableCell className="min-w-56 align-top">
                      <div className="flex items-center justify-between gap-3 text-xs">
                        <span>
                          {formatInteger(finished.toString())} /{" "}
                          {formatInteger(migration.total_items)}
                        </span>
                        <span>{progress}%</span>
                      </div>
                      <Progress value={progress} className="mt-2" />
                      <p className="mt-2 text-xs text-muted-foreground">
                        待处理 {formatInteger(migration.pending_items)}
                        {failed
                          ? ` · 失败 ${formatInteger(migration.failed_items)}`
                          : ""}
                      </p>
                    </TableCell>
                    <TableCell className="align-top">
                      <Badge variant={failed ? "destructive" : "outline"}>
                        {storageStatusLabels[migration.status] ??
                          migration.status}
                      </Badge>
                      {migration.last_error_code ? (
                        <p className="mt-2 max-w-48 font-mono text-xs break-all text-destructive">
                          {migration.last_error_code}
                        </p>
                      ) : null}
                    </TableCell>
                    <TableCell className="min-w-48 align-top text-xs text-muted-foreground">
                      {migration.retention_until ? (
                        <p>
                          保留至{" "}
                          {formatCompactDateTime(migration.retention_until)}
                        </p>
                      ) : (
                        <p>尚未切换读取位置</p>
                      )}
                      <p className="mt-1">
                        {migration.cleanup_approved_at
                          ? `已批准：${formatCompactDateTime(migration.cleanup_approved_at)}`
                          : migration.status === "retaining"
                            ? "到期后仍需管理员显式批准"
                            : "未批准清理"}
                      </p>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function StorageSettingsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载图片与存储状态">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        {Array.from({ length: 5 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-20 rounded-lg" />
      <div className="grid gap-4 xl:grid-cols-2">
        <Skeleton className="h-72 rounded-lg" />
        <Skeleton className="h-72 rounded-lg" />
      </div>
    </StaffPageFrame>
  )
}
