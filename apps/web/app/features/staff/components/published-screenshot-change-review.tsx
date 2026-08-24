import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CheckIcon,
  CircleAlertIcon,
  ImagesIcon,
  RefreshCwIcon,
  XIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Dialog,
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
  FieldLabel,
} from "~/components/ui/field"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import {
  managedPublishedTorrentScreenshotChangesQueryOptions,
  type ManagedPublishedTorrentScreenshotChange,
  useDecidePublishedTorrentScreenshotChange,
} from "~/features/staff/api/torrent-administration.queries"
import { resolveApiUrl } from "~/shared/api/client"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

export function PublishedScreenshotChangeReview({
  enabled,
  csrfToken,
  onDecided,
}: {
  enabled: boolean
  csrfToken: string
  onDecided: (message: string) => void
}) {
  const requests = useQuery({
    ...managedPublishedTorrentScreenshotChangesQueryOptions,
    enabled,
  })
  const [target, setTarget] =
    React.useState<ManagedPublishedTorrentScreenshotChange>()
  if (!enabled) return null
  return (
    <>
      <Card className="gap-0 py-0">
        <CardHeader className="flex flex-row items-center justify-between gap-3 p-5 pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <ImagesIcon className="size-4" /> 已发布截图修改审核
            {requests.data ? (
              <Badge variant="secondary">{requests.data.total}</Badge>
            ) : null}
          </CardTitle>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="刷新截图修改审核"
            disabled={requests.isFetching}
            onClick={() => void requests.refetch()}
          >
            <RefreshCwIcon />
          </Button>
        </CardHeader>
        <CardContent className="p-5 pt-0">
          {requests.isPending ? (
            <div className="flex min-h-20 items-center justify-center gap-2 text-sm text-muted-foreground">
              <Spinner /> 正在读取待审图集
            </div>
          ) : requests.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>截图修改审核暂时无法读取</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(requests.error, "请稍后重试。")}
              </AlertDescription>
            </Alert>
          ) : requests.data.items.length === 0 ? (
            <p className="py-5 text-center text-sm text-muted-foreground">
              暂无等待审核的截图修改
            </p>
          ) : (
            <div className="divide-y rounded-md border">
              {requests.data.items.map((item) => (
                <div
                  key={item.change.id}
                  className="flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">
                      #{item.change.torrent_id} {item.torrent_title}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      用户 #{item.uploader_numeric_id} ·{" "}
                      {item.uploader_display_name || item.uploader_username} ·{" "}
                      {formatDateTime(item.change.created_at)}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {item.change.base_count} 张 →{" "}
                      {item.change.candidate_count} 张 · {item.change.reason}
                    </p>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => setTarget(item)}
                  >
                    对比并处理
                  </Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
      {target ? (
        <ScreenshotDecisionDialog
          item={target}
          csrfToken={csrfToken}
          onOpenChange={(open) => !open && setTarget(undefined)}
          onDecided={(message) => {
            setTarget(undefined)
            onDecided(message)
          }}
        />
      ) : null}
    </>
  )
}

function ScreenshotDecisionDialog({
  item,
  csrfToken,
  onOpenChange,
  onDecided,
}: {
  item: ManagedPublishedTorrentScreenshotChange
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onDecided: (message: string) => void
}) {
  const [reason, setReason] = React.useState("")
  const [decision, setDecision] = React.useState<"approve" | "reject">()
  const requestId = React.useRef<string | undefined>(undefined)
  const mutation = useDecidePublishedTorrentScreenshotChange()
  const reasonLength = Array.from(reason.trim()).length
  async function decide(next: "approve" | "reject") {
    if (reasonLength > 1000) return
    setDecision(next)
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      const result = await mutation.mutateAsync({
        requestId: item.change.id,
        csrfToken,
        idempotencyKey: requestId.current,
        body: {
          expected_request_version: item.change.version,
          decision: next,
          reason: reason.trim(),
        },
      })
      onDecided(
        result.decision === "approve"
          ? `种子 #${result.torrent_id} 的公开图集已切换。`
          : `种子 #${result.torrent_id} 的截图修改已驳回。`
      )
    } catch {
      setDecision(undefined)
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => !mutation.isPending && onOpenChange(open)}
    >
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-6xl">
        <DialogHeader>
          <DialogTitle>审核已发布截图修改</DialogTitle>
          <DialogDescription>
            #{item.change.torrent_id} {item.torrent_title} · 上传者 #
            {item.uploader_numeric_id}
          </DialogDescription>
        </DialogHeader>
        <Alert>
          <ImagesIcon />
          <AlertTitle>修改说明</AlertTitle>
          <AlertDescription>{item.change.reason}</AlertDescription>
        </Alert>
        {mutation.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>审核决定未提交</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(mutation.error, "请重新载入后重试。")}
            </AlertDescription>
          </Alert>
        ) : null}
        <div className="grid gap-4 lg:grid-cols-2">
          <ScreenshotSet
            title="当前公开图集"
            count={item.change.base_count}
            requestId={item.change.id}
            side="base"
          />
          <ScreenshotSet
            title="候选图集"
            count={item.change.candidate_count}
            requestId={item.change.id}
            side="candidate"
            candidate
          />
        </div>
        <Field data-invalid={reasonLength > 1000}>
          <FieldLabel htmlFor="screenshot-decision-reason">审核说明</FieldLabel>
          <Textarea
            id="screenshot-decision-reason"
            value={reason}
            maxLength={1000}
            rows={3}
            disabled={mutation.isPending}
            placeholder="记录图集、封面和图片质量的审核依据"
            onChange={(event) => {
              setReason(event.target.value)
              requestId.current = undefined
              mutation.reset()
            }}
          />
          <FieldDescription>
            {reasonLength} / 1000；留空时由系统自动生成审核说明
          </FieldDescription>
          {reasonLength > 1000 ? (
            <FieldError>审核说明不能超过 1000 个字符。</FieldError>
          ) : null}
        </Field>
        <DialogFooter>
          <Button
            type="button"
            variant="destructive"
            disabled={mutation.isPending || reasonLength > 1000}
            onClick={() => void decide("reject")}
          >
            {mutation.isPending && decision === "reject" ? (
              <Spinner />
            ) : (
              <XIcon data-icon="inline-start" />
            )}
            驳回修改
          </Button>
          <Button
            type="button"
            disabled={mutation.isPending || reasonLength > 1000}
            onClick={() => void decide("approve")}
          >
            {mutation.isPending && decision === "approve" ? (
              <Spinner />
            ) : (
              <CheckIcon data-icon="inline-start" />
            )}
            批准并切换
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ScreenshotSet({
  title,
  count,
  requestId,
  side,
  candidate = false,
}: {
  title: string
  count: number
  requestId: string
  side: "base" | "candidate"
  candidate?: boolean
}) {
  return (
    <section className="overflow-hidden rounded-lg border">
      <header className="flex items-center justify-between border-b bg-muted/40 px-3 py-2">
        <h3 className="text-sm font-medium">{title}</h3>
        <Badge variant={candidate ? "default" : "outline"}>{count} 张</Badge>
      </header>
      {count === 0 ? (
        <p className="p-8 text-center text-sm text-muted-foreground">空图集</p>
      ) : (
        <div className="grid grid-cols-2 gap-2 p-3 sm:grid-cols-3">
          {Array.from({ length: count }, (_, index) => (
            <img
              key={index}
              src={resolveApiUrl(
                `/api/v1/admin/torrents/screenshot-changes/${requestId}/images/${side}/${index}`
              )}
              alt={`${title} ${index + 1}`}
              className="aspect-video w-full rounded-md border bg-muted object-cover"
            />
          ))}
        </div>
      )}
    </section>
  )
}
