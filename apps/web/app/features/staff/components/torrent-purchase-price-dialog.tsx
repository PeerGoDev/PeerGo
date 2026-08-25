import * as React from "react"
import { CoinsIcon, LoaderCircleIcon } from "lucide-react"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Textarea } from "~/components/ui/textarea"
import {
  type ManagedTorrent,
  useUpdateTorrentPurchasePrice,
} from "~/features/staff/api/torrent-administration.queries"

export function TorrentPurchasePriceDialog({
  torrent,
  csrfToken,
  onOpenChange,
  onChanged,
}: {
  torrent?: ManagedTorrent
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onChanged: (message: string) => void
}) {
  const mutation = useUpdateTorrentPurchasePrice()
  const updatePurchasePriceRequestId = React.useRef<string | undefined>(
    undefined
  )
  const [price, setPrice] = React.useState("")
  const [reason, setReason] = React.useState("")
  const parsedPrice = Number(price)
  const priceValid =
    price !== "" &&
    Number.isSafeInteger(parsedPrice) &&
    parsedPrice >= 0 &&
    parsedPrice <= 1_000_000
  const reasonLength = Array.from(reason.trim()).length
  const changed = priceValid && price !== torrent?.purchase_price

  React.useEffect(() => {
    setPrice(torrent?.purchase_price ?? "")
    setReason("")
    mutation.reset()
    updatePurchasePriceRequestId.current = undefined
  }, [torrent]) // eslint-disable-line react-hooks/exhaustive-deps

  async function handleSubmit() {
    if (!torrent || !changed || reasonLength > 1000) {
      return
    }
    try {
      const idempotencyKey =
        updatePurchasePriceRequestId.current ?? crypto.randomUUID()
      updatePurchasePriceRequestId.current = idempotencyKey
      const result = await mutation.mutateAsync({
        torrentId: torrent.id,
        csrfToken,
        idempotencyKey,
        body: {
          price: parsedPrice,
          expected_version: torrent.version,
          reason: reason.trim(),
        },
      })
      updatePurchasePriceRequestId.current = undefined
      onChanged(
        `种子 #${result.torrent_id} 已${result.price === "0" ? "设为免费" : `设为 ${result.price} 魔力值`}。`
      )
      onOpenChange(false)
    } catch {
      // Keep the reviewed values visible; the operator can refresh the list
      // when another edit changed the optimistic torrent version first.
    }
  }

  return (
    <AlertDialog open={Boolean(torrent)} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <CoinsIcon />
          </AlertDialogMedia>
          <AlertDialogTitle>设置种子价格</AlertDialogTitle>
          <AlertDialogDescription>
            价格使用整数魔力值。设为 0
            表示免费下载；已购买用户的永久权限和历史扣款不会改变。
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="rounded-lg border bg-muted/30 p-3 text-sm">
          <div className="font-medium">
            #{torrent?.id} · {torrent?.title}
          </div>
          <div className="mt-1 text-xs text-muted-foreground">
            当前价格：
            {torrent?.purchase_price === "0"
              ? "免费"
              : `${torrent?.purchase_price} 魔力值`}
          </div>
        </div>

        <FieldGroup>
          <Field data-invalid={(!priceValid && price !== "") || undefined}>
            <FieldLabel htmlFor="torrent-purchase-price">价格</FieldLabel>
            <Input
              id="torrent-purchase-price"
              type="number"
              inputMode="numeric"
              min={0}
              max={1_000_000}
              step={1}
              value={price}
              aria-invalid={(!priceValid && price !== "") || undefined}
              onChange={(event) => {
                setPrice(event.target.value)
                if (mutation.isError) {
                  mutation.reset()
                  updatePurchasePriceRequestId.current = undefined
                }
              }}
            />
            <FieldDescription>0–1,000,000，只允许整数。</FieldDescription>
            {!priceValid && price !== "" ? (
              <FieldError>请输入有效的整数魔力值价格。</FieldError>
            ) : null}
          </Field>

          <Field
            data-invalid={reasonLength > 1000 || mutation.isError || undefined}
          >
            <FieldLabel htmlFor="torrent-purchase-price-reason">
              修改说明
            </FieldLabel>
            <Textarea
              id="torrent-purchase-price-reason"
              value={reason}
              rows={3}
              maxLength={1000}
              placeholder="例如：按当前活动规则调整该资源价格"
              onChange={(event) => {
                setReason(event.target.value)
                if (mutation.isError) {
                  mutation.reset()
                  updatePurchasePriceRequestId.current = undefined
                }
              }}
            />
            <FieldDescription>{reasonLength}/1000 字符</FieldDescription>
            {reasonLength > 1000 ? (
              <FieldError>修改说明不能超过 1000 个字符。</FieldError>
            ) : null}
            {mutation.isError ? (
              <FieldError>
                保存失败，种子可能已被其他管理员修改，请刷新后重试。
              </FieldError>
            ) : null}
          </Field>
        </FieldGroup>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            取消
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={mutation.isPending || !changed || reasonLength > 1000}
            onClick={() => void handleSubmit()}
          >
            {mutation.isPending ? (
              <LoaderCircleIcon
                className="animate-spin"
                data-icon="inline-start"
              />
            ) : null}
            {mutation.isPending ? "保存中…" : "确认保存"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
