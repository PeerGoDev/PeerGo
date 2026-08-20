import * as React from "react"
import { BadgePercentIcon, CircleAlertIcon, PinIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Separator } from "~/components/ui/separator"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type PromotionProductOffer,
  type PromotionProductOrder,
  usePromotionProductOffer,
  usePurchasePromotionProducts,
} from "~/features/torrent/api/promotion-products.queries"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

const promotionItems = [
  { value: "free", label: "免费（0× 下载）", priceKey: "free_price_per_day" },
  {
    value: "double_upload",
    label: "2× 上传",
    priceKey: "double_upload_price_per_day",
  },
  {
    value: "double_upload_free",
    label: "2× 上传 / 免费",
    priceKey: "double_upload_free_price_per_day",
  },
  {
    value: "half_download",
    label: "50% 下载",
    priceKey: "half_download_price_per_day",
  },
  {
    value: "double_upload_half_download",
    label: "2× 上传 / 50% 下载",
    priceKey: "double_upload_half_download_price_per_day",
  },
  {
    value: "thirty_percent_download",
    label: "30% 下载",
    priceKey: "thirty_percent_download_price_per_day",
  },
] as const

type PromotionValue = (typeof promotionItems)[number]["value"]

export function TorrentPromotionProductDialog({
  torrentId,
  torrentTitle,
}: {
  torrentId: number
  torrentTitle: string
}) {
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const allowed = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "torrent.promotion.purchase.self"
    )
  )
  const [open, setOpen] = React.useState(false)
  const offer = usePromotionProductOffer(
    session.data?.user.id,
    torrentId,
    open && allowed
  )

  if (!session.data || !allowed) return null

  return (
    <>
      <Button
        type="button"
        variant="outline"
        className="h-9 w-full"
        onClick={() => setOpen(true)}
      >
        <BadgePercentIcon data-icon="inline-start" />
        促销与置顶
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>促销种子</DialogTitle>
            <DialogDescription className="line-clamp-2">
              #{torrentId} · {torrentTitle}
            </DialogDescription>
          </DialogHeader>
          {offer.isPending ? (
            <div className="flex min-h-52 items-center justify-center gap-2 text-muted-foreground">
              <Spinner /> 正在读取价格…
            </div>
          ) : offer.isError || !offer.data ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>促销报价暂时无法读取</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(
                  offer.error,
                  "请稍后重试，当前不会扣除魔力值。"
                )}
              </AlertDescription>
            </Alert>
          ) : (
            <PromotionPurchaseForm
              offer={offer.data}
              userId={session.data.user.id}
              csrfToken={session.data.csrf_token}
              emailVerified={session.data.user.email_verified}
              onClose={() => setOpen(false)}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}

function PromotionPurchaseForm({
  offer,
  userId,
  csrfToken,
  emailVerified,
  onClose,
}: {
  offer: PromotionProductOffer
  userId: string
  csrfToken: string
  emailVerified: boolean
  onClose: () => void
}) {
  const mutation = usePurchasePromotionProducts({
    userId,
    csrfToken,
    torrentId: offer.torrent_id,
  })
  const [buyPromotion, setBuyPromotion] = React.useState(
    offer.policy.promotion_enabled
  )
  const [promotion, setPromotion] = React.useState<PromotionValue>("free")
  const [promotionDays, setPromotionDays] = React.useState("1")
  const [buySticky, setBuySticky] = React.useState(false)
  const [stickyDays, setStickyDays] = React.useState("1")
  const [completed, setCompleted] = React.useState<PromotionProductOrder>()
  const requestID = React.useRef<string>(undefined)
  const promotionDayCount = parseDayCount(promotionDays)
  const stickyDayCount = parseDayCount(stickyDays)
  const promotionPrice = promotionPriceFor(offer, promotion)
  const total =
    (buyPromotion ? promotionPrice * promotionDayCount : 0n) +
    (buySticky
      ? BigInt(offer.policy.sticky_price_per_day) * stickyDayCount
      : 0n)
  const balance = BigInt(offer.magic_balance)
  const valid =
    (buyPromotion || buySticky) &&
    (!buyPromotion ||
      (promotionDayCount >= 1n &&
        promotionDayCount <= BigInt(offer.policy.max_promotion_days))) &&
    (!buySticky ||
      (stickyDayCount >= 1n &&
        stickyDayCount <= BigInt(offer.policy.max_sticky_days)))
  const insufficient = total > balance

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!valid || insufficient || !emailVerified || mutation.isPending) return
    requestID.current ??= crypto.randomUUID()
    try {
      const order = await mutation.mutateAsync({
        idempotencyKey: requestID.current,
        body: {
          ...(buyPromotion
            ? { promotion, promotion_days: Number(promotionDayCount) }
            : {}),
          ...(buySticky ? { sticky_days: Number(stickyDayCount) } : {}),
        },
      })
      requestID.current = undefined
      setCompleted(order)
    } catch (error) {
      // A typed server response proves no ambiguous transport loss remains, so
      // a corrected selection must receive a fresh idempotency key. Unknown
      // transport failures retain the key for a safe retry of the same draft.
      if (error instanceof ApiProblemError) requestID.current = undefined
    }
  }

  if (completed) {
    return (
      <>
        <Alert>
          <BadgePercentIcon />
          <AlertTitle>购买完成</AlertTitle>
          <AlertDescription>
            已支付 {formatInteger(completed.total_price)} 魔力值。
            {completed.promotion_starts_at
              ? ` 优惠从 ${formatCompactDateTime(completed.promotion_starts_at)} 开始。`
              : ""}
            {completed.sticky_starts_at
              ? ` 置顶从 ${formatCompactDateTime(completed.sticky_starts_at)} 开始。`
              : ""}
          </AlertDescription>
        </Alert>
        <DialogFooter>
          <Button type="button" onClick={onClose}>
            完成
          </Button>
        </DialogFooter>
      </>
    )
  }

  return (
    <form onSubmit={submit}>
      <div className="flex items-center justify-between rounded-lg border bg-muted/30 px-3 py-2 text-sm">
        <span className="text-muted-foreground">当前魔力值</span>
        <strong className="tabular-nums">
          {formatInteger(offer.magic_balance)}
        </strong>
      </div>

      {!emailVerified ? (
        <Alert className="mt-4">
          <CircleAlertIcon />
          <AlertTitle>需要先验证邮箱</AlertTitle>
          <AlertDescription>
            可以查看价格，但验证邮箱前不会允许扣除魔力值。
          </AlertDescription>
        </Alert>
      ) : null}

      <FieldGroup className="py-5">
        <Field orientation="horizontal">
          <FieldContent>
            <FieldTitle>购买种子优惠</FieldTitle>
            <FieldDescription>
              {offer.policy.promotion_enabled
                ? "按天购买上传或下载倍率。"
                : "站点暂时关闭了用户付费优惠。"}
            </FieldDescription>
          </FieldContent>
          <Switch
            checked={buyPromotion}
            disabled={!offer.policy.promotion_enabled}
            onCheckedChange={setBuyPromotion}
            aria-label="购买种子优惠"
          />
        </Field>

        {buyPromotion ? (
          <div className="grid gap-4 sm:grid-cols-[1fr_112px]">
            <Field>
              <FieldLabel htmlFor="paid-promotion-type">优惠类型</FieldLabel>
              <Select
                items={promotionItems}
                value={promotion}
                onValueChange={(value) =>
                  value && setPromotion(value as PromotionValue)
                }
              >
                <SelectTrigger id="paid-promotion-type" className="w-full">
                  <SelectValue>
                    {
                      promotionItems.find((item) => item.value === promotion)
                        ?.label
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {promotionItems.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label} ·{" "}
                        {formatInteger(priceFor(offer, item.priceKey))}/天
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field
              data-invalid={
                promotionDayCount < 1n ||
                promotionDayCount > BigInt(offer.policy.max_promotion_days)
              }
            >
              <FieldLabel htmlFor="paid-promotion-days">天数</FieldLabel>
              <Input
                id="paid-promotion-days"
                type="number"
                min={1}
                max={offer.policy.max_promotion_days}
                value={promotionDays}
                onChange={(event) => setPromotionDays(event.target.value)}
              />
              <FieldError>
                {promotionDayCount < 1n ||
                promotionDayCount > BigInt(offer.policy.max_promotion_days)
                  ? `请输入 1–${offer.policy.max_promotion_days} 天`
                  : null}
              </FieldError>
            </Field>
          </div>
        ) : null}

        <Separator />

        <Field orientation="horizontal">
          <FieldContent>
            <FieldTitle className="flex items-center gap-2">
              <PinIcon /> 购买列表置顶
            </FieldTitle>
            <FieldDescription>
              {offer.policy.sticky_enabled
                ? `${formatInteger(offer.policy.sticky_price_per_day)} 魔力值/天，已有置顶时自动顺延。`
                : "站点暂时关闭了用户付费置顶。"}
            </FieldDescription>
          </FieldContent>
          <Switch
            checked={buySticky}
            disabled={!offer.policy.sticky_enabled}
            onCheckedChange={setBuySticky}
            aria-label="购买列表置顶"
          />
        </Field>

        {buySticky ? (
          <Field
            data-invalid={
              stickyDayCount < 1n ||
              stickyDayCount > BigInt(offer.policy.max_sticky_days)
            }
          >
            <FieldLabel htmlFor="paid-sticky-days">置顶天数</FieldLabel>
            <Input
              id="paid-sticky-days"
              type="number"
              min={1}
              max={offer.policy.max_sticky_days}
              value={stickyDays}
              onChange={(event) => setStickyDays(event.target.value)}
            />
            <FieldError>
              {stickyDayCount < 1n ||
              stickyDayCount > BigInt(offer.policy.max_sticky_days)
                ? `请输入 1–${offer.policy.max_sticky_days} 天`
                : null}
            </FieldError>
          </Field>
        ) : null}
      </FieldGroup>

      {offer.promotion_ends_at || offer.sticky_ends_at ? (
        <Alert className="mb-4">
          <PinIcon />
          <AlertTitle>已有时间段会自动顺延</AlertTitle>
          <AlertDescription>
            {offer.promotion_ends_at
              ? `付费优惠已排至 ${formatCompactDateTime(offer.promotion_ends_at)}。`
              : ""}
            {offer.sticky_ends_at
              ? ` 置顶已排至 ${formatCompactDateTime(offer.sticky_ends_at)}。`
              : ""}
          </AlertDescription>
        </Alert>
      ) : null}

      {mutation.isError ? (
        <Alert variant="destructive" className="mb-4">
          <CircleAlertIcon />
          <AlertTitle>购买未完成</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              mutation.error,
              "当前未扣除魔力值，请核对余额后重试。"
            )}
          </AlertDescription>
        </Alert>
      ) : null}

      <DialogFooter>
        <DialogClose render={<Button type="button" variant="outline" />}>
          取消
        </DialogClose>
        <Button
          type="submit"
          disabled={
            !valid || insufficient || !emailVerified || mutation.isPending
          }
        >
          {mutation.isPending ? (
            <Spinner />
          ) : (
            <BadgePercentIcon data-icon="inline-start" />
          )}
          {insufficient
            ? "魔力值不足"
            : `确认支付 ${formatInteger(total.toString())} 魔力值`}
        </Button>
      </DialogFooter>
    </form>
  )
}

function parseDayCount(value: string) {
  if (!/^\d+$/.test(value)) return -1n
  return BigInt(value)
}

function priceFor(
  offer: PromotionProductOffer,
  key: (typeof promotionItems)[number]["priceKey"]
) {
  return offer.policy[key]
}

function promotionPriceFor(
  offer: PromotionProductOffer,
  promotion: PromotionValue
) {
  const item = promotionItems.find((candidate) => candidate.value === promotion)
  return BigInt(item ? priceFor(offer, item.priceKey) : "0")
}
