import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  Clock3Icon,
  GraduationCapIcon,
  PlusIcon,
  RefreshCwIcon,
  SparklesIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "~/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import {
  type NewcomerPolicyPage,
  newcomerPolicyListQueryOptions,
  useIssueNewcomerPolicy,
} from "~/features/staff/api/newcomer-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

const gibibyte = 1024 ** 3

export function NewcomerPolicySettingsCard({
  csrfToken,
  canIssue,
}: {
  csrfToken: string
  canIssue: boolean
}) {
  const policies = useQuery(newcomerPolicyListQueryOptions())
  const [dialogOpen, setDialogOpen] = React.useState(false)

  if (policies.isPending) {
    return (
      <Card>
        <CardContent className="flex min-h-32 items-center justify-center">
          <Spinner />
        </CardContent>
      </Card>
    )
  }
  if (policies.isError || !policies.data) {
    return (
      <Card>
        <CardContent>
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>新人考核规则暂时无法读取</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(
                policies.error,
                "请检查后台登录状态后重试。"
              )}
            </AlertDescription>
            <AlertAction>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void policies.refetch()}
              >
                <RefreshCwIcon data-icon="inline-start" />
                重试
              </Button>
            </AlertAction>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  const current = policies.data.current
  const nextScheduled = [...policies.data.items]
    .filter((item) => item.timeline_state === "scheduled")
    .sort(
      (left, right) =>
        Date.parse(left.effective_at) - Date.parse(right.effective_at)
    )[0]
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="min-h-[88px] content-center items-center py-6">
        <div className="flex min-w-0 flex-col gap-1">
          <CardTitle className="flex items-center gap-2 text-xl">
            <GraduationCapIcon />
            新人考核
          </CardTitle>
          <CardDescription>
            只分配给规则生效后完成注册的新用户，不追溯旧站迁移用户。
          </CardDescription>
        </div>
        {canIssue ? (
          <CardAction>
            <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
              <DialogTrigger render={<Button size="sm" />}>
                <PlusIcon data-icon="inline-start" />
                签发新版本
              </DialogTrigger>
              <NewcomerPolicyDialogContent
                current={current}
                minimumEffectiveFrom={policies.data.minimum_effective_from}
                csrfToken={csrfToken}
                onSaved={() => setDialogOpen(false)}
              />
            </Dialog>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className="flex flex-col gap-5 pb-6">
        <Alert>
          <Clock3Icon />
          <AlertTitle>
            {current?.enabled ? "新人考核已启用" : "新人考核当前关闭"}
          </AlertTitle>
          <AlertDescription>
            {current?.enabled
              ? `新注册用户有 ${Math.floor(current.duration_seconds / 86400)} 天完成两项要求。`
              : "关闭期间完成注册的用户不会在以后补分配考核。"}
          </AlertDescription>
        </Alert>
        <Alert>
          <GraduationCapIcon />
          <AlertTitle>当前采用两项可信任务</AlertTitle>
          <AlertDescription>
            有效上传只认优惠结算后的最终流量，做种时长只认已经完成的证据窗口。不会要求新人制造下载量、刷魔力值或灌水发种；到期未达标只限制新下载，继续做种达标后自动恢复。
          </AlertDescription>
        </Alert>
        <Alert>
          <SparklesIcon />
          <AlertTitle>VIP 沿用 Rousi 的一次性豁免</AlertTitle>
          <AlertDescription>
            注册完成时有效的 VIP 不会被分配考核；考核中的成员获签 VIP
            会立即豁免。VIP
            到期或撤销后不会重新创建考核，等级、工作组和其它身份不会隐式跳过。
          </AlertDescription>
        </Alert>
        {current ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <PolicyFact label="当前规则" value={`第 ${current.revision} 版`} />
            <PolicyFact
              label="有效上传"
              value={formatBytes(current.minimum_credited_upload_bytes)}
            />
            <PolicyFact
              label="做种时长"
              value={`${Math.floor(current.minimum_seeding_active_seconds / 3600)} 小时`}
            />
            <PolicyFact
              label="生效时间"
              value={
                current.source_kind === "opening"
                  ? "初始关闭"
                  : formatCompactDateTime(current.effective_at)
              }
            />
          </div>
        ) : null}
        {nextScheduled ? (
          <Alert>
            <Clock3Icon />
            <AlertTitle>
              下一规则第 {nextScheduled.revision} 版已排期
            </AlertTitle>
            <AlertDescription>
              {formatCompactDateTime(nextScheduled.effective_at)}生效；
              {nextScheduled.enabled
                ? `${Math.floor(nextScheduled.duration_seconds / 86400)} 天内完成 ${formatBytes(nextScheduled.minimum_credited_upload_bytes)} 有效上传与 ${Math.floor(nextScheduled.minimum_seeding_active_seconds / 3600)} 小时做种。`
                : "届时停止向之后完成注册的用户分配新人考核。"}
            </AlertDescription>
          </Alert>
        ) : null}
        <div className="flex flex-wrap gap-2">
          <Badge variant="outline">进行中 {policies.data.summary.active}</Badge>
          <Badge variant="destructive">
            下载受限 {policies.data.summary.download_restricted}
          </Badge>
          <Badge variant="secondary">
            已通过 {policies.data.summary.passed}
          </Badge>
          <Badge variant="outline">
            已豁免 {policies.data.summary.exempted}
          </Badge>
        </div>
        <div className="flex flex-wrap items-center gap-2 border-t pt-4 text-sm">
          <span className="font-medium">自动巡检</span>
          <Badge
            variant={
              policies.data.worker.last_error_code ? "destructive" : "secondary"
            }
          >
            {policies.data.worker.last_error_code
              ? "运行异常"
              : policies.data.worker.last_completed_at
                ? "运行正常"
                : "等待首次运行"}
          </Badge>
          <span className="text-muted-foreground">
            {policies.data.worker.last_completed_at
              ? `${formatCompactDateTime(policies.data.worker.last_completed_at)} · 检查 ${policies.data.worker.last_examined} 条 · 转换 ${policies.data.worker.last_transitioned} 条`
              : "统一 Policy Worker 启动后会自动更新进度。"}
          </span>
          {policies.data.worker.last_error_code ? (
            <code className="text-xs text-destructive">
              {policies.data.worker.last_error_code}
            </code>
          ) : null}
        </div>
      </CardContent>
    </Card>
  )
}

function NewcomerPolicyDialogContent({
  current,
  minimumEffectiveFrom,
  csrfToken,
  onSaved,
}: {
  current: NewcomerPolicyPage["current"]
  minimumEffectiveFrom: string
  csrfToken: string
  onSaved: () => void
}) {
  const [enabled, setEnabled] = React.useState(current?.enabled ?? true)
  const [days, setDays] = React.useState(
    String(Math.floor((current?.duration_seconds ?? 2_592_000) / 86_400))
  )
  const [uploadGiB, setUploadGiB] = React.useState(
    String(
      current?.enabled
        ? Number(BigInt(current.minimum_credited_upload_bytes)) / gibibyte
        : 50
    )
  )
  const [seedingHours, setSeedingHours] = React.useState(
    String(
      current?.enabled ? current.minimum_seeding_active_seconds / 3600 : 72
    )
  )
  const [effectiveAt, setEffectiveAt] = React.useState(() =>
    localDateTimeValue(new Date(Date.parse(minimumEffectiveFrom) + 60_000))
  )
  const [reason, setReason] = React.useState("")
  const [idempotencyKey] = React.useState(() => crypto.randomUUID())
  const mutation = useIssueNewcomerPolicy()
  const reasonLength = Array.from(reason.trim()).length
  const valid =
    Number(days) >= 7 &&
    Number(days) <= 90 &&
    Number(uploadGiB) >= 0 &&
    Number(seedingHours) >= 0 &&
    (!enabled || Number(uploadGiB) > 0 || Number(seedingHours) > 0) &&
    reasonLength <= 1000 &&
    Date.parse(effectiveAt) >= Date.parse(minimumEffectiveFrom)

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!valid) return
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey,
        policy: {
          enabled,
          duration_seconds: Math.round(Number(days) * 86400),
          minimum_credited_upload_bytes: enabled
            ? String(Math.round(Number(uploadGiB) * gibibyte))
            : "0",
          minimum_seeding_active_seconds: enabled
            ? Math.round(Number(seedingHours) * 3600)
            : 0,
        },
        effectiveAt: new Date(effectiveAt).toISOString(),
        reason: reason.trim(),
      })
      onSaved()
    } catch {
      // Keep reviewed values for retry.
    }
  }

  return (
    <DialogContent className="sm:max-w-xl">
      <form onSubmit={handleSubmit}>
        <DialogHeader>
          <DialogTitle>签发新人考核规则</DialogTitle>
          <DialogDescription>
            新版本只影响生效后完成注册的用户，已有考核继续使用原规则。
          </DialogDescription>
        </DialogHeader>
        <FieldGroup className="py-5">
          <Field orientation="horizontal">
            <div className="flex flex-1 flex-col gap-1">
              <FieldTitle>启用新人考核</FieldTitle>
              <FieldDescription>
                关闭时仍会保留历史考核和已产生的状态。
              </FieldDescription>
            </div>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </Field>
          <div className="grid gap-4 sm:grid-cols-3">
            <Field>
              <FieldLabel htmlFor="newcomer-duration">考核天数</FieldLabel>
              <Input
                id="newcomer-duration"
                type="number"
                min={7}
                max={90}
                value={days}
                onChange={(event) => setDays(event.target.value)}
              />
            </Field>
            <Field data-disabled={!enabled}>
              <FieldLabel htmlFor="newcomer-upload">有效上传 GiB</FieldLabel>
              <Input
                id="newcomer-upload"
                type="number"
                min={0}
                step={1}
                value={uploadGiB}
                disabled={!enabled}
                onChange={(event) => setUploadGiB(event.target.value)}
              />
            </Field>
            <Field data-disabled={!enabled}>
              <FieldLabel htmlFor="newcomer-seeding">做种小时</FieldLabel>
              <Input
                id="newcomer-seeding"
                type="number"
                min={0}
                step={1}
                value={seedingHours}
                disabled={!enabled}
                onChange={(event) => setSeedingHours(event.target.value)}
              />
            </Field>
          </div>
          {enabled ? (
            <div className="flex flex-col gap-2 rounded-lg border bg-muted/30 p-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0 text-sm">
                <p className="font-medium">推荐初始基线</p>
                <p className="text-muted-foreground">
                  30 天、50 GiB 有效上传、72
                  小时做种；上线后再按真实完成率调整。
                </p>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  setDays("30")
                  setUploadGiB("50")
                  setSeedingHours("72")
                }}
              >
                <SparklesIcon data-icon="inline-start" />
                应用推荐值
              </Button>
            </div>
          ) : null}
          <Field>
            <FieldLabel htmlFor="newcomer-effective-at">生效时间</FieldLabel>
            <Input
              id="newcomer-effective-at"
              type="datetime-local"
              value={effectiveAt}
              onChange={(event) => setEffectiveAt(event.target.value)}
            />
            <FieldDescription>
              至少提前 5 分钟，最长可排期一年。
            </FieldDescription>
          </Field>
          <Field
            data-invalid={reasonLength > 1000 || mutation.isError || undefined}
          >
            <FieldLabel htmlFor="newcomer-policy-reason">调整原因</FieldLabel>
            <Textarea
              id="newcomer-policy-reason"
              value={reason}
              maxLength={1000}
              aria-invalid={reasonLength > 1000 || mutation.isError}
              onChange={(event) => {
                setReason(event.target.value)
                mutation.reset()
              }}
              placeholder="可留空；系统会自动记录本次调整原因"
            />
            <FieldDescription>{reasonLength}/1000 个字符</FieldDescription>
            {mutation.isError ? (
              <FieldError>
                {requestErrorDescription(
                  mutation.error,
                  "规则签发失败，请刷新当前版本后重试。"
                )}
              </FieldError>
            ) : null}
          </Field>
        </FieldGroup>
        <DialogFooter>
          <DialogClose render={<Button variant="outline" />}>取消</DialogClose>
          <Button type="submit" disabled={!valid || mutation.isPending}>
            {mutation.isPending ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <PlusIcon data-icon="inline-start" />
            )}
            签发未来版本
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  )
}

function PolicyFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="font-medium tabular-nums">{value}</span>
    </div>
  )
}

function localDateTimeValue(date: Date) {
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}
