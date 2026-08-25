import * as React from "react"
import { Link } from "react-router"
import {
  AwardIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  CircleAlertIcon,
  DownloadIcon,
  LogInIcon,
  RefreshCwIcon,
  ShoppingBagIcon,
  SparklesIcon,
  UploadIcon,
  UserPlusIcon,
  ZapIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
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
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Separator } from "~/components/ui/separator"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type MemberMedal,
  type MemberMedalOverview,
  useMemberMedals,
  usePurchaseMedal,
  useUpdateMedalPriority,
  useUpdateMedalWearing,
} from "~/features/economy/api/medals.queries"
import { cn } from "~/lib/utils"
import { requestErrorDescription } from "~/shared/api/problem"
import { PageLayout } from "~/shared/components/page-layout"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

type MedalFilter = "owned" | "wearing" | "shop"

export function MedalsPage() {
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const medals = useMemberMedals(session.data?.user.id)
  const purchase = usePurchaseMedal()
  const wearing = useUpdateMedalWearing()
  const priority = useUpdateMedalPriority()
  const [filter, setFilter] = React.useState<MedalFilter>("owned")
  const csrfToken = session.data?.csrf_token ?? ""
  const purchaseRequestIds = React.useRef(new Map<string, string>())
  const canWear = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "economy.medal.wear.self"
    )
  )
  const canPurchase = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "economy.medal.purchase.self"
    )
  )
  const mutationError =
    (purchase.isError ? purchase.error : undefined) ??
    (wearing.isError ? wearing.error : undefined) ??
    (priority.isError ? priority.error : undefined)

  if (session.isPending || (session.data && medals.isPending)) {
    return <MedalsPageSkeleton />
  }

  return (
    <PageLayout>
      {session.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>会话状态暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(session.error, "请稍后刷新页面。")}
          </AlertDescription>
        </Alert>
      ) : null}

      {!session.isError && !session.data ? (
        <Card>
          <CardHeader>
            <CardTitle>需要登录</CardTitle>
            <CardDescription>
              登录后可查看、购买和佩戴自己的勋章。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          </CardContent>
        </Card>
      ) : null}

      {session.data && medals.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>勋章暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(medals.error, "请稍后重试。")}
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void medals.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      ) : null}

      {mutationError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>勋章操作未完成</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(mutationError, "请刷新页面后重试。")}
          </AlertDescription>
        </Alert>
      ) : null}

      {session.data && medals.data ? (
        <MedalCenter
          overview={medals.data}
          filter={filter}
          onFilterChange={setFilter}
          busyMedalId={
            (wearing.isPending ? wearing.variables?.medalId : undefined) ??
            (priority.isPending ? priority.variables?.medalId : undefined)
          }
          purchasePending={purchase.isPending}
          purchaseMedalId={
            purchase.isPending ? purchase.variables?.medalId : undefined
          }
          canWear={canWear}
          canPurchase={canPurchase}
          onPurchase={async (medal) => {
            const idempotencyKey =
              purchaseRequestIds.current.get(medal.id) ?? crypto.randomUUID()
            purchaseRequestIds.current.set(medal.id, idempotencyKey)
            await purchase.mutateAsync({
              medalId: Number(medal.id),
              csrfToken,
              idempotencyKey,
            })
            purchaseRequestIds.current.delete(medal.id)
          }}
          onWearing={(medal, nextWearing) =>
            wearing.mutate({
              medalId: Number(medal.id),
              expectedVersion: medal.holding?.version ?? 0,
              wearing: nextWearing,
              csrfToken,
            })
          }
          onPriority={(medal, direction) =>
            priority.mutate({
              medalId: Number(medal.id),
              expectedVersion: medal.holding?.version ?? 0,
              direction,
              csrfToken,
            })
          }
        />
      ) : null}
    </PageLayout>
  )
}

function MedalCenter({
  overview,
  filter,
  onFilterChange,
  busyMedalId,
  purchasePending,
  purchaseMedalId,
  canWear,
  canPurchase,
  onPurchase,
  onWearing,
  onPriority,
}: {
  overview: MemberMedalOverview
  filter: MedalFilter
  onFilterChange: (filter: MedalFilter) => void
  busyMedalId?: number
  purchasePending: boolean
  purchaseMedalId?: number
  canWear: boolean
  canPurchase: boolean
  onPurchase: (medal: MemberMedal) => Promise<void>
  onWearing: (medal: MemberMedal, wearing: boolean) => void
  onPriority: (medal: MemberMedal, direction: "up" | "down") => void
}) {
  const items = filteredMedals(overview.items, filter)
  const wearingItems = filteredMedals(overview.items, "wearing")

  return (
    <>
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="flex items-center gap-2 font-heading text-3xl font-bold">
            <AwardIcon aria-hidden="true" />
            勋章中心
          </h1>
          <p className="text-sm text-muted-foreground">
            收集勋章，展示荣誉，获得加成
          </p>
        </div>
        <div className="text-right">
          <p className="text-sm text-muted-foreground">当前魔力值</p>
          <p className="flex items-center justify-end gap-2 font-heading text-2xl font-bold tabular-nums">
            <SparklesIcon className="text-primary" aria-hidden="true" />
            {formatInteger(overview.magic_balance)}
          </p>
        </div>
      </header>

      <Card className="border-primary/20 bg-linear-to-r from-primary/5 to-card">
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center gap-3">
            <ZapIcon aria-hidden="true" />
            当前勋章加成
            <Badge variant="secondary">
              {overview.wearing_count}/{overview.settings.maximum_wear_count}{" "}
              已佩戴
            </Badge>
          </CardTitle>
          <CardDescription>
            工作组勋章自动生效，不占普通佩戴名额；以下数值已经按站点上限封顶。
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <BenefitMetric
            icon={UploadIcon}
            label="上传加成"
            value={formatBPS(overview.benefits.upload_bonus_bps, "+")}
          />
          <BenefitMetric
            icon={DownloadIcon}
            label="下载折扣"
            value={formatBPS(overview.benefits.download_discount_bps, "-")}
          />
          <BenefitMetric
            icon={SparklesIcon}
            label="魔力加成"
            value={formatBPS(overview.benefits.magic_bonus_bps, "+")}
          />
          <BenefitMetric
            icon={UserPlusIcon}
            label="邀请加成"
            value={`+${overview.benefits.invite_bonus}`}
          />
        </CardContent>
      </Card>

      <section className="flex flex-col gap-4" aria-label="勋章列表">
        <div className="overflow-x-auto border-b">
          <ToggleGroup
            value={[filter]}
            onValueChange={(values) => {
              const selected = values[0] as MedalFilter | undefined
              if (selected) onFilterChange(selected)
            }}
            spacing={0}
            aria-label="切换勋章分类"
            className="min-w-max rounded-none"
          >
            <MedalTab
              value="owned"
              label="我的勋章"
              count={overview.owned_count}
            />
            <MedalTab
              value="wearing"
              label="佩戴中"
              count={overview.wearing_count}
            />
            <MedalTab
              value="shop"
              label="勋章商店"
              count={overview.shop_count}
              icon={ShoppingBagIcon}
            />
          </ToggleGroup>
        </div>

        {items.length === 0 ? (
          <Empty className="min-h-52 border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <AwardIcon />
              </EmptyMedia>
              <EmptyTitle>这里还没有勋章</EmptyTitle>
              <EmptyDescription>
                {filter === "shop"
                  ? "当前没有开放购买的勋章。"
                  : "获得勋章后会在这里展示。"}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="grid gap-4 md:grid-cols-2">
            {items.map((medal) => {
              const wearingIndex = wearingItems.findIndex(
                (item) => item.id === medal.id
              )
              return (
                <MedalCard
                  key={medal.id}
                  medal={medal}
                  shop={filter === "shop"}
                  busy={busyMedalId === Number(medal.id)}
                  purchasePending={purchasePending}
                  purchaseBusy={purchaseMedalId === Number(medal.id)}
                  canWear={canWear}
                  canPurchase={canPurchase}
                  firstWearing={wearingIndex === 0}
                  lastWearing={wearingIndex === wearingItems.length - 1}
                  onPurchase={() => {
                    void onPurchase(medal).catch(() => undefined)
                  }}
                  onWearing={(next) => onWearing(medal, next)}
                  onPriority={(direction) => onPriority(medal, direction)}
                />
              )
            })}
          </div>
        )}
      </section>

      <Card size="sm">
        <CardHeader>
          <CardTitle>加成上限说明</CardTitle>
          <CardDescription>
            多枚有效勋章先相加，再按以下站点设置封顶。
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Cap
            label="上传加成上限"
            value={formatBPS(overview.settings.maximum_upload_bonus_bps, "+")}
          />
          <Cap
            label="下载折扣上限"
            value={formatBPS(
              overview.settings.maximum_download_discount_bps,
              "-"
            )}
          />
          <Cap
            label="魔力加成上限"
            value={formatBPS(overview.settings.maximum_magic_bonus_bps, "+")}
          />
          <Cap
            label="邀请加成上限"
            value={`+${overview.settings.maximum_invite_bonus}`}
          />
        </CardContent>
      </Card>
    </>
  )
}

function BenefitMetric({
  icon: Icon,
  label,
  value,
}: {
  icon: typeof UploadIcon
  label: string
  value: string
}) {
  return (
    <dl className="flex items-center gap-3">
      <span
        className="flex size-9 items-center justify-center rounded-full bg-background text-primary shadow-xs"
        aria-hidden="true"
      >
        <Icon />
      </span>
      <div>
        <dt className="text-sm text-muted-foreground">{label}</dt>
        <dd className="text-lg font-semibold tabular-nums">{value}</dd>
      </div>
    </dl>
  )
}

function MedalTab({
  value,
  label,
  count,
  icon: Icon,
}: {
  value: MedalFilter
  label: string
  count: string
  icon?: typeof AwardIcon
}) {
  return (
    <ToggleGroupItem
      value={value}
      className="h-11 rounded-none border-0 border-b-2 border-transparent px-4 data-[state=on]:border-primary data-[state=on]:bg-transparent data-[state=on]:text-primary"
    >
      {Icon ? <Icon data-icon="inline-start" /> : null}
      {label}
      <Badge variant="secondary">{count}</Badge>
    </ToggleGroupItem>
  )
}

function MedalCard({
  medal,
  shop,
  busy,
  purchasePending,
  purchaseBusy,
  canWear,
  canPurchase,
  firstWearing,
  lastWearing,
  onPurchase,
  onWearing,
  onPriority,
}: {
  medal: MemberMedal
  shop: boolean
  busy: boolean
  purchasePending: boolean
  purchaseBusy: boolean
  canWear: boolean
  canPurchase: boolean
  firstWearing: boolean
  lastWearing: boolean
  onPurchase: () => void
  onWearing: (wearing: boolean) => void
  onPriority: (direction: "up" | "down") => void
}) {
  const holdingActive = isHoldingActive(medal)
  const isWearing =
    holdingActive && (medal.is_workgroup || medal.holding?.state === "wearing")
  const image = medal.image_large_path ?? medal.image_small_path

  return (
    <Card
      className={cn(isWearing && "border-primary/70 ring-1 ring-primary/20")}
    >
      <CardHeader className="grid grid-cols-[4rem_1fr_auto] items-start gap-4">
        <div className="row-span-2 flex size-16 items-center justify-center overflow-hidden rounded-lg bg-muted">
          {image ? (
            <img
              src={image}
              alt={medal.name}
              className="size-16 object-contain"
            />
          ) : (
            <AwardIcon
              className="size-8 text-muted-foreground"
              aria-hidden="true"
            />
          )}
        </div>
        <CardTitle>{medal.name}</CardTitle>
        <CardAction>
          <Badge
            variant={
              medal.acquisition_method === "purchase" ? "secondary" : "outline"
            }
          >
            {acquisitionLabel(medal.acquisition_method)}
          </Badge>
        </CardAction>
        <CardDescription className="line-clamp-3 min-h-10">
          {medal.description || "暂无勋章说明。"}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex min-h-5 flex-wrap gap-2">
          <BenefitBadges medal={medal} />
        </div>
        {medal.holding?.expires_at ? (
          <p className="text-xs text-muted-foreground">
            有效期至 {formatCompactDateTime(medal.holding.expires_at)}
          </p>
        ) : null}
        {shop ? (
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="font-medium tabular-nums">
              {formatInteger(medal.price)} 魔力值
              <span className="ml-2 text-xs font-normal text-muted-foreground">
                {medal.duration_days === 0
                  ? "永久"
                  : `${medal.duration_days} 天`}
              </span>
            </p>
            {medal.inventory ? (
              <span className="text-xs text-muted-foreground">
                库存 {medal.inventory}
              </span>
            ) : null}
          </div>
        ) : null}
      </CardContent>
      <CardFooter className="gap-2 bg-background">
        {shop ? (
          <Button
            className="flex-1"
            disabled={!medal.purchasable || purchasePending || !canPurchase}
            onClick={onPurchase}
          >
            {purchaseBusy ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <ShoppingBagIcon data-icon="inline-start" />
            )}
            {medal.purchasable
              ? "购买"
              : medal.purchase_unavailable_reason || "暂不可购买"}
          </Button>
        ) : medal.is_workgroup ? (
          <Button variant="outline" className="flex-1" disabled>
            工作组权益自动生效
          </Button>
        ) : (
          <>
            <Button
              variant={
                medal.holding?.state === "wearing" ? "outline" : "default"
              }
              className="flex-1"
              disabled={!holdingActive || busy || !canWear}
              onClick={() => onWearing(medal.holding?.state !== "wearing")}
            >
              {busy ? <Spinner data-icon="inline-start" /> : null}
              {!holdingActive
                ? "已过期"
                : medal.holding?.state === "wearing"
                  ? "取下"
                  : "佩戴"}
            </Button>
            {medal.holding?.state === "wearing" ? (
              <>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="上移勋章"
                  disabled={firstWearing || busy || !canWear}
                  onClick={() => onPriority("up")}
                >
                  <ChevronUpIcon />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="下移勋章"
                  disabled={lastWearing || busy || !canWear}
                  onClick={() => onPriority("down")}
                >
                  <ChevronDownIcon />
                </Button>
              </>
            ) : null}
          </>
        )}
      </CardFooter>
    </Card>
  )
}

function BenefitBadges({ medal }: { medal: MemberMedal }) {
  const benefits = [
    medal.upload_bonus_bps > 0
      ? `上传 ${formatBPS(medal.upload_bonus_bps, "+")}`
      : null,
    medal.download_discount_bps > 0
      ? `下载 ${formatBPS(medal.download_discount_bps, "-")}`
      : null,
    medal.magic_bonus_bps > 0
      ? `魔力 ${formatBPS(medal.magic_bonus_bps, "+")}`
      : null,
    BigInt(medal.invite_bonus) > 0n ? `邀请 +${medal.invite_bonus}` : null,
  ].filter((value): value is string => Boolean(value))
  if (benefits.length === 0) return <Badge variant="outline">纪念勋章</Badge>
  return benefits.map((benefit) => (
    <Badge key={benefit} variant="secondary">
      {benefit}
    </Badge>
  ))
}

function Cap({ label, value }: { label: string; value: string }) {
  return (
    <dl className="flex items-center justify-between gap-3">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="font-semibold tabular-nums">{value}</dd>
    </dl>
  )
}

function filteredMedals(items: MemberMedal[], filter: MedalFilter) {
  if (filter === "shop") {
    return items.filter((item) => item.acquisition_method === "purchase")
  }
  if (filter === "wearing") {
    return items
      .filter(
        (item) =>
          isHoldingActive(item) &&
          (item.is_workgroup || item.holding?.state === "wearing")
      )
      .sort(
        (left, right) =>
          (right.holding?.priority ?? 0) - (left.holding?.priority ?? 0)
      )
  }
  return items.filter((item) => item.holding)
}

function isHoldingActive(medal: MemberMedal) {
  if (!medal.holding) return false
  return (
    !medal.holding.expires_at ||
    Date.parse(medal.holding.expires_at) > Date.now()
  )
}

function acquisitionLabel(method: MemberMedal["acquisition_method"]) {
  return {
    purchase: "购买",
    grant: "授予",
    sponsor: "赞助",
    workgroup: "工作组",
    developer: "开发者",
  }[method]
}

function formatBPS(value: number, prefix: "+" | "-") {
  const whole = Math.trunc(value / 100)
  const fraction = value % 100
  return `${prefix}${whole}${fraction ? `.${String(fraction).padStart(2, "0").replace(/0+$/, "")}` : ""}%`
}

function MedalsPageSkeleton() {
  return (
    <PageLayout aria-label="勋章加载中">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-2">
          <Skeleton className="h-9 w-40" />
          <Skeleton className="h-4 w-56" />
        </div>
        <Skeleton className="h-12 w-44" />
      </div>
      <Skeleton className="h-36 w-full" />
      <Separator />
      <div className="grid gap-4 md:grid-cols-2">
        <Skeleton className="h-64 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    </PageLayout>
  )
}
