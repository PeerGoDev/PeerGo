import * as React from "react"
import { Link } from "react-router"
import {
  CalendarCheckIcon,
  CircleAlertIcon,
  CrownIcon,
  HourglassIcon,
  LogInIcon,
  PaletteIcon,
  RefreshCwIcon,
  ShoppingBagIcon,
  TicketIcon,
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
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  isMagicRequestUncertain,
  type MagicShopItemKey,
  type MagicShopOrder,
  type MagicShopOverview,
  useMagicShop,
  usePurchaseMagicShopItem,
  useSetNameStyle,
} from "~/features/economy/api/magic-store.queries"
import { nameStyleLabel } from "~/features/economy/components/name-style"
import { cn } from "~/lib/utils"
import { requestErrorDescription } from "~/shared/api/problem"
import { PageLayout } from "~/shared/components/page-layout"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function MagicShopPage() {
  const session = useWebSession()
  const shop = useMagicShop(session.data?.user.id)
  const purchase = usePurchaseMagicShopItem()
  const setStyle = useSetNameStyle()
  const csrfToken = session.data?.csrf_token ?? ""
  const [notice, setNotice] = React.useState<string>()
  const mutationError = purchase.error ?? setStyle.error
  const retryPurchase =
    purchase.isError && isMagicRequestUncertain(purchase.error)

  if (session.isPending || (session.data && shop.isPending)) {
    return <MagicShopSkeleton />
  }

  const buy = (input: NonNullable<typeof purchase.variables>) => {
    setNotice(undefined)
    setStyle.reset()
    purchase.mutate(input, {
      onSuccess: (receipt) => {
        setNotice(
          `已购买「${itemLabel(receipt.order.item_key)}」× ${receipt.order.quantity}，花费 ${formatInteger(receipt.order.total_price)} 魔力值，余额 ${formatInteger(receipt.balance_after)}。`
        )
      },
    })
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
              登录后可使用魔力值兑换邀请、VIP 与彩色 ID。
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

      {session.data && shop.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>魔力商店暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(shop.error, "请稍后重试。")}
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void shop.refetch()}
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
          <AlertTitle>操作未完成</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(mutationError, "请刷新页面后重试。")}
          </AlertDescription>
          {retryPurchase && session.data ? (
            <AlertAction>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  if (purchase.variables)
                    buy({ ...purchase.variables, csrfToken })
                }}
              >
                确认上次购买
              </Button>
            </AlertAction>
          ) : null}
        </Alert>
      ) : notice ? (
        <Alert>
          <ShoppingBagIcon />
          <AlertTitle>兑换成功</AlertTitle>
          <AlertDescription>{notice}</AlertDescription>
        </Alert>
      ) : null}

      {session.data && shop.data ? (
        <ShopContent
          overview={shop.data}
          username={session.data.user.username}
          busyItem={
            purchase.isPending ? purchase.variables?.itemKey : undefined
          }
          styleBusy={setStyle.isPending}
          locked={purchase.isPending || setStyle.isPending || retryPurchase}
          onBuy={(itemKey, quantity, unitPrice, styleKey) => {
            buy({
              csrfToken,
              idempotencyKey: crypto.randomUUID(),
              itemKey,
              quantity,
              unitPrice,
              styleKey,
            })
          }}
          onSwitchStyle={(styleKey) => {
            setNotice(undefined)
            purchase.reset()
            setStyle.mutate(
              { csrfToken, styleKey },
              {
                onSuccess: () =>
                  setNotice(
                    `彩色 ID 已切换为「${nameStyleLabel(styleKey)}」。`
                  ),
              }
            )
          }}
        />
      ) : null}
    </PageLayout>
  )
}

function ShopContent({
  overview,
  username,
  busyItem,
  locked,
  styleBusy,
  onBuy,
  onSwitchStyle,
}: {
  overview: MagicShopOverview
  username: string
  busyItem?: MagicShopItemKey
  locked: boolean
  styleBusy: boolean
  onBuy: (
    itemKey: MagicShopItemKey,
    quantity: number,
    unitPrice: number,
    styleKey?: string
  ) => void
  onSwitchStyle: (styleKey: string) => void
}) {
  const { settings } = overview
  const balance = BigInt(overview.magic_balance)
  const invitesRemaining = monthlyRemaining(
    overview.invites_purchased_this_month,
    settings.invite.monthly_limit
  )
  const temporaryInvitesRemaining = monthlyRemaining(
    overview.temporary_invites_purchased_this_month,
    settings.temporary_invite.monthly_limit
  )
  const cardsRemaining = Math.max(
    0,
    settings.makeup_card.maximum_holding - overview.makeup_cards
  )
  const inviteBlocked =
    overview.invite_blocker && overview.invite_blocker !== "none"

  return (
    <>
      <header className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2 pt-1">
        <div className="flex min-w-0 flex-col gap-1">
          <h1 className="font-heading text-lg leading-tight font-semibold tracking-title">
            魔力商店
          </h1>
          <p className="text-xs text-muted-foreground">
            用魔力值兑换邀请名额、VIP、彩色 ID 与补签卡
          </p>
        </div>
        <div className="flex items-baseline gap-2 rounded-lg border bg-card px-3 py-1.5 shadow-card">
          <span className="text-xs text-muted-foreground">当前魔力值</span>
          <span className="font-mono text-md font-semibold text-warning-foreground tabular-nums">
            {formatInteger(overview.magic_balance)}
          </span>
        </div>
      </header>

      {!settings.shop_enabled ? (
        <Alert>
          <CircleAlertIcon />
          <AlertTitle>商店暂未开放</AlertTitle>
          <AlertDescription>
            管理员暂时关闭了魔力商店，请稍后再来。
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-3 sm:gap-4 md:grid-cols-2">
        <ShopItemCard
          icon={TicketIcon}
          title="永久邀请名额"
          description="增加 1 个永久邀请名额，可随时生成邀请码。"
          price={settings.invite.price}
          balance={balance}
          maximum={invitesRemaining}
          enabled={settings.shop_enabled && settings.invite.enabled}
          busy={busyItem === "invite"}
          locked={locked}
          blocker={
            inviteBlocked
              ? inviteBlockerLabel(overview.invite_blocker ?? "")
              : invitesRemaining === 0
                ? "本月已达限购"
                : undefined
          }
          onBuy={(quantity) => onBuy("invite", quantity, settings.invite.price)}
          facts={[
            ["当前剩余名额", formatInteger(overview.remaining_invites)],
            [
              "本月已购",
              limitText(
                overview.invites_purchased_this_month,
                settings.invite.monthly_limit
              ),
            ],
          ]}
        />
        <ShopItemCard
          icon={HourglassIcon}
          title="临时邀请"
          description={`获得 1 个 ${settings.temporary_invite.valid_days} 天内有效的临时邀请，优先于永久名额使用，过期作废。`}
          price={settings.temporary_invite.price}
          balance={balance}
          maximum={temporaryInvitesRemaining}
          enabled={settings.shop_enabled && settings.temporary_invite.enabled}
          busy={busyItem === "temporary_invite"}
          locked={locked}
          blocker={
            inviteBlocked
              ? inviteBlockerLabel(overview.invite_blocker ?? "")
              : temporaryInvitesRemaining === 0
                ? "本月已达限购"
                : undefined
          }
          onBuy={(quantity) =>
            onBuy("temporary_invite", quantity, settings.temporary_invite.price)
          }
          facts={[
            ["持有临时邀请", formatInteger(overview.temporary_invites)],
            [
              "最近过期",
              overview.temporary_invite_next_expiry
                ? formatCompactDateTime(overview.temporary_invite_next_expiry)
                : "—",
            ],
            [
              "本月已购",
              limitText(
                overview.temporary_invites_purchased_this_month,
                settings.temporary_invite.monthly_limit
              ),
            ],
          ]}
        />
        <ShopItemCard
          icon={CrownIcon}
          title={`VIP ${settings.vip.duration_days} 天`}
          description="享受 VIP 身份与相关权益；已是 VIP 时会在现有到期时间上顺延。"
          price={settings.vip.price}
          balance={balance}
          maximum={Math.min(12, Math.floor(3650 / settings.vip.duration_days))}
          enabled={settings.shop_enabled && settings.vip.enabled}
          busy={busyItem === "vip"}
          locked={locked}
          blocker={overview.vip.permanent ? "已是永久 VIP" : undefined}
          onBuy={(quantity) => onBuy("vip", quantity, settings.vip.price)}
          facts={[
            [
              "当前状态",
              overview.vip.permanent
                ? "永久 VIP"
                : overview.vip.active && overview.vip.until
                  ? `有效至 ${formatCompactDateTime(overview.vip.until)}`
                  : "未开通",
            ],
          ]}
        />
        <ShopItemCard
          icon={CalendarCheckIcon}
          title="补签卡"
          description={`漏签后可补签最近 ${settings.makeup_card.window_days} 天内最早断开的一天，补签同样计入连续签到并发放奖励。`}
          price={settings.makeup_card.price}
          balance={balance}
          maximum={cardsRemaining}
          enabled={settings.shop_enabled && settings.makeup_card.enabled}
          busy={busyItem === "makeup_card"}
          locked={locked}
          blocker={cardsRemaining === 0 ? "已达持有上限" : undefined}
          onBuy={(quantity) =>
            onBuy("makeup_card", quantity, settings.makeup_card.price)
          }
          facts={[
            [
              "持有数量",
              `${overview.makeup_cards} / ${settings.makeup_card.maximum_holding}`,
            ],
          ]}
          footer={
            <Link
              to="/account/economy?tab=attendance"
              className="text-xs text-link hover:underline"
            >
              前往签到补签
            </Link>
          }
        />
      </div>

      <NameColorCard
        overview={overview}
        username={username}
        busyItem={busyItem}
        locked={locked}
        styleBusy={styleBusy}
        balance={balance}
        onBuy={(itemKey, price, styleKey) => onBuy(itemKey, 1, price, styleKey)}
        onSwitchStyle={onSwitchStyle}
      />

      <RecentOrders orders={overview.recent_orders} />
    </>
  )
}

function ShopItemCard({
  icon: Icon,
  title,
  description,
  price,
  balance,
  maximum,
  enabled,
  busy,
  locked,
  blocker,
  facts,
  footer,
  onBuy,
}: {
  icon: typeof TicketIcon
  title: string
  description: string
  price: number
  balance: bigint
  maximum: number
  enabled: boolean
  busy: boolean
  locked: boolean
  blocker?: string
  facts: [string, string][]
  footer?: React.ReactNode
  onBuy: (quantity: number) => void
}) {
  const [quantity, setQuantity] = React.useState("1")
  const count = Number(quantity)
  const valid = Number.isInteger(count) && count >= 1 && count <= maximum
  const total = valid ? BigInt(price) * BigInt(count) : 0n
  const unavailable =
    blocker ?? (valid && balance < total ? "魔力值不足" : undefined)
  return (
    <Card>
      <CardHeader className="grid-cols-[2.5rem_1fr_auto] gap-x-3 has-data-[slot=card-action]:grid-cols-[2.5rem_1fr_auto]">
        <span
          className="row-span-2 flex size-10 items-center justify-center rounded-lg bg-primary/10 text-link [&_svg]:size-5"
          aria-hidden="true"
        >
          <Icon />
        </span>
        <CardTitle className="col-start-2">{title}</CardTitle>
        <CardAction className="col-start-3">
          <Badge variant="warning" className="font-mono tabular-nums">
            {formatInteger(price)} 魔力
          </Badge>
        </CardAction>
        <CardDescription className="col-span-2 col-start-2">
          {description}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-1.5">
        {facts.map(([label, value]) => (
          <dl
            key={label}
            className="flex items-center justify-between gap-3 text-sm"
          >
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="font-medium tabular-nums">{value}</dd>
          </dl>
        ))}
        {footer}
      </CardContent>
      <CardFooter className="flex-wrap gap-2">
        {enabled && !blocker ? (
          <Input
            type="number"
            min={1}
            max={maximum}
            value={quantity}
            onChange={(event) => setQuantity(event.target.value)}
            aria-label={`${title}购买数量`}
            disabled={locked}
            className="w-24"
          />
        ) : null}
        <Button
          className="flex-1"
          disabled={!enabled || Boolean(unavailable) || !valid || locked}
          onClick={() => onBuy(count)}
        >
          {busy ? (
            <Spinner data-icon="inline-start" />
          ) : (
            <ShoppingBagIcon data-icon="inline-start" />
          )}
          {!enabled
            ? "暂未开放"
            : (unavailable ??
              (valid
                ? `购买 · ${formatInteger(total)} 魔力`
                : `请输入 1–${maximum} 的整数`))}
        </Button>
      </CardFooter>
    </Card>
  )
}

function NameColorCard({
  overview,
  username,
  busyItem,
  locked,
  styleBusy,
  balance,
  onBuy,
  onSwitchStyle,
}: {
  overview: MagicShopOverview
  username: string
  busyItem?: MagicShopItemKey
  locked: boolean
  styleBusy: boolean
  balance: bigint
  onBuy: (itemKey: MagicShopItemKey, price: number, styleKey: string) => void
  onSwitchStyle: (styleKey: string) => void
}) {
  const settings = overview.settings.name_color
  const current =
    overview.name_style &&
    Date.parse(overview.name_style.expires_at) > Date.now()
      ? overview.name_style
      : undefined
  const [selected, setSelected] = React.useState(
    current?.style_key ?? overview.name_style_options[0]?.key ?? ""
  )
  const option = overview.name_style_options.find(
    (item) => item.key === selected
  )
  const tier = option?.tier ?? "solid"
  const itemKey: MagicShopItemKey =
    tier === "gradient" ? "name_color_gradient" : "name_color_solid"
  const price =
    tier === "gradient" ? settings.gradient_price : settings.solid_price
  const enabled = overview.settings.shop_enabled && settings.enabled
  const canSwitch =
    current &&
    current.style_key !== selected &&
    (tier === "solid" || current.tier === "gradient")
  const purchaseBlocker =
    current?.tier === "gradient" && tier === "solid"
      ? "渐变特权期内可免费切换纯色"
      : balance < BigInt(price)
        ? "魔力值不足"
        : undefined
  const restartHint = current && current.tier === "solid" && tier === "gradient"

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <PaletteIcon aria-hidden="true" />
          彩色 ID
        </CardTitle>
        <CardDescription>
          购买后用户名在动态、评论等位置以所选颜色展示，有效期{" "}
          {settings.duration_days}{" "}
          天；同档位续购会顺延，有效期内可免费切换同档或更低档的配色。
        </CardDescription>
        <CardAction>
          {current ? (
            <Badge variant="primary">
              {current.tier === "gradient" ? "渐变" : "纯色"} · 至{" "}
              {formatCompactDateTime(current.expires_at)}
            </Badge>
          ) : (
            <Badge variant="outline">未拥有</Badge>
          )}
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {(["solid", "gradient"] as const).map((group) => (
          <section
            key={group}
            className="flex flex-col gap-2"
            aria-label={group === "solid" ? "纯色" : "渐变"}
          >
            <div className="flex items-baseline justify-between gap-2">
              <h3 className="text-sm font-medium">
                {group === "solid" ? "纯色" : "渐变 / 流光"}
              </h3>
              <span className="font-mono text-xs text-muted-foreground tabular-nums">
                {formatInteger(
                  group === "solid"
                    ? settings.solid_price
                    : settings.gradient_price
                )}{" "}
                魔力
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
              {overview.name_style_options
                .filter((item) => item.tier === group)
                .map((item) => (
                  <button
                    key={item.key}
                    type="button"
                    aria-pressed={selected === item.key}
                    onClick={() => setSelected(item.key)}
                    className={cn(
                      "flex min-w-0 flex-col items-start gap-0.5 rounded-lg border bg-muted/40 px-3 py-2 text-left transition-colors hover:bg-muted",
                      selected === item.key &&
                        "border-primary/60 bg-primary/5 ring-1 ring-primary/20"
                    )}
                  >
                    <span
                      data-name-style={item.key}
                      className="max-w-full truncate text-sm font-semibold"
                    >
                      {username}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {nameStyleLabel(item.key)}
                      {current?.style_key === item.key ? " · 使用中" : ""}
                    </span>
                  </button>
                ))}
            </div>
          </section>
        ))}
        {restartHint ? (
          <p className="text-xs text-muted-foreground">
            从纯色升级到渐变会重新计算有效期，剩余的纯色天数不会保留。
          </p>
        ) : null}
      </CardContent>
      <CardFooter className="flex-wrap gap-2">
        {canSwitch ? (
          <Button
            variant="outline"
            disabled={locked}
            onClick={() => onSwitchStyle(selected)}
          >
            {styleBusy ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <PaletteIcon data-icon="inline-start" />
            )}
            免费切换为「{nameStyleLabel(selected)}」
          </Button>
        ) : null}
        <Button
          className="flex-1"
          disabled={!enabled || !option || Boolean(purchaseBlocker) || locked}
          onClick={() => onBuy(itemKey, price, selected)}
        >
          {busyItem === itemKey ? (
            <Spinner data-icon="inline-start" />
          ) : (
            <ShoppingBagIcon data-icon="inline-start" />
          )}
          {!enabled
            ? "暂未开放"
            : (purchaseBlocker ??
              `${current && current.tier === tier ? "续期" : "购买"}「${nameStyleLabel(selected)}」· ${formatInteger(price)} 魔力`)}
        </Button>
      </CardFooter>
    </Card>
  )
}

function RecentOrders({ orders }: { orders: MagicShopOrder[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>最近兑换</CardTitle>
        <CardDescription>最多展示最近 50 条兑换记录。</CardDescription>
      </CardHeader>
      <CardContent>
        {orders.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            还没有兑换记录。
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>商品</TableHead>
                <TableHead className="text-right">花费</TableHead>
                <TableHead>效果</TableHead>
                <TableHead className="text-right">时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {orders.map((order) => (
                <TableRow key={order.id}>
                  <TableCell>
                    {itemLabel(order.item_key)}
                    {order.quantity > 1 ? ` ×${order.quantity}` : ""}
                    {order.style_key ? (
                      <span className="ml-2 text-xs text-muted-foreground">
                        {nameStyleLabel(order.style_key)}
                      </span>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-right font-mono tabular-nums">
                    {formatInteger(order.total_price)}
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {order.effect_until
                      ? `至 ${formatCompactDateTime(order.effect_until)}`
                      : "—"}
                  </TableCell>
                  <TableCell className="text-right text-muted-foreground">
                    {formatCompactDateTime(order.purchased_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

export function itemLabel(key: MagicShopItemKey) {
  return {
    invite: "永久邀请名额",
    temporary_invite: "临时邀请",
    vip: "VIP",
    name_color_solid: "彩色 ID（纯色）",
    name_color_gradient: "彩色 ID（渐变）",
    makeup_card: "补签卡",
  }[key]
}

function inviteBlockerLabel(blocker: string) {
  return (
    {
      disabled: "成员邀请未开放",
      account_unavailable: "账户状态受限",
      email_unverified: "请先验证邮箱",
      account_age: "注册时间未达标",
      level: "等级未达标",
      quota_exhausted: "已达站点发邀上限",
    }[blocker] ?? "暂不可购买"
  )
}

function monthlyRemaining(used: number, limit: number) {
  return limit > 0 ? Math.min(100, Math.max(0, limit - used)) : 100
}

function limitText(used: number, limit: number) {
  return limit > 0 ? `${used} / ${limit}` : `${used}（不限购）`
}

function MagicShopSkeleton() {
  return (
    <PageLayout aria-label="魔力商店加载中">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-2">
          <Skeleton className="h-9 w-40" />
          <Skeleton className="h-4 w-56" />
        </div>
        <Skeleton className="h-12 w-44" />
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <Skeleton className="h-56 w-full" />
        <Skeleton className="h-56 w-full" />
        <Skeleton className="h-56 w-full" />
        <Skeleton className="h-56 w-full" />
      </div>
    </PageLayout>
  )
}
