import * as React from "react"
import {
  CalculatorIcon,
  CircleAlertIcon,
  Clock3Icon,
  LoaderCircleIcon,
  SaveIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
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
  FieldLegend,
  FieldSet,
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
import {
  type HNRPolicyPreview,
  useIssueHNRPolicy,
  usePreviewHNRPolicy,
} from "~/features/staff/api/hnr-policy-administration.queries"
import {
  hnrPolicyDraftFromCurrent,
  hnrPolicyFromDraft,
  type HNRPolicyDraft,
  type HNRPolicySettings,
} from "~/features/staff/model/hnr-policy-form"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

const modeItems = [
  { value: "enforced", label: "启用 H&R" },
  { value: "exempt", label: "全站豁免" },
  { value: "disabled", label: "停用 H&R" },
] as const

export function HNRPolicyDialog({
  open,
  current,
  minimumEffectiveFrom,
  csrfToken,
  onOpenChange,
  onIssued,
}: {
  open: boolean
  current: HNRPolicySettings
  minimumEffectiveFrom: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onIssued: (message: string) => void
}) {
  const [draft, setDraft] = React.useState<HNRPolicyDraft>(() =>
    hnrPolicyDraftFromCurrent(current)
  )
  const [effectiveAt, setEffectiveAt] = React.useState(() =>
    defaultEffectiveAt(minimumEffectiveFrom)
  )
  const [reason, setReason] = React.useState("")
  const [preview, setPreview] = React.useState<HNRPolicyPreview>()
  const previewMutation = usePreviewHNRPolicy()
  const issueMutation = useIssueHNRPolicy()
  const policy = hnrPolicyFromDraft(draft)
  const reasonLength = Array.from(reason.trim()).length
  const minimumTimestamp = new Date(minimumEffectiveFrom).getTime()
  const effectiveTimestamp = new Date(effectiveAt).getTime()
  const validEffectiveAt =
    Number.isFinite(effectiveTimestamp) &&
    effectiveTimestamp >= minimumTimestamp &&
    effectiveTimestamp <= Date.now() + 365 * 24 * 60 * 60_000
  const canIssue =
    policy !== undefined &&
    preview !== undefined &&
    validEffectiveAt &&
    reasonLength <= 1000

  React.useEffect(() => {
    if (!open) return
    setDraft(hnrPolicyDraftFromCurrent(current))
    setEffectiveAt(defaultEffectiveAt(minimumEffectiveFrom))
    setReason("")
    setPreview(undefined)
    previewMutation.reset()
    issueMutation.reset()
    // Mutation instances are stable; including them would make this reset on
    // every state transition instead of only when the dialog is reopened.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, current, minimumEffectiveFrom])

  function updateDraft<Key extends keyof HNRPolicyDraft>(
    key: Key,
    value: HNRPolicyDraft[Key]
  ) {
    setDraft((existing) => ({ ...existing, [key]: value }))
    setPreview(undefined)
    previewMutation.reset()
    issueMutation.reset()
  }

  async function handlePreview() {
    if (!policy) return
    try {
      setPreview(await previewMutation.mutateAsync(policy))
    } catch {
      setPreview(undefined)
    }
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!policy || !canIssue) return
    try {
      const revision = await issueMutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        policy,
        effectiveAt: new Date(effectiveAt).toISOString(),
        reason: reason.trim(),
      })
      onIssued(
        `H&R 第 ${revision.rule_version} 版规则已签发，将于 ${formatCompactDateTime(revision.effective_at)} 生效。`
      )
      onOpenChange(false)
    } catch {
      // Keep the complete draft visible so the operator can resolve a
      // timeline or idempotency conflict without reconstructing the policy.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[min(90vh,54rem)] overflow-y-auto sm:max-w-2xl">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>调整 H&R 规则</DialogTitle>
            <DialogDescription>
              新规则只影响生效后完成的下载。历史版本和已有 H&R 账目不会被回算。
            </DialogDescription>
          </DialogHeader>

          <FieldGroup className="py-5">
            <FieldSet>
              <FieldLegend>执行方式</FieldLegend>
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="hnr-mode">H&R 状态</FieldLabel>
                  <Select
                    items={modeItems}
                    value={draft.mode}
                    onValueChange={(value) =>
                      value &&
                      updateDraft("mode", value as HNRPolicyDraft["mode"])
                    }
                  >
                    <SelectTrigger id="hnr-mode" className="w-full">
                      <SelectValue>
                        {
                          modeItems.find((item) => item.value === draft.mode)
                            ?.label
                        }
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {modeItems.map((item) => (
                          <SelectItem key={item.value} value={item.value}>
                            {item.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    “全站豁免”会保留 H&R
                    记录但直接标记豁免；“停用”不再产生义务。
                  </FieldDescription>
                </Field>
              </FieldGroup>
            </FieldSet>

            {draft.mode === "enforced" ? (
              <FieldSet>
                <FieldLegend>达标条件</FieldLegend>
                <FieldDescription>
                  用户达到最低做种时间或单种分享率中的任意一项，即视为完成本次
                  H&R。
                </FieldDescription>
                <FieldGroup>
                  <div className="grid gap-4 sm:grid-cols-2">
                    <NumberField
                      id="hnr-seed-hours"
                      label="最低做种时间（小时）"
                      value={draft.seedHours}
                      min="0"
                      max="87600"
                      step="0.01"
                      onChange={(value) => updateDraft("seedHours", value)}
                    />
                    <NumberField
                      id="hnr-ratio"
                      label="达标分享率"
                      value={draft.ratio}
                      min="0"
                      max="100"
                      step="0.01"
                      onChange={(value) => updateDraft("ratio", value)}
                    />
                    <NumberField
                      id="hnr-assessment-days"
                      label="考察期（天）"
                      value={draft.assessmentDays}
                      min="0.01"
                      max="3650"
                      step="0.01"
                      onChange={(value) => updateDraft("assessmentDays", value)}
                    />
                    <NumberField
                      id="hnr-grace-hours"
                      label="逾期宽限（小时）"
                      value={draft.graceHours}
                      min="0"
                      max="8760"
                      step="0.01"
                      onChange={(value) => updateDraft("graceHours", value)}
                    />
                  </div>
                  <Field data-invalid={!policy || undefined}>
                    <FieldLabel htmlFor="hnr-interval-minutes">
                      单次心跳最多计时（分钟）
                    </FieldLabel>
                    <Input
                      id="hnr-interval-minutes"
                      type="number"
                      min="1"
                      max="1440"
                      step="1"
                      value={draft.intervalMinutes}
                      aria-invalid={!policy || undefined}
                      onChange={(event) =>
                        updateDraft("intervalMinutes", event.target.value)
                      }
                    />
                    <FieldDescription>
                      防止客户端长时间不汇报时，一次性虚增做种时长。通常保持 60
                      分钟。
                    </FieldDescription>
                    {!policy ? (
                      <FieldError>
                        请检查各项数值；考察期不能短于最低做种时间，做种时间和分享率不能同时为
                        0。
                      </FieldError>
                    ) : null}
                  </Field>
                </FieldGroup>
              </FieldSet>
            ) : (
              <Alert>
                <CircleAlertIcon />
                <AlertTitle>
                  {draft.mode === "exempt"
                    ? "全站下载均豁免"
                    : "不再建立 H&R 义务"}
                </AlertTitle>
                <AlertDescription>
                  生效前已经建立的义务仍按当时的不可变规则处理。
                </AlertDescription>
              </Alert>
            )}

            <FieldSet>
              <FieldLegend>生效与留痕</FieldLegend>
              <FieldGroup>
                <Field data-invalid={!validEffectiveAt || undefined}>
                  <FieldLabel htmlFor="hnr-effective-at">生效时间</FieldLabel>
                  <Input
                    id="hnr-effective-at"
                    type="datetime-local"
                    min={localDateTimeValue(new Date(minimumEffectiveFrom))}
                    value={effectiveAt}
                    aria-invalid={!validEffectiveAt || undefined}
                    onChange={(event) => setEffectiveAt(event.target.value)}
                  />
                  <FieldDescription>
                    最早可于 {formatCompactDateTime(minimumEffectiveFrom)}{" "}
                    生效。
                  </FieldDescription>
                  {!validEffectiveAt ? (
                    <FieldError>请选择允许范围内的未来时间。</FieldError>
                  ) : null}
                </Field>
                <Field
                  data-invalid={
                    reasonLength > 1000 || issueMutation.isError || undefined
                  }
                >
                  <FieldLabel htmlFor="hnr-reason">调整原因</FieldLabel>
                  <Textarea
                    id="hnr-reason"
                    value={reason}
                    rows={3}
                    maxLength={1000}
                    placeholder="可留空；系统会自动记录调整原因"
                    onChange={(event) => {
                      setReason(event.target.value)
                      issueMutation.reset()
                    }}
                  />
                  <FieldDescription>
                    已输入 {reasonLength}/1000 个字符；原因只保存在 Core
                    审计记录中。
                  </FieldDescription>
                  {reasonLength > 1000 ? (
                    <FieldError>调整原因不能超过 1000 个字符。</FieldError>
                  ) : null}
                  {issueMutation.isError ? (
                    <FieldError className="items-start">
                      <CircleAlertIcon className="mt-0.5" />
                      {requestErrorDescription(
                        issueMutation.error,
                        "签发失败，请刷新版本记录并检查生效时间。"
                      )}
                    </FieldError>
                  ) : null}
                </Field>
              </FieldGroup>
            </FieldSet>

            {previewMutation.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>规则参数无法预览</AlertTitle>
                <AlertDescription>
                  {requestErrorDescription(
                    previewMutation.error,
                    "请检查做种时间、分享率和考察期。"
                  )}
                </AlertDescription>
              </Alert>
            ) : null}

            {preview ? <PolicyPreview preview={preview} /> : null}
          </FieldGroup>

          <DialogFooter>
            <DialogClose
              render={<Button variant="outline" />}
              disabled={previewMutation.isPending || issueMutation.isPending}
            >
              取消
            </DialogClose>
            <Button
              type="button"
              variant="outline"
              disabled={
                !policy || previewMutation.isPending || issueMutation.isPending
              }
              onClick={() => void handlePreview()}
            >
              {previewMutation.isPending ? (
                <LoaderCircleIcon
                  data-icon="inline-start"
                  className="animate-spin"
                />
              ) : (
                <CalculatorIcon data-icon="inline-start" />
              )}
              {previewMutation.isPending ? "预览中…" : "预览规则"}
            </Button>
            <Button
              type="submit"
              disabled={!canIssue || issueMutation.isPending}
            >
              {issueMutation.isPending ? (
                <LoaderCircleIcon
                  data-icon="inline-start"
                  className="animate-spin"
                />
              ) : (
                <SaveIcon data-icon="inline-start" />
              )}
              {issueMutation.isPending ? "签发中…" : "确认签发"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function NumberField({
  id,
  label,
  value,
  min,
  max,
  step,
  onChange,
}: {
  id: string
  label: string
  value: string
  min: string
  max: string
  step: string
  onChange: (value: string) => void
}) {
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
    </Field>
  )
}

function PolicyPreview({ preview }: { preview: HNRPolicyPreview }) {
  if (preview.policy.mode !== "enforced") {
    return (
      <Alert>
        <CalculatorIcon />
        <AlertTitle>预览通过</AlertTitle>
        <AlertDescription>
          此模式没有做种时间、分享率或逾期截止时间。
        </AlertDescription>
      </Alert>
    )
  }
  return (
    <Alert>
      <Clock3Icon />
      <AlertTitle>以现在完成下载为例</AlertTitle>
      <AlertDescription>
        正常考察至 {formatCompactDateTime(preview.assessment_due_at)}，宽限至{" "}
        {formatCompactDateTime(preview.grace_ends_at)}；持续做种最快于{" "}
        {preview.continuous_seed_satisfied_at
          ? formatCompactDateTime(preview.continuous_seed_satisfied_at)
          : "未设置"}{" "}
        达标。
      </AlertDescription>
    </Alert>
  )
}

function defaultEffectiveAt(minimum: string) {
  const earliest = new Date(minimum).getTime()
  const preferred = Date.now() + 10 * 60_000
  const value = new Date(Math.max(earliest + 60_000, preferred))
  value.setSeconds(0, 0)
  return localDateTimeValue(value)
}

function localDateTimeValue(value: Date) {
  const local = new Date(value.getTime() - value.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}
