import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CheckIcon,
  CircleAlertIcon,
  FileDiffIcon,
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
  managedPublishedTorrentContentChangesQueryOptions,
  type ManagedPublishedTorrentContentChange,
  useDecidePublishedTorrentContentChange,
} from "~/features/staff/api/torrent-administration.queries"
import { formatDateTime } from "~/shared/formatters/date-time"
import { requestErrorDescription } from "~/shared/api/problem"

export function PublishedContentChangeReview({
  enabled,
  csrfToken,
  onDecided,
}: {
  enabled: boolean
  csrfToken: string
  onDecided: (message: string) => void
}) {
  const requests = useQuery({
    ...managedPublishedTorrentContentChangesQueryOptions,
    enabled,
  })
  const [target, setTarget] =
    React.useState<ManagedPublishedTorrentContentChange>()

  if (!enabled) return null

  return (
    <>
      <Card className="gap-0 py-0">
        <CardHeader className="flex flex-row items-center justify-between gap-3 p-5 pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <FileDiffIcon className="size-4" />
            已发布内容修改审核
            {requests.data ? (
              <Badge variant="secondary">{requests.data.total}</Badge>
            ) : null}
          </CardTitle>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="刷新内容修改审核"
            disabled={requests.isFetching}
            onClick={() => void requests.refetch()}
          >
            <RefreshCwIcon />
          </Button>
        </CardHeader>
        <CardContent className="p-5 pt-0">
          {requests.isPending ? (
            <div className="flex min-h-20 items-center justify-center gap-2 text-sm text-muted-foreground">
              <Spinner /> 正在读取待审修改
            </div>
          ) : requests.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>内容修改审核暂时无法读取</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(requests.error, "请稍后重试。")}
              </AlertDescription>
            </Alert>
          ) : requests.data.items.length === 0 ? (
            <p className="py-5 text-center text-sm text-muted-foreground">
              暂无等待审核的已发布内容修改
            </p>
          ) : (
            <div className="divide-y rounded-md border">
              {requests.data.items.map((item) => (
                <div
                  key={item.change.id}
                  className="flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0">
                    <Link
                      to={`/torrents/${item.change.torrent_id}`}
                      className="block truncate text-sm font-medium hover:text-primary hover:underline"
                    >
                      #{item.change.torrent_id} {item.torrent_title}
                    </Link>
                    <p className="mt-1 text-xs text-muted-foreground">
                      用户 #{item.uploader_numeric_id} ·{" "}
                      {item.uploader_display_name || item.uploader_username} ·{" "}
                      {formatDateTime(item.change.created_at)}
                    </p>
                    <p className="mt-1 line-clamp-1 text-xs text-muted-foreground">
                      {item.change.reason}
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
        <PublishedContentChangeDecisionDialog
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

function PublishedContentChangeDecisionDialog({
  item,
  csrfToken,
  onOpenChange,
  onDecided,
}: {
  item: ManagedPublishedTorrentContentChange
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onDecided: (message: string) => void
}) {
  const reasonId = React.useId()
  const [reason, setReason] = React.useState("")
  const [decision, setDecision] = React.useState<"approve" | "reject">()
  const requestId = React.useRef<string | undefined>(undefined)
  const mutation = useDecidePublishedTorrentContentChange()
  const reasonLength = Array.from(reason.trim()).length

  async function decide(nextDecision: "approve" | "reject") {
    if (reasonLength > 1000) return
    setDecision(nextDecision)
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      const result = await mutation.mutateAsync({
        requestId: item.change.id,
        csrfToken,
        idempotencyKey: requestId.current,
        body: {
          expected_request_version: item.change.version,
          decision: nextDecision,
          reason: reason.trim(),
        },
      })
      onDecided(
        result.decision === "approve"
          ? `种子 #${result.torrent_id} 的公开内容已切换到审核版本。`
          : `种子 #${result.torrent_id} 的内容修改已驳回，原公开版本保持不变。`
      )
    } catch {
      setDecision(undefined)
    }
  }

  const errorMessage = mutation.isError
    ? requestErrorDescription(
        mutation.error,
        "审核决定提交失败，请重新载入后重试。"
      )
    : ""

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onOpenChange(false)
      }}
    >
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>审核已发布内容修改</DialogTitle>
          <DialogDescription>
            #{item.change.torrent_id} {item.torrent_title} · 上传者 #
            {item.uploader_numeric_id}
          </DialogDescription>
        </DialogHeader>

        <Alert>
          <FileDiffIcon />
          <AlertTitle>修改说明</AlertTitle>
          <AlertDescription>{item.change.reason}</AlertDescription>
        </Alert>

        {errorMessage ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>审核决定未提交</AlertTitle>
            <AlertDescription>{errorMessage}</AlertDescription>
          </Alert>
        ) : null}

        <div className="mt-4 grid gap-4 lg:grid-cols-2">
          <ContentSnapshot title="当前公开版本" snapshot={item.change.base} />
          <ContentSnapshot
            title="候选修改版本"
            snapshot={item.change.candidate}
            candidate
          />
        </div>

        <Field className="mt-4" data-invalid={reasonLength > 1000}>
          <FieldLabel htmlFor={reasonId}>审核说明</FieldLabel>
          <Textarea
            id={reasonId}
            value={reason}
            maxLength={1000}
            rows={3}
            disabled={mutation.isPending}
            aria-invalid={reasonLength > 1000}
            placeholder="可留空；系统自动记录，或填写批准依据与修改建议"
            onChange={(event) => {
              setReason(event.target.value)
              requestId.current = undefined
              if (mutation.isError) mutation.reset()
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

function ContentSnapshot({
  title,
  snapshot,
  candidate = false,
}: {
  title: string
  snapshot: ManagedPublishedTorrentContentChange["change"]["base"]
  candidate?: boolean
}) {
  return (
    <section className="min-w-0 rounded-lg border">
      <header className="flex items-center justify-between border-b bg-muted/40 px-3 py-2">
        <h3 className="text-sm font-medium">{title}</h3>
        {candidate ? (
          <Badge>待审核</Badge>
        ) : (
          <Badge variant="outline">线上</Badge>
        )}
      </header>
      <div className="flex flex-col gap-3 p-3 text-xs">
        <SnapshotField label="简介" value={snapshot.description} />
        <SnapshotField
          label="MediaInfo"
          value={snapshot.media_info || "（空）"}
          mono
        />
        <SnapshotField
          label="外部资料"
          value={
            snapshot.external_identifiers.length
              ? snapshot.external_identifiers
                  .map(
                    (item) =>
                      `${item.provider.toUpperCase()}: ${item.external_id}`
                  )
                  .join("\n")
              : "（空）"
          }
        />
      </div>
    </section>
  )
}

function SnapshotField({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div>
      <h4 className="mb-1 font-medium text-muted-foreground">{label}</h4>
      <pre
        className={`max-h-48 overflow-auto rounded bg-muted/50 p-2 break-words whitespace-pre-wrap ${mono ? "font-mono" : "font-sans"}`}
      >
        {value}
      </pre>
    </div>
  )
}
