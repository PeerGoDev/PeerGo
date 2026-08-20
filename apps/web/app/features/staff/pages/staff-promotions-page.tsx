import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  BadgePercentIcon,
  CalendarClockIcon,
  CircleAlertIcon,
  CoinsIcon,
  PencilIcon,
  PinIcon,
  PlusIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import {
  promotionCampaignListQueryOptions,
  promotionProductOrdersQueryOptions,
  promotionProductPolicyQueryOptions,
  type PromotionCampaign,
  type PromotionProductOrderPage,
  type PromotionProductPolicy,
} from "~/features/staff/api/promotion-administration.queries"
import { PromotionCampaignDialog } from "~/features/staff/components/promotion-campaign-dialog"
import { PromotionProductPolicyDialog } from "~/features/staff/components/promotion-product-policy-dialog"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function StaffPromotionsPage() {
  return (
    <StaffAccessGate
      requiredAction="promotion.manage.read"
      pageHeader={{
        title: "优惠规则",
        description: "管理运营活动、成员付费优惠和列表置顶。",
      }}
    >
      {({ session, capabilities }) => (
        <PromotionWorkbench
          csrfToken={session.csrf_token}
          canSchedule={hasCapability(capabilities, "promotion.schedule")}
        />
      )}
    </StaffAccessGate>
  )
}

function PromotionWorkbench({
  csrfToken,
  canSchedule,
}: {
  csrfToken: string
  canSchedule: boolean
}) {
  const campaigns = useQuery(promotionCampaignListQueryOptions())
  const productPolicy = useQuery(promotionProductPolicyQueryOptions())
  const productOrders = useQuery(promotionProductOrdersQueryOptions("", 10, 0))
  const [dialogOpen, setDialogOpen] = React.useState(false)
  const [policyDialogOpen, setPolicyDialogOpen] = React.useState(false)
  const [successMessage, setSuccessMessage] = React.useState("")

  if (campaigns.isPending) return <PromotionSkeleton />
  if (campaigns.isError || !campaigns.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>优惠政策暂时无法读取</AlertTitle>
          <AlertDescription>
            请检查后台登录状态与 Core 服务后重试。
          </AlertDescription>
        </Alert>
        <Button
          variant="outline"
          className="w-fit"
          onClick={() => void campaigns.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </StaffPageFrame>
    )
  }

  const data = campaigns.data
  const active = data.items.filter(
    (item) => item.timeline_state === "active"
  ).length
  const scheduled = data.items.filter(
    (item) => item.timeline_state === "scheduled"
  ).length
  const awaitingDelivery = data.items.filter(
    (item) => item.delivery_state !== "delivered"
  ).length

  return (
    <StaffPageFrame className="gap-4">
      <header>
        <h1 className="font-heading text-xl font-semibold">优惠规则</h1>
        <p className="text-sm text-muted-foreground">
          管理全站和单种子倍率、成员付费价格与置顶，并检查规则是否已经同步。
        </p>
      </header>

      {successMessage ? (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>优惠政策已保存</AlertTitle>
          <AlertDescription>{successMessage}</AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-3">
        <SummaryCard
          title="当前生效"
          value={active}
          description="以 Core 当前时刻计算"
        />
        <SummaryCard
          title="即将开始"
          value={scheduled}
          description="已签发的未来时间段"
        />
        <SummaryCard
          title="等待同步"
          value={awaitingDelivery}
          description="尚未获得结算服务确认"
          attention={awaitingDelivery > 0}
        />
      </div>

      <Alert>
        <CalendarClockIcon />
        <AlertTitle>按 announce 所属时刻结算</AlertTitle>
        <AlertDescription>
          活动时间线不可编辑；跨越开始或结束边界的流量会按持续时间拆分。后台全站活动会覆盖后台单种子活动，但会避开成员已经付费的完整优惠时段。
        </AlertDescription>
      </Alert>

      {!canSchedule ? (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>当前权限仅可查看</AlertTitle>
          <AlertDescription>
            需要 promotion.schedule 权限才能签发新优惠。
          </AlertDescription>
        </Alert>
      ) : null}

      <ProductPolicyCard
        policy={productPolicy.data}
        pending={productPolicy.isPending}
        error={productPolicy.isError}
        canEdit={canSchedule}
        onEdit={() => setPolicyDialogOpen(true)}
        onRetry={() => void productPolicy.refetch()}
      />

      <ProductOrdersCard
        page={productOrders.data}
        pending={productOrders.isPending}
        error={productOrders.isError}
        onRetry={() => void productOrders.refetch()}
      />

      <Card className="gap-0 py-0">
        <CardHeader className="flex-row items-start justify-between gap-4 p-6 pb-4">
          <div>
            <CardTitle className="flex items-center gap-2 text-xl">
              <BadgePercentIcon className="size-5" />
              优惠政策时间线
            </CardTitle>
            <CardDescription className="mt-1">
              共 {data.total.toLocaleString("zh-CN")} 条不可变记录
            </CardDescription>
          </div>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="icon-sm"
              aria-label="刷新优惠政策"
              disabled={campaigns.isFetching}
              onClick={() => void campaigns.refetch()}
            >
              <RefreshCwIcon
                className={campaigns.isFetching ? "animate-spin" : undefined}
              />
            </Button>
            {canSchedule ? (
              <Button size="sm" onClick={() => setDialogOpen(true)}>
                <PlusIcon data-icon="inline-start" />
                签发优惠
              </Button>
            ) : null}
          </div>
        </CardHeader>
        <CardContent className="p-6 pt-0">
          <PromotionTable campaigns={data.items} />
        </CardContent>
      </Card>

      <PromotionCampaignDialog
        open={dialogOpen}
        csrfToken={csrfToken}
        onOpenChange={setDialogOpen}
        onCreated={(message) => {
          setSuccessMessage(message)
          void campaigns.refetch()
        }}
      />

      {productPolicy.data ? (
        <PromotionProductPolicyDialog
          open={policyDialogOpen}
          onOpenChange={setPolicyDialogOpen}
          policy={productPolicy.data}
          csrfToken={csrfToken}
          onUpdated={(message) => {
            setSuccessMessage(message)
            void productPolicy.refetch()
          }}
        />
      ) : null}
    </StaffPageFrame>
  )
}

function SummaryCard({
  title,
  value,
  description,
  attention = false,
}: {
  title: string
  value: number
  description: string
  attention?: boolean
}) {
  return (
    <Card className="gap-1 py-4">
      <CardHeader className="px-5 pb-0">
        <CardDescription>{title}</CardDescription>
        <CardTitle
          className={
            attention ? "text-xl text-amber-700" : "text-xl text-primary"
          }
        >
          {value.toLocaleString("zh-CN")}
        </CardTitle>
      </CardHeader>
      <CardContent className="px-5 text-xs text-muted-foreground">
        {description}
      </CardContent>
    </Card>
  )
}

function ProductPolicyCard({
  policy,
  pending,
  error,
  canEdit,
  onEdit,
  onRetry,
}: {
  policy: PromotionProductPolicy | undefined
  pending: boolean
  error: boolean
  canEdit: boolean
  onEdit: () => void
  onRetry: () => void
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="flex-row items-start justify-between gap-4 p-6 pb-4">
        <div>
          <CardTitle className="flex items-center gap-2 text-xl">
            <CoinsIcon className="size-5" />
            用户付费规则
          </CardTitle>
          <CardDescription className="mt-1">
            按天使用整数魔力值购买优惠或列表置顶
          </CardDescription>
        </div>
        {canEdit && policy ? (
          <Button variant="outline" size="sm" onClick={onEdit}>
            <PencilIcon data-icon="inline-start" />
            编辑价格
          </Button>
        ) : null}
      </CardHeader>
      <CardContent className="p-6 pt-0">
        {pending ? (
          <div className="grid gap-3 sm:grid-cols-4">
            {Array.from({ length: 4 }).map((_, index) => (
              <Skeleton key={index} className="h-20" />
            ))}
          </div>
        ) : error || !policy ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>用户付费规则暂时无法读取</AlertTitle>
            <AlertDescription>
              <Button variant="link" className="h-auto p-0" onClick={onRetry}>
                重新读取
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <PolicyValue
              title="免费种子"
              value={`${formatInteger(policy.free_price_per_day)} / 天`}
              enabled={policy.promotion_enabled}
            />
            <PolicyValue
              title="2× 上传 / 免费"
              value={`${formatInteger(policy.double_upload_free_price_per_day)} / 天`}
              enabled={policy.promotion_enabled}
            />
            <PolicyValue
              title="50% 下载"
              value={`${formatInteger(policy.half_download_price_per_day)} / 天`}
              enabled={policy.promotion_enabled}
            />
            <PolicyValue
              title="列表置顶"
              value={`${formatInteger(policy.sticky_price_per_day)} / 天`}
              enabled={policy.sticky_enabled}
            />
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function PolicyValue({
  title,
  value,
  enabled,
}: {
  title: string
  value: string
  enabled: boolean
}) {
  return (
    <div className="rounded-lg border bg-muted/20 px-4 py-3">
      <div className="flex items-center justify-between gap-2 text-sm text-muted-foreground">
        <span>{title}</span>
        <Badge variant={enabled ? "secondary" : "outline"}>
          {enabled ? "开放" : "关闭"}
        </Badge>
      </div>
      <div className="mt-2 font-semibold tabular-nums">
        {enabled ? value : "暂停购买"}
      </div>
    </div>
  )
}

function ProductOrdersCard({
  page,
  pending,
  error,
  onRetry,
}: {
  page: PromotionProductOrderPage | undefined
  pending: boolean
  error: boolean
  onRetry: () => void
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle className="flex items-center gap-2 text-xl">
          <PinIcon className="size-5" />
          最近付费订单
        </CardTitle>
        <CardDescription>
          促销与置顶组合下单会一次性扣款并原子生效
        </CardDescription>
      </CardHeader>
      <CardContent className="p-6 pt-0">
        {pending ? (
          <Skeleton className="h-40" />
        ) : error || !page ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>付费订单暂时无法读取</AlertTitle>
            <AlertDescription>
              <Button variant="link" className="h-auto p-0" onClick={onRetry}>
                重新读取
              </Button>
            </AlertDescription>
          </Alert>
        ) : page.items.length === 0 ? (
          <div className="rounded-lg border border-dashed py-10 text-center text-sm text-muted-foreground">
            还没有用户购买优惠或置顶
          </div>
        ) : (
          <div className="overflow-x-auto rounded-lg border">
            <Table className="min-w-[820px]">
              <TableHeader>
                <TableRow>
                  <TableHead>用户</TableHead>
                  <TableHead>种子</TableHead>
                  <TableHead>购买内容</TableHead>
                  <TableHead className="text-right">支付魔力值</TableHead>
                  <TableHead>购买时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {page.items.map((order) => (
                  <TableRow key={order.id}>
                    <TableCell>
                      <div className="font-medium">{order.buyer_username}</div>
                      <div className="text-xs text-muted-foreground">
                        UID {order.buyer_numeric_id}
                      </div>
                    </TableCell>
                    <TableCell className="max-w-80">
                      <div className="truncate font-medium">
                        #{order.torrent_id} {order.torrent_title}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {order.promotion ? (
                          <Badge variant="secondary">
                            {promotionLabel(order.promotion)} ·{" "}
                            {order.promotion_days} 天
                          </Badge>
                        ) : null}
                        {order.sticky_days ? (
                          <Badge variant="outline">
                            置顶 · {order.sticky_days} 天
                          </Badge>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className="text-right font-medium tabular-nums">
                      {formatInteger(order.total_price)}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {formatCompactDateTime(order.purchased_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function PromotionTable({ campaigns }: { campaigns: PromotionCampaign[] }) {
  if (campaigns.length === 0) {
    return (
      <div className="rounded-lg border border-dashed py-14 text-center">
        <BadgePercentIcon className="mx-auto mb-3 size-7 text-muted-foreground" />
        <p className="font-medium">还没有优惠政策</p>
        <p className="mt-1 text-sm text-muted-foreground">
          首条记录签发后会同时进入审计与 Settlement 投递队列。
        </p>
      </div>
    )
  }
  return (
    <div className="overflow-x-auto rounded-lg border">
      <Table className="min-w-[960px]">
        <TableHeader>
          <TableRow>
            <TableHead>范围</TableHead>
            <TableHead>来源</TableHead>
            <TableHead>优惠</TableHead>
            <TableHead>时间段</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>Settlement</TableHead>
            <TableHead>签发原因</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {campaigns.map((campaign) => (
            <TableRow key={campaign.id}>
              <TableCell className="max-w-[240px]">
                <div className="font-medium">
                  {campaign.scope === "global"
                    ? "全站活动"
                    : `种子 #${campaign.torrent_id}`}
                </div>
                {campaign.torrent_title ? (
                  <div className="truncate text-xs text-muted-foreground">
                    {campaign.torrent_title}
                  </div>
                ) : campaign.override_lower_scopes ? (
                  <div className="text-xs text-muted-foreground">
                    生效期间覆盖单种子优惠
                  </div>
                ) : null}
              </TableCell>
              <TableCell>
                <Badge
                  variant={
                    campaign.source === "member_purchase"
                      ? "secondary"
                      : "outline"
                  }
                >
                  {campaign.source === "member_purchase"
                    ? "用户购买"
                    : "后台签发"}
                </Badge>
              </TableCell>
              <TableCell>
                <Badge variant="outline">
                  {promotionLabel(campaign.promotion)}
                </Badge>
              </TableCell>
              <TableCell className="text-xs whitespace-nowrap">
                <div>{formatCompactDateTime(campaign.starts_at)}</div>
                <div className="mt-1 text-muted-foreground">
                  至 {formatCompactDateTime(campaign.ends_at)}
                </div>
              </TableCell>
              <TableCell>
                <TimelineBadge state={campaign.timeline_state} />
              </TableCell>
              <TableCell>
                <DeliveryBadge campaign={campaign} />
              </TableCell>
              <TableCell className="max-w-[260px]">
                <p className="line-clamp-2 text-sm">{campaign.reason}</p>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function TimelineBadge({
  state,
}: {
  state: PromotionCampaign["timeline_state"]
}) {
  if (state === "active") return <Badge>生效中</Badge>
  if (state === "scheduled") return <Badge variant="secondary">待开始</Badge>
  return <Badge variant="outline">已结束</Badge>
}

function DeliveryBadge({ campaign }: { campaign: PromotionCampaign }) {
  if (campaign.delivery_state === "delivered") {
    return <Badge variant="secondary">已入账本</Badge>
  }
  if (campaign.delivery_state === "retrying") {
    return (
      <div>
        <Badge variant="destructive">重试中</Badge>
        <div className="mt-1 text-[11px] text-muted-foreground">
          第 {campaign.delivery_attempts} 次
        </div>
      </div>
    )
  }
  return <Badge variant="outline">待投递</Badge>
}

function promotionLabel(value: PromotionCampaign["promotion"]) {
  switch (value) {
    case "free":
      return "免费"
    case "double_upload":
      return "2× 上传"
    case "double_upload_free":
      return "2× / 免费"
    case "half_download":
      return "50% 下载"
    case "double_upload_half_download":
      return "2× / 50%"
    case "thirty_percent_download":
      return "30% 下载"
  }
}

function PromotionSkeleton() {
  return (
    <StaffPageFrame className="gap-4" aria-label="正在加载优惠政策">
      <div className="grid gap-3 sm:grid-cols-3">
        {Array.from({ length: 3 }).map((_, index) => (
          <Skeleton key={index} className="h-24 rounded-xl" />
        ))}
      </div>
      <Skeleton className="h-20 rounded-xl" />
      <Skeleton className="h-[420px] rounded-xl" />
    </StaffPageFrame>
  )
}
