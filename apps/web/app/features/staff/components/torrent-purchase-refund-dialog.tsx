import * as React from "react"
import { LoaderCircleIcon, RotateCcwIcon } from "lucide-react"

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
import { Textarea } from "~/components/ui/textarea"
import {
  type ManagedTorrentPurchase,
  useRefundTorrentPurchase,
} from "~/features/staff/api/torrent-purchase-administration.queries"
import { formatInteger } from "~/shared/formatters/integer"

export function TorrentPurchaseRefundDialog({
  purchase,
  csrfToken,
  onOpenChange,
  onRefunded,
}: {
  purchase?: ManagedTorrentPurchase
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onRefunded: (message: string) => void
}) {
  const mutation = useRefundTorrentPurchase()
  const refundPurchaseRequestId = React.useRef<string | undefined>(undefined)
  const [reason, setReason] = React.useState("")
  const reasonLength = Array.from(reason.trim()).length

  React.useEffect(() => {
    setReason("")
    mutation.reset()
    refundPurchaseRequestId.current = undefined
  }, [purchase]) // eslint-disable-line react-hooks/exhaustive-deps

  async function handleSubmit() {
    if (!purchase || reasonLength < 10 || reasonLength > 1000) return
    try {
      const idempotencyKey =
        refundPurchaseRequestId.current ?? crypto.randomUUID()
      refundPurchaseRequestId.current = idempotencyKey
      const result = await mutation.mutateAsync({
        buyerNumericId: purchase.buyer_numeric_id,
        torrentId: purchase.torrent_id,
        csrfToken,
        idempotencyKey,
        body: { reason: reason.trim() },
      })
      refundPurchaseRequestId.current = undefined
      onRefunded(
        `已撤销用户 #${result.buyer_numeric_id} 对种子 #${result.torrent_id} 的购买权限，并返还 ${formatInteger(result.refund_amount)} 魔力值。`
      )
      onOpenChange(false)
    } catch {
      // Preserve the reviewed reason so staff can compare the refreshed record
      // before retrying an idempotent financial operation.
    }
  }

  return (
    <AlertDialog open={Boolean(purchase)} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <RotateCcwIcon />
          </AlertDialogMedia>
          <AlertDialogTitle>确认退款并撤销购买权限？</AlertDialogTitle>
          <AlertDialogDescription>
            买家将收到购买时支付的全部整数魔力值，下载权限同时失效。退款由站点承担，不追回发布者历史收入和手续费。
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="rounded-lg border bg-muted/30 p-3 text-sm">
          <div className="font-medium">
            用户 #{purchase?.buyer_numeric_id} · {purchase?.buyer_username}
          </div>
          <div className="mt-1 line-clamp-2 text-muted-foreground">
            种子 #{purchase?.torrent_id} · {purchase?.torrent_title}
          </div>
          <div className="mt-2 font-medium tabular-nums">
            返还 {formatInteger(purchase?.price ?? "0")} 魔力值
          </div>
        </div>

        <FieldGroup>
          <Field
            data-invalid={
              (reasonLength > 0 && reasonLength < 10) ||
              mutation.isError ||
              undefined
            }
          >
            <FieldLabel htmlFor="torrent-purchase-refund-reason">
              退款说明
            </FieldLabel>
            <Textarea
              id="torrent-purchase-refund-reason"
              value={reason}
              rows={3}
              maxLength={1000}
              placeholder="例如：核实为重复购买，按工单结论执行全额退款"
              onChange={(event) => {
                setReason(event.target.value)
                if (mutation.isError) {
                  mutation.reset()
                  refundPurchaseRequestId.current = undefined
                }
              }}
            />
            <FieldDescription>{reasonLength}/1000 字符</FieldDescription>
            {reasonLength > 0 && reasonLength < 10 ? (
              <FieldError>退款说明至少需要 10 个字符。</FieldError>
            ) : null}
            {mutation.isError ? (
              <FieldError>
                退款失败，该记录可能已经退款，请刷新购买记录后重试。
              </FieldError>
            ) : null}
          </Field>
        </FieldGroup>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            取消
          </AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={
              mutation.isPending || reasonLength < 10 || reasonLength > 1000
            }
            onClick={() => void handleSubmit()}
          >
            {mutation.isPending ? (
              <LoaderCircleIcon
                className="animate-spin"
                data-icon="inline-start"
              />
            ) : (
              <RotateCcwIcon data-icon="inline-start" />
            )}
            {mutation.isPending ? "退款中…" : "确认退款"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
