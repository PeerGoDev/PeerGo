import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CheckIcon,
  CircleAlertIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  Trash2Icon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
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
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "~/components/ui/field"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import {
  managedTorrentWithdrawalsQueryOptions,
  type ManagedTorrentWithdrawalRequest,
  useDecideTorrentWithdrawal,
} from "~/features/staff/api/torrent-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

export function TorrentWithdrawalReview({
  enabled,
  csrfToken,
  onDecided,
}: {
  enabled: boolean
  csrfToken: string
  onDecided: (message: string) => void
}) {
  const requests = useQuery({
    ...managedTorrentWithdrawalsQueryOptions,
    enabled,
  })
  const [target, setTarget] = React.useState<ManagedTorrentWithdrawalRequest>()

  if (!enabled) return null

  return (
    <>
      <Card className="gap-0 py-0">
        <CardHeader className="flex flex-row items-center justify-between gap-3 p-5 pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <Trash2Icon />
            种子撤回审核
            {requests.data ? (
              <Badge variant="secondary">{requests.data.total}</Badge>
            ) : null}
          </CardTitle>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="刷新种子撤回审核"
            disabled={requests.isFetching}
            onClick={() => void requests.refetch()}
          >
            <RefreshCwIcon />
          </Button>
        </CardHeader>
        <CardContent className="p-5 pt-0">
          {requests.isPending ? (
            <div className="flex min-h-20 items-center justify-center gap-2 text-sm text-muted-foreground">
              <Spinner /> 正在读取撤回申请
            </div>
          ) : requests.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>撤回申请暂时无法读取</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(requests.error, "请稍后重试。")}
              </AlertDescription>
            </Alert>
          ) : requests.data.items.length === 0 ? (
            <Empty className="min-h-28 border-0 py-4">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <CheckIcon />
                </EmptyMedia>
                <EmptyTitle>暂无待处理撤回申请</EmptyTitle>
                <EmptyDescription>
                  用户申请后会先停止公开，申请将在这里等待审核。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="divide-y rounded-md border">
              {requests.data.items.map((item) => (
                <div
                  key={item.request.id}
                  className="flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">
                      #{item.request.torrent_id} {item.request.torrent_title}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      用户 #{item.uploader_numeric_id} ·{" "}
                      {item.uploader_display_name || item.uploader_username} ·{" "}
                      {formatDateTime(item.request.created_at)}
                    </p>
                    <p className="mt-1 line-clamp-1 text-xs text-muted-foreground">
                      {item.request.reason}
                    </p>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    {item.active_purchase_count > 0 ? (
                      <Badge variant="destructive">
                        {item.active_purchase_count} 项待退款权益
                      </Badge>
                    ) : null}
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() => setTarget(item)}
                    >
                      审核
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {target ? (
        <TorrentWithdrawalDecisionDialog
          item={target}
          csrfToken={csrfToken}
          onOpenChange={(open) => {
            if (!open) setTarget(undefined)
          }}
          onDecided={(message) => {
            setTarget(undefined)
            onDecided(message)
          }}
        />
      ) : null}
    </>
  )
}

function TorrentWithdrawalDecisionDialog({
  item,
  csrfToken,
  onOpenChange,
  onDecided,
}: {
  item: ManagedTorrentWithdrawalRequest
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onDecided: (message: string) => void
}) {
  const reasonId = React.useId()
  const [reason, setReason] = React.useState("")
  const [decision, setDecision] = React.useState<"approve" | "reject">()
  const requestId = React.useRef<string | undefined>(undefined)
  const mutation = useDecideTorrentWithdrawal()
  const reasonLength = Array.from(reason.trim()).length

  async function decide(nextDecision: "approve" | "reject") {
    if (reasonLength < 10 || reasonLength > 1000) return
    if (nextDecision === "approve" && item.active_purchase_count > 0) return
    setDecision(nextDecision)
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      const result = await mutation.mutateAsync({
        requestId: item.request.id,
        csrfToken,
        idempotencyKey: requestId.current,
        body: {
          expected_request_version: item.request.version,
          decision: nextDecision,
          reason: reason.trim(),
        },
      })
      onDecided(
        result.decision === "approve"
          ? `种子 #${result.torrent_id} 已完成墓碑删除，原始证据仍保留。`
          : `种子 #${result.torrent_id} 已恢复发布和 Tracker 准入。`
      )
    } catch {
      setDecision(undefined)
    }
  }

  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onOpenChange(false)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <Trash2Icon />
          </AlertDialogMedia>
          <AlertDialogTitle>审核种子撤回申请</AlertDialogTitle>
          <AlertDialogDescription>
            #{item.request.torrent_id} · {item.request.torrent_title} · 上传者 #
            {item.uploader_numeric_id}
          </AlertDialogDescription>
        </AlertDialogHeader>

        <Alert>
          <CircleAlertIcon />
          <AlertTitle>用户撤回理由</AlertTitle>
          <AlertDescription>{item.request.reason}</AlertDescription>
        </Alert>

        {item.active_purchase_count > 0 ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>暂时不能批准删除</AlertTitle>
            <AlertDescription>
              仍有 {item.active_purchase_count}{" "}
              项有效购买权益。请先在购买记录中逐项退款；全部撤销后刷新本队列，才能批准删除。
            </AlertDescription>
          </Alert>
        ) : null}

        {mutation.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>审核决定未提交</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(
                mutation.error,
                "请重新载入撤回队列并核对最新状态。"
              )}
            </AlertDescription>
          </Alert>
        ) : null}

        <Field data-invalid={reasonLength > 0 && reasonLength < 10}>
          <FieldLabel htmlFor={reasonId}>审核说明</FieldLabel>
          <Textarea
            id={reasonId}
            value={reason}
            minLength={10}
            maxLength={1000}
            rows={4}
            disabled={mutation.isPending}
            aria-invalid={reasonLength > 0 && reasonLength < 10}
            placeholder="记录批准依据，或说明恢复发布的原因"
            onChange={(event) => {
              setReason(event.target.value)
              requestId.current = undefined
              if (mutation.isError) mutation.reset()
            }}
          />
          <FieldDescription>
            {reasonLength} / 1000，至少 10 个字符
          </FieldDescription>
          {reasonLength > 0 && reasonLength < 10 ? (
            <FieldError>审核说明至少需要 10 个字符。</FieldError>
          ) : null}
        </Field>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            取消
          </AlertDialogCancel>
          <AlertDialogAction
            variant="actionSuccess"
            disabled={mutation.isPending || reasonLength < 10}
            onClick={() => void decide("reject")}
          >
            {mutation.isPending && decision === "reject" ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <RotateCcwIcon data-icon="inline-start" />
            )}
            驳回并恢复
          </AlertDialogAction>
          <AlertDialogAction
            variant="destructive"
            disabled={
              mutation.isPending ||
              reasonLength < 10 ||
              item.active_purchase_count > 0
            }
            onClick={() => void decide("approve")}
          >
            {mutation.isPending && decision === "approve" ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <Trash2Icon data-icon="inline-start" />
            )}
            {item.active_purchase_count > 0 ? "需先完成退款" : "批准删除"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
