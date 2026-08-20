import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  EyeIcon,
  ImageIcon,
  MedalIcon,
  PencilIcon,
  PlusIcon,
  RefreshCwIcon,
  Settings2Icon,
  SparklesIcon,
  UsersRoundIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import {
  type MedalDefinition,
  medalDefinitionOverviewQueryOptions,
} from "~/features/staff/api/medal-administration.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { MedalDefinitionDialog } from "~/features/staff/components/medal-definition-dialog"
import { MedalSettingsDialog } from "~/features/staff/components/medal-settings-dialog"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"
import { requestErrorDescription } from "~/shared/api/problem"
import { MetricCard } from "~/shared/components/metric-card"
import { formatInteger } from "~/shared/formatters/integer"

type CapabilityList = components["schemas"]["CapabilityList"]

const acquisitionLabels: Record<
  components["schemas"]["MedalAcquisitionMethod"],
  string
> = {
  purchase: "魔力值购买",
  grant: "后台颁发",
  sponsor: "站点贡献",
  workgroup: "工作组",
  developer: "开发维护",
}

export function StaffMedalSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="economy.medal.manage.read"
      pageHeader={{
        title: "勋章管理",
        description: "设置勋章图片、获取方式与权益，并核对旧站持有数据。",
      }}
    >
      {({ session, capabilities }) => (
        <MedalSettingsContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function MedalSettingsContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const overview = useQuery(medalDefinitionOverviewQueryOptions())
  const [dialogOpen, setDialogOpen] = React.useState(false)
  const [settingsDialogOpen, setSettingsDialogOpen] = React.useState(false)
  const [selectedMedal, setSelectedMedal] = React.useState<
    MedalDefinition | undefined
  >()

  if (overview.isPending) return <MedalSettingsSkeleton />
  if (overview.isError || !overview.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>勋章设置暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              overview.error,
              "请确认 Core 数据库已迁移到最新版本，并检查后台会话。"
            )}
          </AlertDescription>
        </Alert>
        <Button
          variant="outline"
          className="w-fit"
          onClick={() => void overview.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </StaffPageFrame>
    )
  }

  const data = overview.data
  const visibleCount = data.items.filter((item) => item.display_on_page).length
  const imageCount = data.items.filter(
    (item) => item.image_small_path || item.image_large_path
  ).length
  const holdingCount = data.items.reduce(
    (total, item) => total + BigInt(item.holder_count),
    0n
  )
  const canCreate = hasCapability(capabilities, "economy.medal.create")
  const canUpdate = hasCapability(capabilities, "economy.medal.update")
  const canReadWorkgroups = hasCapability(capabilities, "workgroup.manage.read")

  function openCreate() {
    setSelectedMedal(undefined)
    setDialogOpen(true)
  }

  function openEdit(medal: MedalDefinition) {
    setSelectedMedal(medal)
    setDialogOpen(true)
  }

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">勋章管理</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            用户持有与佩戴数据独立保存；这里调整的是勋章本身和未来生效的权益。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={overview.isFetching}
            onClick={() => void overview.refetch()}
          >
            <RefreshCwIcon
              data-icon="inline-start"
              className={overview.isFetching ? "animate-spin" : undefined}
            />
            刷新
          </Button>
          {canCreate ? (
            <Button size="sm" onClick={openCreate}>
              <PlusIcon data-icon="inline-start" />
              新增勋章
            </Button>
          ) : null}
        </div>
      </header>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          title="勋章定义"
          value={formatInteger(data.total)}
          description="旧站定义与 PeerGo 新增"
          icon={<MedalIcon />}
          tone="primary"
        />
        <MetricCard
          title="已配置图片"
          value={formatInteger(imageCount)}
          description={"尚缺 " + formatInteger(data.items.length - imageCount)}
          icon={<ImageIcon />}
          tone={imageCount === data.items.length ? "positive" : "warning"}
        />
        <MetricCard
          title="用户持有记录"
          value={formatInteger(holdingCount)}
          description="包含有效、过期和已撤销记录"
          icon={<UsersRoundIcon />}
          tone="default"
        />
        <MetricCard
          title="前台可见"
          value={formatInteger(visibleCount)}
          description="隐藏不会删除持有记录"
          icon={<EyeIcon />}
          tone="muted"
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>全站勋章规则</CardTitle>
          <CardDescription>
            控制勋章入口、佩戴数量和多枚勋章叠加后的权益上限。
          </CardDescription>
          <CardAction>
            <div className="flex items-center gap-2">
              <Badge variant={data.settings.enabled ? "outline" : "secondary"}>
                {data.settings.enabled ? "勋章系统已启用" : "勋章系统已停用"}
              </Badge>
              {canUpdate ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setSettingsDialogOpen(true)}
                >
                  <Settings2Icon data-icon="inline-start" />
                  编辑规则
                </Button>
              ) : null}
            </div>
          </CardAction>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 text-sm sm:grid-cols-2 lg:grid-cols-4">
            <SettingValue
              label="最多佩戴"
              value={formatInteger(data.settings.maximum_wear_count) + " 枚"}
            />
            <SettingValue
              label="上传加成上限"
              value={formatPercent(data.settings.maximum_upload_bonus_bps)}
            />
            <SettingValue
              label="下载减免上限"
              value={formatPercent(data.settings.maximum_download_discount_bps)}
            />
            <SettingValue
              label="魔力加成上限"
              value={formatPercent(data.settings.maximum_magic_bonus_bps)}
            />
            <SettingValue
              label="邀请奖励上限"
              value={formatInteger(data.settings.maximum_invite_bonus)}
            />
            <SettingValue
              label="旧站条件检查日"
              value={`每月 ${formatInteger(data.settings.condition_check_day)} 日`}
            />
            <SettingValue
              label="旧站条件失效预警"
              value={`${formatInteger(data.settings.condition_warning_days)} 天`}
            />
          </div>
        </CardContent>
        <CardFooter className="text-xs text-muted-foreground">
          当前第 {data.settings.version}{" "}
          版。全站规则与单枚勋章分别修改，保存后立即生效。
        </CardFooter>
      </Card>

      <Alert>
        <CircleAlertIcon />
        <AlertTitle>旧站条件已归档，工作组目标独立管理</AlertTitle>
        <AlertDescription className="space-y-3">
          <p>
            旧站工作组勋章的条件 JSON
            仅作为迁移证据保留，不会直接触发自动撤销。PeerGo
            使用有版本、按自然月冻结的贡献目标统计与提醒；目前只观察，不会因一次任务失败误删勋章或工作组身份。
          </p>
          {canReadWorkgroups ? (
            <Link
              to="/staff/workgroups"
              className={buttonVariants({ variant: "outline", size: "sm" })}
            >
              前往工作组目标
            </Link>
          ) : null}
        </AlertDescription>
      </Alert>

      {imageCount < data.items.length ? (
        <Alert>
          <ImageIcon />
          <AlertTitle>旧站勋章图片尚未全部绑定</AlertTitle>
          <AlertDescription>
            迁移没有凭空猜测旧图片路径。你后续可逐项填写 PeerGo
            本地图片地址；用户持有关系已经保留，不受图片缺失影响。
          </AlertDescription>
        </Alert>
      ) : null}

      <Card className="gap-0 py-0">
        <CardHeader className="border-b p-5">
          <CardTitle>勋章列表</CardTitle>
          <CardDescription>
            共 {formatInteger(data.total)} 个定义，按前台显示状态和优先级排列。
          </CardDescription>
        </CardHeader>
        <CardContent className="overflow-x-auto p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="min-w-64">勋章</TableHead>
                <TableHead>获取方式</TableHead>
                <TableHead className="text-right">持有 / 有效 / 佩戴</TableHead>
                <TableHead className="text-right">权益</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.items.map((medal) => (
                <TableRow key={medal.id}>
                  <TableCell>
                    <div className="flex items-center gap-3">
                      <MedalThumbnail medal={medal} />
                      <div className="min-w-0">
                        <p className="truncate font-medium">{medal.name}</p>
                        <p className="line-clamp-1 text-xs text-muted-foreground">
                          {medal.description || "暂无说明"}
                        </p>
                        <p className="text-xs text-muted-foreground">
                          ID {medal.id} · 第 {medal.version} 版
                        </p>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {acquisitionLabels[medal.acquisition_method]}
                    </Badge>
                    {medal.acquisition_method === "purchase" ? (
                      <p className="mt-1 text-xs text-muted-foreground">
                        {formatInteger(medal.price)} 魔力值
                      </p>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(medal.holder_count)} /{" "}
                    {formatInteger(medal.active_holder_count)} /{" "}
                    {formatInteger(medal.wearing_count)}
                  </TableCell>
                  <TableCell className="text-right">
                    <MedalBenefits medal={medal} />
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      <Badge
                        variant={
                          medal.display_on_page ? "outline" : "secondary"
                        }
                      >
                        {medal.display_on_page ? "前台显示" : "已隐藏"}
                      </Badge>
                      {medal.is_workgroup ? (
                        <Badge variant="secondary">工作组</Badge>
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      aria-label={"编辑 " + medal.name}
                      disabled={!canUpdate}
                      onClick={() => openEdit(medal)}
                    >
                      <PencilIcon data-icon="icon" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <MedalDefinitionDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        medal={selectedMedal}
        csrfToken={csrfToken}
      />
      <MedalSettingsDialog
        open={settingsDialogOpen}
        onOpenChange={setSettingsDialogOpen}
        settings={data.settings}
        csrfToken={csrfToken}
      />
    </StaffPageFrame>
  )
}

function SettingValue({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border bg-muted/15 p-3">
      <p className="text-muted-foreground">{label}</p>
      <p className="mt-1 font-medium tabular-nums">{value}</p>
    </div>
  )
}

function MedalThumbnail({ medal }: { medal: MedalDefinition }) {
  const source = medal.image_small_path || medal.image_large_path
  const [failed, setFailed] = React.useState(false)
  React.useEffect(() => setFailed(false), [source])

  if (!source || failed) {
    return (
      <span className="flex size-11 shrink-0 items-center justify-center rounded-lg border bg-muted/30 text-muted-foreground">
        <MedalIcon className="size-5" />
      </span>
    )
  }
  return (
    <span className="flex size-11 shrink-0 items-center justify-center rounded-lg border bg-muted/20 p-1">
      <img
        src={source}
        alt=""
        className="max-h-full max-w-full object-contain"
        onError={() => setFailed(true)}
      />
    </span>
  )
}

function MedalBenefits({ medal }: { medal: MedalDefinition }) {
  const benefits = [
    medal.upload_bonus_bps
      ? "上传 +" + formatPercent(medal.upload_bonus_bps)
      : null,
    medal.download_discount_bps
      ? "下载 -" + formatPercent(medal.download_discount_bps)
      : null,
    medal.magic_bonus_bps
      ? "魔力 +" + formatPercent(medal.magic_bonus_bps)
      : null,
  ].filter(Boolean)
  if (!benefits.length) {
    return <span className="text-muted-foreground">—</span>
  }
  return (
    <div className="flex flex-col items-end gap-0.5 text-xs">
      {benefits.map((benefit) => (
        <span key={benefit} className="inline-flex items-center gap-1">
          <SparklesIcon className="size-3 text-primary" />
          {benefit}
        </span>
      ))}
    </div>
  )
}

function formatPercent(basisPoints: number) {
  return (
    new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 2 }).format(
      basisPoints / 100
    ) + "%"
  )
}

function MedalSettingsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载勋章设置">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-40 rounded-lg" />
      <Skeleton className="h-96 rounded-lg" />
    </StaffPageFrame>
  )
}
