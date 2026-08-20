import * as React from "react"
import { CircleAlertIcon, SaveIcon } from "lucide-react"

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
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import {
  type PromotionProductPolicy,
  useUpdatePromotionProductPolicy,
} from "~/features/staff/api/promotion-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"

const priceFields = [
  { key: "free_price_per_day", label: "免费（0× 下载）" },
  { key: "double_upload_price_per_day", label: "2× 上传" },
  { key: "double_upload_free_price_per_day", label: "2× 上传 / 免费" },
  { key: "half_download_price_per_day", label: "50% 下载" },
  {
    key: "double_upload_half_download_price_per_day",
    label: "2× 上传 / 50% 下载",
  },
  { key: "thirty_percent_download_price_per_day", label: "30% 下载" },
] as const

type PriceKey = (typeof priceFields)[number]["key"]

export function PromotionProductPolicyDialog({
  open,
  onOpenChange,
  policy,
  csrfToken,
  onUpdated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  policy: PromotionProductPolicy
  csrfToken: string
  onUpdated: (message: string) => void
}) {
  const mutation = useUpdatePromotionProductPolicy()
  const [promotionEnabled, setPromotionEnabled] = React.useState(
    policy.promotion_enabled
  )
  const [stickyEnabled, setStickyEnabled] = React.useState(
    policy.sticky_enabled
  )
  const [prices, setPrices] = React.useState<Record<PriceKey, string>>(() =>
    productPrices(policy)
  )
  const [stickyPrice, setStickyPrice] = React.useState(
    policy.sticky_price_per_day
  )
  const [maxPromotionDays, setMaxPromotionDays] = React.useState(
    String(policy.max_promotion_days)
  )
  const [maxStickyDays, setMaxStickyDays] = React.useState(
    String(policy.max_sticky_days)
  )
  const [reason, setReason] = React.useState("")
  const reasonLength = Array.from(reason.trim()).length
  const allPrices = [...Object.values(prices), stickyPrice]
  const validPrices = allPrices.every(validPrice)
  const promotionDays = Number(maxPromotionDays)
  const stickyDays = Number(maxStickyDays)
  const validDays =
    Number.isInteger(promotionDays) &&
    promotionDays >= 1 &&
    promotionDays <= 30 &&
    Number.isInteger(stickyDays) &&
    stickyDays >= 1 &&
    stickyDays <= 30
  const canSubmit =
    validPrices && validDays && reasonLength >= 10 && reasonLength <= 1000

  React.useEffect(() => {
    if (!open) return
    mutation.reset()
    setPromotionEnabled(policy.promotion_enabled)
    setStickyEnabled(policy.sticky_enabled)
    setPrices(productPrices(policy))
    setStickyPrice(policy.sticky_price_per_day)
    setMaxPromotionDays(String(policy.max_promotion_days))
    setMaxStickyDays(String(policy.max_sticky_days))
    setReason("")
  }, [open, policy]) // eslint-disable-line react-hooks/exhaustive-deps

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canSubmit || mutation.isPending) return
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        body: {
          expected_revision: policy.revision,
          promotion_enabled: promotionEnabled,
          sticky_enabled: stickyEnabled,
          free_price_per_day: Number(prices.free_price_per_day),
          double_upload_price_per_day: Number(
            prices.double_upload_price_per_day
          ),
          double_upload_free_price_per_day: Number(
            prices.double_upload_free_price_per_day
          ),
          half_download_price_per_day: Number(
            prices.half_download_price_per_day
          ),
          double_upload_half_download_price_per_day: Number(
            prices.double_upload_half_download_price_per_day
          ),
          thirty_percent_download_price_per_day: Number(
            prices.thirty_percent_download_price_per_day
          ),
          sticky_price_per_day: Number(stickyPrice),
          max_promotion_days: promotionDays,
          max_sticky_days: stickyDays,
          reason: reason.trim(),
        },
      })
      onUpdated("新的付费优惠与置顶价格已经生效，历史订单价格保持不变。")
      onOpenChange(false)
    } catch {
      // Keep the operator's complete draft visible for conflict review.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-2xl">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>编辑用户付费规则</DialogTitle>
            <DialogDescription>
              使用整数魔力值按天计价。保存会追加新版本，不会修改历史订单。
            </DialogDescription>
          </DialogHeader>

          <FieldGroup className="py-5">
            <FieldSet>
              <FieldLegend>种子优惠</FieldLegend>
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldTitle>允许用户购买优惠</FieldTitle>
                  <FieldDescription>
                    关闭后不再接受新订单，已经购买的时间段继续生效。
                  </FieldDescription>
                </FieldContent>
                <Switch
                  checked={promotionEnabled}
                  onCheckedChange={setPromotionEnabled}
                  aria-label="允许用户购买优惠"
                />
              </Field>
              <div className="grid gap-4 sm:grid-cols-2">
                {priceFields.map((item) => (
                  <Field key={item.key}>
                    <FieldLabel htmlFor={`policy-${item.key}`}>
                      {item.label}（魔力/天）
                    </FieldLabel>
                    <Input
                      id={`policy-${item.key}`}
                      type="number"
                      min={0}
                      max={1_000_000}
                      value={prices[item.key]}
                      onChange={(event) =>
                        setPrices((current) => ({
                          ...current,
                          [item.key]: event.target.value,
                        }))
                      }
                    />
                  </Field>
                ))}
                <Field>
                  <FieldLabel htmlFor="policy-max-promotion-days">
                    单次最长天数
                  </FieldLabel>
                  <Input
                    id="policy-max-promotion-days"
                    type="number"
                    min={1}
                    max={30}
                    value={maxPromotionDays}
                    onChange={(event) =>
                      setMaxPromotionDays(event.target.value)
                    }
                  />
                </Field>
              </div>
            </FieldSet>

            <FieldSet>
              <FieldLegend>列表置顶</FieldLegend>
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldTitle>允许用户购买置顶</FieldTitle>
                  <FieldDescription>
                    同一种子的置顶按顺序续期，不会互相覆盖。
                  </FieldDescription>
                </FieldContent>
                <Switch
                  checked={stickyEnabled}
                  onCheckedChange={setStickyEnabled}
                  aria-label="允许用户购买置顶"
                />
              </Field>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="policy-sticky-price">
                    置顶价格（魔力/天）
                  </FieldLabel>
                  <Input
                    id="policy-sticky-price"
                    type="number"
                    min={0}
                    max={1_000_000}
                    value={stickyPrice}
                    onChange={(event) => setStickyPrice(event.target.value)}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="policy-max-sticky-days">
                    单次最长天数
                  </FieldLabel>
                  <Input
                    id="policy-max-sticky-days"
                    type="number"
                    min={1}
                    max={30}
                    value={maxStickyDays}
                    onChange={(event) => setMaxStickyDays(event.target.value)}
                  />
                </Field>
              </div>
            </FieldSet>

            <Field>
              <FieldLabel htmlFor="policy-change-reason">修改原因</FieldLabel>
              <Textarea
                id="policy-change-reason"
                rows={3}
                minLength={10}
                maxLength={1000}
                value={reason}
                placeholder="至少 10 个字符，用于保留设置变更依据"
                onChange={(event) => setReason(event.target.value)}
              />
              <FieldDescription>{reasonLength}/1000</FieldDescription>
            </Field>
          </FieldGroup>

          {mutation.isError ? (
            <Alert variant="destructive" className="mb-4">
              <CircleAlertIcon />
              <AlertTitle>规则保存失败</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(
                  mutation.error,
                  "请刷新当前规则后重新修改。"
                )}
              </AlertDescription>
            </Alert>
          ) : null}

          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" />}>
              取消
            </DialogClose>
            <Button type="submit" disabled={!canSubmit || mutation.isPending}>
              {mutation.isPending ? (
                <Spinner />
              ) : (
                <SaveIcon data-icon="inline-start" />
              )}
              保存新版本
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function productPrices(policy: PromotionProductPolicy) {
  return Object.fromEntries(
    priceFields.map((item) => [item.key, policy[item.key]])
  ) as Record<PriceKey, string>
}

function validPrice(value: string) {
  return /^\d+$/.test(value) && Number(value) >= 0 && Number(value) <= 1_000_000
}
