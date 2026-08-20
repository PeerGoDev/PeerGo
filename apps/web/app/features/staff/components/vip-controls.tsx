import * as React from "react"
import {
  CrownIcon,
  HistoryIcon,
  ShieldCheckIcon,
  XCircleIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
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
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Spinner } from "~/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type ManagedUserDetail,
  useChangeVIP,
} from "~/features/staff/api/user-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

type VIPDuration = "30" | "90" | "365" | "permanent"

export function VIPControls({
  detail,
  csrfToken,
  currentStaffUserId,
  canManage,
}: {
  detail: ManagedUserDetail
  csrfToken: string
  currentStaffUserId: string
  canManage: boolean
}) {
  const [mode, setMode] = React.useState<"grant" | "revoke">()
  const state = detail.vip_state
  const isSelf = detail.id === currentStaffUserId

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="font-heading font-medium">VIP 身份</h2>
          <p className="text-xs text-muted-foreground">
            变更会同步分享率豁免和做种奖励权益时间线。
          </p>
        </div>
        <Badge variant={state.active ? "secondary" : "outline"}>
          {state.active ? "生效中" : state.enabled ? "已到期" : "非 VIP"}
        </Badge>
      </div>

      {state.active ? (
        <Alert>
          <CrownIcon />
          <AlertTitle>{state.until ? "限期 VIP" : "永久 VIP"}</AlertTitle>
          <AlertDescription>
            {state.until
              ? `有效至 ${formatDateTime(state.until)}`
              : "当前没有到期时间"}
            {` · 状态版本 ${state.version}`}
          </AlertDescription>
        </Alert>
      ) : (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>
            {state.enabled ? "VIP 已到期" : "当前不是 VIP"}
          </AlertTitle>
          <AlertDescription>
            {state.until
              ? `原有效期截止 ${formatDateTime(state.until)}`
              : "当前不享受 VIP 权益。"}
          </AlertDescription>
        </Alert>
      )}

      {isSelf ? (
        <p className="text-xs text-muted-foreground">
          不能变更自己的 VIP，请由另一名管理员执行。
        </p>
      ) : canManage ? (
        <div className="flex flex-wrap gap-2">
          <Button size="sm" onClick={() => setMode("grant")}>
            <CrownIcon data-icon="inline-start" />
            {state.enabled ? "续期／改为永久" : "签发 VIP"}
          </Button>
          {state.enabled ? (
            <Button
              size="sm"
              variant="outline"
              onClick={() => setMode("revoke")}
            >
              <XCircleIcon data-icon="inline-start" />
              撤销 VIP
            </Button>
          ) : null}
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">当前权限仅可查看。</p>
      )}

      <VIPHistory detail={detail} />
      <VIPChangeDialog
        open={Boolean(mode)}
        mode={mode ?? "grant"}
        detail={detail}
        csrfToken={csrfToken}
        onOpenChange={(open) => {
          if (!open) setMode(undefined)
        }}
      />
    </section>
  )
}

function VIPHistory({ detail }: { detail: ManagedUserDetail }) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2 text-sm font-medium">
        <HistoryIcon aria-hidden="true" />
        VIP 历史
        <Badge variant="outline" className="tabular-nums">
          {detail.vip_history.length}
        </Badge>
      </div>
      {detail.vip_history.length === 0 ? (
        <p className="text-xs text-muted-foreground">暂无 VIP 变更记录。</p>
      ) : (
        <Table className="text-xs">
          <TableHeader>
            <TableRow>
              <TableHead>时间／操作</TableHead>
              <TableHead>期限</TableHead>
              <TableHead>理由</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {detail.vip_history.map((item) => (
              <TableRow key={`${item.state_version}-${item.occurred_at}`}>
                <TableCell>
                  <div>{vipTransitionLabel(item.transition)}</div>
                  <div className="text-muted-foreground">
                    {formatDateTime(item.occurred_at)}
                  </div>
                </TableCell>
                <TableCell>
                  {item.enabled
                    ? item.until
                      ? formatDateTime(item.until)
                      : "永久"
                    : "已撤销"}
                </TableCell>
                <TableCell className="max-w-48 whitespace-normal">
                  {item.reason_summary}
                  {item.actor_username ? (
                    <div className="text-muted-foreground">
                      @{item.actor_username}
                    </div>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

function VIPChangeDialog({
  open,
  mode,
  detail,
  csrfToken,
  onOpenChange,
}: {
  open: boolean
  mode: "grant" | "revoke"
  detail: ManagedUserDetail
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const mutation = useChangeVIP()
  const [duration, setDuration] = React.useState<VIPDuration>("30")
  const [reason, setReason] = React.useState("")
  const [validationError, setValidationError] = React.useState<string>()

  React.useEffect(() => {
    if (!open) {
      setDuration("30")
      setReason("")
      setValidationError(undefined)
      mutation.reset()
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  async function handleSubmit() {
    const normalizedReason = reason.trim()
    if (normalizedReason.length < 10 || normalizedReason.length > 500) {
      setValidationError("请填写 10–500 个字符的人工理由。")
      return
    }
    setValidationError(undefined)
    await mutation.mutateAsync({
      userId: detail.id,
      csrfToken,
      body: {
        enabled: mode === "grant",
        duration_days:
          mode === "grant" && duration !== "permanent"
            ? Number(duration)
            : undefined,
        reason: normalizedReason,
        expected_user_version: detail.version,
        expected_state_version: detail.vip_state.version,
      },
    })
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {mode === "grant" ? "签发或续期 VIP" : "撤销 VIP"}
          </DialogTitle>
          <DialogDescription>
            操作会写入不可变历史；做种奖励从下一个结算小时按新状态计算。
          </DialogDescription>
        </DialogHeader>
        <FieldGroup>
          {mode === "grant" ? (
            <Field>
              <FieldLabel id="vip-duration-label">有效期</FieldLabel>
              <ToggleGroup
                value={[duration]}
                onValueChange={(values) => {
                  const selected = values[0] as VIPDuration | undefined
                  if (selected) setDuration(selected)
                }}
                variant="outline"
                spacing={0}
                aria-labelledby="vip-duration-label"
                className="w-full flex-wrap"
                disabled={mutation.isPending}
              >
                <ToggleGroupItem value="30" className="flex-1">
                  30 天
                </ToggleGroupItem>
                <ToggleGroupItem value="90" className="flex-1">
                  90 天
                </ToggleGroupItem>
                <ToggleGroupItem value="365" className="flex-1">
                  1 年
                </ToggleGroupItem>
                <ToggleGroupItem value="permanent" className="flex-1">
                  永久
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
          ) : null}
          <Field data-invalid={Boolean(validationError || mutation.isError)}>
            <FieldLabel htmlFor="vip-change-reason">人工理由</FieldLabel>
            <Textarea
              id="vip-change-reason"
              value={reason}
              minLength={10}
              maxLength={500}
              rows={4}
              placeholder={
                mode === "grant"
                  ? "记录签发依据（10–500 字）…"
                  : "记录撤销依据（10–500 字）…"
              }
              onChange={(event) => {
                setReason(event.target.value)
                setValidationError(undefined)
                mutation.reset()
              }}
              aria-invalid={Boolean(validationError || mutation.isError)}
            />
            {validationError ? (
              <FieldError>{validationError}</FieldError>
            ) : null}
            {mutation.isError ? (
              <FieldError>
                {requestErrorDescription(
                  mutation.error,
                  "VIP 变更失败，请刷新账户详情后重试。"
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
          <Button
            variant={mode === "revoke" ? "destructive" : "default"}
            disabled={mutation.isPending}
            onClick={() => void handleSubmit()}
          >
            {mutation.isPending ? <Spinner data-icon="inline-start" /> : null}
            {mutation.isPending
              ? "正在提交…"
              : mode === "grant"
                ? "确认签发"
                : "确认撤销"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function vipTransitionLabel(
  value: ManagedUserDetail["vip_history"][number]["transition"]
) {
  switch (value) {
    case "granted":
      return "签发"
    case "renewed":
      return "续期／变更期限"
    case "revoked":
      return "撤销"
  }
}
