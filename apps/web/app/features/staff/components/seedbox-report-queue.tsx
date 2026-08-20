import { useState } from "react"
import { CheckIcon, RefreshCwIcon, XIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Field, FieldError, FieldLabel } from "~/components/ui/field"
import { Skeleton } from "~/components/ui/skeleton"
import { Textarea } from "~/components/ui/textarea"
import {
  type SeedboxReport,
  type SeedboxReportPage,
  useDecideSeedboxReport,
} from "~/features/seedbox/api/seedbox.queries"
import { ApiProblemError } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

export function SeedboxReportQueue({
  data,
  isPending,
  isError,
  canRead,
  canDecide,
  csrfToken,
  refresh,
}: {
  data?: SeedboxReportPage
  isPending: boolean
  isError: boolean
  canRead: boolean
  canDecide: boolean
  csrfToken: string
  refresh: () => void
}) {
  if (!canRead) {
    return (
      <Alert>
        <AlertTitle>当前后台身份不能查看盒子申报</AlertTitle>
        <AlertDescription>
          盒子政策和用户地址审核是两个独立权限，不会因为能修改 Tracker
          设置就自动显示用户地址。
        </AlertDescription>
      </Alert>
    )
  }
  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-3">
        <div>
          <CardTitle>用户盒子申报</CardTitle>
          <CardDescription>
            仅批准单个主机地址；批准后生成只绑定该用户数字 ID 的 Tracker 规则。
          </CardDescription>
        </div>
        <Button variant="outline" size="sm" onClick={refresh}>
          <RefreshCwIcon data-icon="inline-start" />
          刷新
        </Button>
      </CardHeader>
      <CardContent className="space-y-3">
        {isPending ? <Skeleton className="h-32 w-full" /> : null}
        {isError ? (
          <Alert variant="destructive">
            <AlertTitle>盒子申报暂时无法读取</AlertTitle>
            <AlertDescription>
              请检查 Core 服务和后台登录状态后重试。
            </AlertDescription>
          </Alert>
        ) : null}
        {!isPending && !isError && data?.items.length === 0 ? (
          <p className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
            当前没有盒子申报记录。
          </p>
        ) : null}
        {data?.items.map((report) => (
          <SeedboxReportItem
            key={report.id}
            report={report}
            csrfToken={csrfToken}
            canDecide={canDecide}
          />
        ))}
      </CardContent>
    </Card>
  )
}

function SeedboxReportItem({
  report,
  csrfToken,
  canDecide,
}: {
  report: SeedboxReport
  csrfToken: string
  canDecide: boolean
}) {
  const mutation = useDecideSeedboxReport()
  const [reason, setReason] = useState("")
  const [error, setError] = useState("")
  const pending = report.status === "pending"

  async function decide(decision: "approve" | "reject") {
    if (reason.trim().length < 10) {
      setError("请填写至少 10 个字符的审核说明。")
      return
    }
    setError("")
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        reportId: report.id,
        body: {
          expected_version: report.version,
          decision,
          reason: reason.trim(),
        },
      })
    } catch (caught) {
      setError(
        caught instanceof ApiProblemError
          ? caught.detail || caught.message
          : "盒子审核失败，请刷新后重试。"
      )
    }
  }

  return (
    <section className="rounded-md border p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <strong>{report.username}</strong>
            <span className="text-sm text-muted-foreground">
              ID {report.user_numeric_id}
            </span>
            <ReportStatus status={report.status} />
          </div>
          <p className="mt-1 font-mono text-sm">{report.address}</p>
          <p className="mt-1 text-sm text-muted-foreground">
            {report.provider} · {report.bandwidth_mbps.toLocaleString("zh-CN")}{" "}
            Mbps · 提交于 {formatDateTime(report.submitted_at)}
          </p>
        </div>
      </div>
      <p className="mt-3 text-sm whitespace-pre-wrap">{report.statement}</p>
      {report.decision_reason ? (
        <p className="mt-3 rounded-md bg-muted px-3 py-2 text-sm">
          审核说明：{report.decision_reason}
        </p>
      ) : null}
      {pending ? (
        <div className="mt-4 space-y-3">
          <Field data-invalid={Boolean(error)}>
            <FieldLabel htmlFor={`seedbox-reason-${report.id}`}>
              审核说明
            </FieldLabel>
            <Textarea
              id={`seedbox-reason-${report.id}`}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              rows={2}
              disabled={!canDecide || mutation.isPending}
              placeholder="说明核验依据、服务商和地址归属"
            />
            {error ? <FieldError>{error}</FieldError> : null}
          </Field>
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={!canDecide || mutation.isPending}
              onClick={() => void decide("reject")}
            >
              <XIcon data-icon="inline-start" />
              驳回
            </Button>
            <Button
              type="button"
              disabled={!canDecide || mutation.isPending}
              onClick={() => void decide("approve")}
            >
              <CheckIcon data-icon="inline-start" />
              批准并签发
            </Button>
          </div>
        </div>
      ) : null}
    </section>
  )
}

function ReportStatus({ status }: { status: SeedboxReport["status"] }) {
  const values = {
    pending: { label: "待审核", variant: "secondary" as const },
    approved: { label: "已批准", variant: "outline" as const },
    rejected: { label: "已驳回", variant: "destructive" as const },
  }
  const value = values[status]
  return <Badge variant={value.variant}>{value.label}</Badge>
}
