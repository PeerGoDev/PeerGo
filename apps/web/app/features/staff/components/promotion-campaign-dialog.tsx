import * as React from "react"
import { CircleAlertIcon, LoaderCircleIcon } from "lucide-react"

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
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
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
import { Textarea } from "~/components/ui/textarea"
import { useSchedulePromotionCampaign } from "~/features/staff/api/promotion-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"

const promotionItems = [
  { value: "free", label: "免费（0× 下载）" },
  { value: "double_upload", label: "2× 上传" },
  { value: "double_upload_free", label: "2× 上传 / 免费" },
  { value: "half_download", label: "50% 下载" },
  { value: "double_upload_half_download", label: "2× 上传 / 50% 下载" },
  { value: "thirty_percent_download", label: "30% 下载" },
] as const

const scopeItems = [
  { value: "global", label: "全站活动" },
  { value: "torrent", label: "单个种子" },
] as const

export function PromotionCampaignDialog({
  open,
  csrfToken,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onCreated: (message: string) => void
}) {
  const mutation = useSchedulePromotionCampaign()
  const initial = React.useMemo(defaultWindow, [open])
  const [scope, setScope] = React.useState<"global" | "torrent">("global")
  const [torrentId, setTorrentId] = React.useState("")
  const [promotion, setPromotion] = React.useState("free")
  const [startsAt, setStartsAt] = React.useState(initial.startsAt)
  const [endsAt, setEndsAt] = React.useState(initial.endsAt)
  const [reason, setReason] = React.useState("")
  const reasonLength = Array.from(reason.trim()).length
  const torrentIDNumber = Number(torrentId)
  const validTorrent =
    scope === "global" ||
    (Number.isSafeInteger(torrentIDNumber) && torrentIDNumber >= 1)
  const startTimestamp = new Date(startsAt).getTime()
  const endTimestamp = new Date(endsAt).getTime()
  const duration = endTimestamp - startTimestamp
  const validWindow =
    Number.isFinite(duration) &&
    startTimestamp >= Date.now() - 30_000 &&
    duration >= 5 * 60_000 &&
    duration <= 30 * 24 * 60 * 60_000
  const canSubmit =
    validTorrent && validWindow && reasonLength >= 10 && reasonLength <= 1000
  const invalidReason = reasonLength > 0 && reasonLength < 10

  React.useEffect(() => {
    if (!open) {
      mutation.reset()
      setScope("global")
      setTorrentId("")
      setPromotion("free")
      const next = defaultWindow()
      setStartsAt(next.startsAt)
      setEndsAt(next.endsAt)
      setReason("")
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canSubmit) return
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        body: {
          scope,
          torrent_id: scope === "torrent" ? torrentIDNumber : undefined,
          promotion: promotion as (typeof promotionItems)[number]["value"],
          starts_at: new Date(startsAt).toISOString(),
          ends_at: new Date(endsAt).toISOString(),
          reason: reason.trim(),
        },
      })
      onCreated(
        scope === "global"
          ? "全站优惠已签发，正在投递到 Settlement。"
          : `种子 #${torrentIDNumber} 的优惠已签发，正在投递到 Settlement。`
      )
      onOpenChange(false)
    } catch {
      // Keep the complete command visible so the operator can review the
      // conflict response before changing its scope or time window.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>签发优惠政策</DialogTitle>
            <DialogDescription>
              创建后不可编辑。全站活动生效期间覆盖单种子优惠，结束后原单种子优惠会自动恢复。
            </DialogDescription>
          </DialogHeader>

          <FieldGroup className="py-5">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="promotion-scope">作用范围</FieldLabel>
                <Select
                  items={scopeItems}
                  value={scope}
                  onValueChange={(value) =>
                    setScope(value === "torrent" ? "torrent" : "global")
                  }
                >
                  <SelectTrigger id="promotion-scope" className="w-full">
                    <SelectValue>
                      {scopeItems.find((item) => item.value === scope)?.label}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {scopeItems.map((item) => (
                        <SelectItem key={item.value} value={item.value}>
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>

              <Field>
                <FieldLabel htmlFor="promotion-type">优惠类型</FieldLabel>
                <Select
                  items={promotionItems}
                  value={promotion}
                  onValueChange={(value) => value && setPromotion(value)}
                >
                  <SelectTrigger id="promotion-type" className="w-full">
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
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            </div>

            {scope === "torrent" ? (
              <Field data-invalid={!validTorrent || undefined}>
                <FieldLabel htmlFor="promotion-torrent-id">
                  数字种子 ID
                </FieldLabel>
                <Input
                  id="promotion-torrent-id"
                  type="number"
                  min={1}
                  step={1}
                  value={torrentId}
                  aria-invalid={!validTorrent || undefined}
                  placeholder="例如 1234"
                  onChange={(event) => setTorrentId(event.target.value)}
                />
                {!validTorrent ? (
                  <FieldError>请输入有效的已发布种子 ID。</FieldError>
                ) : null}
              </Field>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <Field data-invalid={!validWindow || undefined}>
                <FieldLabel htmlFor="promotion-starts-at">开始时间</FieldLabel>
                <Input
                  id="promotion-starts-at"
                  type="datetime-local"
                  value={startsAt}
                  aria-invalid={!validWindow || undefined}
                  onChange={(event) => setStartsAt(event.target.value)}
                />
              </Field>
              <Field data-invalid={!validWindow || undefined}>
                <FieldLabel htmlFor="promotion-ends-at">结束时间</FieldLabel>
                <Input
                  id="promotion-ends-at"
                  type="datetime-local"
                  value={endsAt}
                  aria-invalid={!validWindow || undefined}
                  onChange={(event) => setEndsAt(event.target.value)}
                />
              </Field>
            </div>
            {!validWindow ? (
              <FieldError>
                开始时间不能早于当前时间，活动须持续 5 分钟至 30 天。
              </FieldError>
            ) : null}

            <Field
              data-invalid={invalidReason || mutation.isError || undefined}
            >
              <FieldLabel htmlFor="promotion-reason">签发原因</FieldLabel>
              <Textarea
                id="promotion-reason"
                value={reason}
                maxLength={1000}
                rows={4}
                aria-invalid={invalidReason || mutation.isError || undefined}
                placeholder="说明活动目的、范围与交接信息（至少 10 个字符）"
                onChange={(event) => {
                  setReason(event.target.value)
                  if (mutation.isError) mutation.reset()
                }}
              />
              <FieldDescription>
                已输入 {reasonLength}/1000 个字符；至少填写 10
                个字符后才可签发。
              </FieldDescription>
              {invalidReason ? (
                <FieldError>签发原因至少需要 10 个字符。</FieldError>
              ) : null}
              {mutation.isError ? (
                <FieldError className="items-start">
                  <CircleAlertIcon className="mt-0.5" />
                  {requestErrorDescription(
                    mutation.error,
                    "签发失败。请检查目标种子、时间重叠与后台权限后重试。"
                  )}
                </FieldError>
              ) : null}
            </Field>
          </FieldGroup>

          <DialogFooter>
            <DialogClose
              render={<Button variant="outline" />}
              disabled={mutation.isPending}
            >
              取消
            </DialogClose>
            <Button type="submit" disabled={!canSubmit || mutation.isPending}>
              {mutation.isPending ? (
                <LoaderCircleIcon
                  data-icon="inline-start"
                  className="animate-spin"
                />
              ) : null}
              {mutation.isPending ? "签发中…" : "确认签发"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function defaultWindow() {
  const startsAt = new Date(Date.now() + 10 * 60_000)
  startsAt.setSeconds(0, 0)
  const endsAt = new Date(startsAt.getTime() + 24 * 60 * 60_000)
  return {
    startsAt: localDateTimeValue(startsAt),
    endsAt: localDateTimeValue(endsAt),
  }
}

function localDateTimeValue(value: Date) {
  const local = new Date(value.getTime() - value.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}
