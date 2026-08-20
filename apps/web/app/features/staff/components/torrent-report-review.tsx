import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  BanIcon,
  CheckIcon,
  CircleAlertIcon,
  FlagIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
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
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import {
  managedTorrentReportCasesQueryOptions,
  type ManagedTorrentReportCase,
  useDecideTorrentReportCase,
} from "~/features/staff/api/torrent-administration.queries"
import {
  torrentReportReasonLabel,
  torrentReportReasonOptions,
} from "~/features/torrent/model/torrent-report"
import type { components } from "~/generated/api"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

type Decision = components["schemas"]["TorrentReportDecision"]
type DecisionReason = components["schemas"]["TorrentReportDecisionReasonCode"]

export function TorrentReportReview({
  enabled,
  csrfToken,
  onDecided,
}: {
  enabled: boolean
  csrfToken: string
  onDecided: (message: string) => void
}) {
  const cases = useQuery({
    ...managedTorrentReportCasesQueryOptions,
    enabled,
  })
  const [target, setTarget] = React.useState<ManagedTorrentReportCase>()

  if (!enabled) return null

  return (
    <>
      <Card className="gap-0 py-0">
        <CardHeader className="flex flex-row items-center justify-between gap-3 p-5 pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <FlagIcon />
            种子举报审核
            {cases.data ? (
              <Badge variant="secondary">{cases.data.total}</Badge>
            ) : null}
          </CardTitle>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="刷新种子举报审核"
            disabled={cases.isFetching}
            onClick={() => void cases.refetch()}
          >
            <RefreshCwIcon />
          </Button>
        </CardHeader>
        <CardContent className="p-5 pt-0">
          {cases.isPending ? (
            <div className="flex min-h-20 items-center justify-center gap-2 text-sm text-muted-foreground">
              <Spinner /> 正在读取举报案件
            </div>
          ) : cases.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>举报案件暂时无法读取</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(cases.error, "请稍后重试。")}
              </AlertDescription>
            </Alert>
          ) : cases.data.items.length === 0 ? (
            <Empty className="min-h-28 border-0 py-4">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <CheckIcon />
                </EmptyMedia>
                <EmptyTitle>暂无待处理种子举报</EmptyTitle>
                <EmptyDescription>
                  多名成员针对同一种子的举报会聚合到一条案件中。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="divide-y rounded-md border">
              {cases.data.items.map((item) => (
                <div
                  key={item.id}
                  className="flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">
                      #{item.torrent_id} {item.torrent_title}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      上传者 #{item.uploader_numeric_id} ·{" "}
                      {item.uploader_display_name || item.uploader_username} ·
                      最近举报 {formatDateTime(item.latest_reported_at)}
                    </p>
                    <p className="mt-1 line-clamp-1 text-xs text-muted-foreground">
                      {torrentReportReasonLabel(
                        item.reports.at(-1)?.reason_code ?? "other"
                      )}
                      {item.reports.at(-1)?.details
                        ? `：${item.reports.at(-1)?.details}`
                        : ""}
                    </p>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Badge variant="secondary">
                      {item.report_count} 人举报
                    </Badge>
                    {item.active_purchase_count > 0 ? (
                      <Badge variant="outline">
                        {item.active_purchase_count} 项购买权益
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
        <TorrentReportDecisionDialog
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

function TorrentReportDecisionDialog({
  item,
  csrfToken,
  onOpenChange,
  onDecided,
}: {
  item: ManagedTorrentReportCase
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onDecided: (message: string) => void
}) {
  const decisionFieldId = React.useId()
  const reasonFieldId = React.useId()
  const noteFieldId = React.useId()
  const canDisable = item.torrent_state === "published"
  const [decision, setDecision] = React.useState<Decision>("dismiss")
  const [reasonCode, setReasonCode] =
    React.useState<Exclude<DecisionReason, "no_violation">>("content_mismatch")
  const [note, setNote] = React.useState("")
  const requestId = React.useRef<string>(undefined)
  const mutation = useDecideTorrentReportCase()
  const noteCount = Array.from(note.trim()).length
  const noteInvalid = noteCount > 0 && (noteCount < 10 || noteCount > 1000)
  const decisionOptions: Array<{ value: Decision; label: string }> = [
    { value: "dismiss", label: "确认无违规并关闭" },
    ...(canDisable
      ? [{ value: "disable_torrent" as const, label: "确认违规并临时下架" }]
      : []),
  ]

  function resetAttempt() {
    requestId.current = undefined
    mutation.reset()
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (noteCount < 10 || noteCount > 1000) return
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      const result = await mutation.mutateAsync({
        caseId: item.id,
        csrfToken,
        idempotencyKey: requestId.current,
        body: {
          expected_case_version: item.version,
          expected_torrent_version: item.torrent_version,
          decision,
          reason_code: decision === "dismiss" ? "no_violation" : reasonCode,
          note: note.trim(),
        },
      })
      onDecided(
        result.decision === "disable_torrent"
          ? `种子 #${result.torrent_id} 已临时下架并停止 Tracker 准入。`
          : `种子 #${result.torrent_id} 的举报案件已按无违规关闭。`
      )
    } catch {
      // Preserve the decision UUID for an exact retry after response loss.
    }
  }

  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onOpenChange(false)
      }}
    >
      <AlertDialogContent className="sm:max-w-xl">
        <AlertDialogHeader>
          <AlertDialogMedia>
            <FlagIcon />
          </AlertDialogMedia>
          <AlertDialogTitle>审核种子举报</AlertDialogTitle>
          <AlertDialogDescription>
            #{item.torrent_id} · {item.torrent_title} · {item.report_count}{" "}
            人举报
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="max-h-52 overflow-y-auto rounded-md border p-3">
          <div className="flex flex-col gap-3">
            {item.reports.map((report, index) => (
              <div key={`${report.created_at}-${index}`} className="text-sm">
                <p className="font-medium">
                  {torrentReportReasonLabel(report.reason_code)}
                </p>
                <p className="mt-1 whitespace-pre-wrap text-muted-foreground">
                  {report.details || "未填写补充说明"}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {formatDateTime(report.created_at)}
                </p>
              </div>
            ))}
          </div>
        </div>

        <form id="torrent-report-decision-form" onSubmit={submit} noValidate>
          <FieldGroup>
            <Alert>
              <ShieldCheckIcon />
              <AlertTitle>审核队列不显示举报人</AlertTitle>
              <AlertDescription>
                处置只能关闭案件或临时下架种子；不会自动封禁上传者、退款、硬删文件或清理评论。
              </AlertDescription>
            </Alert>

            {item.active_purchase_count > 0 ? (
              <Alert>
                <CircleAlertIcon />
                <AlertTitle>存在有效购买权益</AlertTitle>
                <AlertDescription>
                  临时下架不会撤销这 {item.active_purchase_count}{" "}
                  项权益。若后续决定永久删除，仍需在购买记录中显式退款。
                </AlertDescription>
              </Alert>
            ) : null}

            <Field>
              <FieldLabel htmlFor={decisionFieldId}>处置结果</FieldLabel>
              <Select
                items={decisionOptions}
                value={decision}
                disabled={mutation.isPending}
                onValueChange={(value) => {
                  if (!value) return
                  setDecision(value)
                  resetAttempt()
                }}
              >
                <SelectTrigger id={decisionFieldId} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectLabel>案件处置</SelectLabel>
                    {decisionOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {!canDisable ? (
                <FieldDescription>
                  种子当前为 {item.torrent_state}
                  ，状态已被其他流程改变，本案只能核对后关闭。
                </FieldDescription>
              ) : null}
            </Field>

            {decision === "disable_torrent" ? (
              <Field>
                <FieldLabel htmlFor={reasonFieldId}>确认的违规类别</FieldLabel>
                <Select
                  items={torrentReportReasonOptions}
                  value={reasonCode}
                  disabled={mutation.isPending}
                  onValueChange={(value) => {
                    if (!value) return
                    setReasonCode(value)
                    resetAttempt()
                  }}
                >
                  <SelectTrigger id={reasonFieldId} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectLabel>下架原因</SelectLabel>
                      {torrentReportReasonOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            ) : null}

            {decision === "disable_torrent" ? (
              <Alert variant="destructive">
                <BanIcon />
                <AlertTitle>种子将立即停止公开和 Tracker 准入</AlertTitle>
                <AlertDescription>
                  原始
                  .torrent、图片、评论和购买记录会继续保留，可由后续独立流程恢复或进一步处置。
                </AlertDescription>
              </Alert>
            ) : null}

            <Field data-invalid={noteInvalid || undefined}>
              <FieldLabel htmlFor={noteFieldId}>内部处置说明</FieldLabel>
              <Textarea
                id={noteFieldId}
                value={note}
                rows={4}
                minLength={10}
                maxLength={1001}
                disabled={mutation.isPending}
                aria-invalid={noteInvalid || undefined}
                placeholder="记录核对依据和处置边界，至少 10 个字符。"
                onChange={(event) => {
                  setNote(event.target.value)
                  resetAttempt()
                }}
              />
              <FieldDescription>
                {noteCount}/1000，至少 10 个字符
              </FieldDescription>
              {noteInvalid ? (
                <FieldError>内部说明需要 10–1000 个字符。</FieldError>
              ) : null}
            </Field>

            {mutation.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>处置未能保存</AlertTitle>
                <AlertDescription>
                  {requestErrorDescription(
                    mutation.error,
                    "请刷新举报队列并核对最新状态。"
                  )}
                </AlertDescription>
              </Alert>
            ) : null}
          </FieldGroup>
        </form>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            取消
          </AlertDialogCancel>
          <AlertDialogAction
            type="submit"
            form="torrent-report-decision-form"
            variant={decision === "disable_torrent" ? "destructive" : "default"}
            disabled={mutation.isPending || noteCount < 10 || noteCount > 1000}
          >
            {mutation.isPending ? <Spinner data-icon="inline-start" /> : null}
            {decision === "disable_torrent" ? "确认临时下架" : "确认关闭案件"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
