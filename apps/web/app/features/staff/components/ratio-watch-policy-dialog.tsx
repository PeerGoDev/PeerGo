import * as React from "react"
import {
  CalculatorIcon,
  CircleAlertIcon,
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
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import {
  type RatioWatchPolicyImpactPreview,
  type RatioWatchPolicyRevision,
  useIssueRatioWatchPolicy,
  usePreviewRatioWatchPolicy,
} from "~/features/staff/api/ratio-watch-administration.queries"
import {
  ratioWatchDraftFromCurrent,
  ratioWatchPolicyFromDraft,
  type RatioWatchPolicyDraft,
} from "~/features/staff/model/ratio-watch-policy-form"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function RatioWatchPolicyDialog({
  open,
  current,
  minimumEffectiveFrom,
  csrfToken,
  onOpenChange,
  onIssued,
}: {
  open: boolean
  current: RatioWatchPolicyRevision | null
  minimumEffectiveFrom: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onIssued: (message: string) => void
}) {
  const [draft, setDraft] = React.useState<RatioWatchPolicyDraft>(() =>
    ratioWatchDraftFromCurrent(current)
  )
  const [effectiveAt, setEffectiveAt] = React.useState(() =>
    defaultEffectiveAt(minimumEffectiveFrom)
  )
  const [reason, setReason] = React.useState("")
  const [preview, setPreview] = React.useState<RatioWatchPolicyImpactPreview>()
  const previewMutation = usePreviewRatioWatchPolicy()
  const issueMutation = useIssueRatioWatchPolicy()
  const ratioWatchPolicyRequestId = React.useRef<string | undefined>(undefined)
  const policy = ratioWatchPolicyFromDraft(draft)
  const reasonLength = Array.from(reason.trim()).length
  const effectiveTimestamp = new Date(effectiveAt).getTime()
  const validEffectiveAt =
    Number.isFinite(effectiveTimestamp) &&
    effectiveTimestamp >= new Date(minimumEffectiveFrom).getTime() &&
    effectiveTimestamp <= Date.now() + 365 * 24 * 60 * 60_000
  const canIssue =
    policy !== undefined &&
    preview !== undefined &&
    validEffectiveAt &&
    reasonLength >= 10 &&
    reasonLength <= 1000

  React.useEffect(() => {
    if (!open) return
    setDraft(ratioWatchDraftFromCurrent(current))
    setEffectiveAt(defaultEffectiveAt(minimumEffectiveFrom))
    setReason("")
    setPreview(undefined)
    previewMutation.reset()
    issueMutation.reset()
    ratioWatchPolicyRequestId.current = undefined
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, current, minimumEffectiveFrom])

  function updateDraft<Key extends keyof RatioWatchPolicyDraft>(
    key: Key,
    value: RatioWatchPolicyDraft[Key]
  ) {
    setDraft((existing) => ({ ...existing, [key]: value }))
    setPreview(undefined)
    previewMutation.reset()
    issueMutation.reset()
    ratioWatchPolicyRequestId.current = undefined
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
    const idempotencyKey =
      ratioWatchPolicyRequestId.current ?? crypto.randomUUID()
    ratioWatchPolicyRequestId.current = idempotencyKey
    try {
      const revision = await issueMutation.mutateAsync({
        csrfToken,
        idempotencyKey,
        policy,
        effectiveAt: new Date(effectiveAt).toISOString(),
        reason: reason.trim(),
      })
      ratioWatchPolicyRequestId.current = undefined
      onIssued(
        `长期分享率第 ${revision.rule_version} 版规则已签发，将于 ${formatCompactDateTime(revision.effective_at)} 生效。`
      )
      onOpenChange(false)
    } catch {
      // Preserve the draft and preview so the operator can resolve conflicts.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[min(90vh,54rem)] overflow-y-auto sm:max-w-2xl">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>调整长期分享率考核</DialogTitle>
            <DialogDescription>
              采用 PtYes
              熟悉的下载量、最低分享率、观察期和限制线；新版本不会回算旧账。
            </DialogDescription>
          </DialogHeader>

          <FieldGroup className="py-5">
            <Field
              orientation="horizontal"
              className="rounded-md border px-3 py-3"
            >
              <FieldContent>
                <FieldTitle>启用分享率考核</FieldTitle>
                <FieldDescription>
                  VIP
                  自动豁免。停用只阻止新考核，已有考核仍可达标或由管理员解除。
                </FieldDescription>
              </FieldContent>
              <Switch
                checked={draft.enabled}
                onCheckedChange={(value) => updateDraft("enabled", value)}
                aria-label="启用分享率考核"
              />
            </Field>

            {draft.enabled ? (
              <FieldSet>
                <FieldLegend>考核条件</FieldLegend>
                <FieldDescription>
                  达到下载量且总分享率低于最低线时开始观察；到期低于限制线才限制下载。
                </FieldDescription>
                <FieldGroup>
                  <div className="grid gap-4 sm:grid-cols-2">
                    <NumberField
                      id="ratio-threshold"
                      label="下载量阈值（GiB）"
                      description="不足此下载量不进入考核。"
                      value={draft.thresholdGiB}
                      min="1"
                      max="8388608000"
                      step="1"
                      onChange={(value) => updateDraft("thresholdGiB", value)}
                    />
                    <NumberField
                      id="ratio-minimum"
                      label="最低分享率"
                      description="达到此值即结束考核或解除自动限制。"
                      value={draft.minimumRatio}
                      min="0.0001"
                      max="100"
                      step="0.01"
                      onChange={(value) => updateDraft("minimumRatio", value)}
                    />
                    <NumberField
                      id="ratio-watch-days"
                      label="观察天数"
                      description="从首次不达标开始计算。"
                      value={draft.watchDays}
                      min="1"
                      max="365"
                      step="1"
                      onChange={(value) => updateDraft("watchDays", value)}
                    />
                    <NumberField
                      id="ratio-restriction"
                      label="到期限制线"
                      description="不得高于最低分享率。"
                      value={draft.restrictionRatio}
                      min="0.0001"
                      max="100"
                      step="0.01"
                      onChange={(value) =>
                        updateDraft("restrictionRatio", value)
                      }
                    />
                  </div>
                  {!policy ? (
                    <FieldError>
                      请检查数值；限制线不能高于最低分享率，观察期为 1–365 天。
                    </FieldError>
                  ) : null}
                </FieldGroup>
              </FieldSet>
            ) : (
              <Alert>
                <CircleAlertIcon />
                <AlertTitle>停止建立新考核</AlertTitle>
                <AlertDescription>
                  旧站迁移或管理员手工设置的下载限制不会被这一开关清除。
                </AlertDescription>
              </Alert>
            )}

            <FieldSet>
              <FieldLegend>生效与留痕</FieldLegend>
              <FieldGroup>
                <Field data-invalid={!validEffectiveAt || undefined}>
                  <FieldLabel htmlFor="ratio-effective-at">生效时间</FieldLabel>
                  <Input
                    id="ratio-effective-at"
                    type="datetime-local"
                    min={localDateTimeValue(new Date(minimumEffectiveFrom))}
                    value={effectiveAt}
                    aria-invalid={!validEffectiveAt || undefined}
                    onChange={(event) => {
                      setEffectiveAt(event.target.value)
                      issueMutation.reset()
                      ratioWatchPolicyRequestId.current = undefined
                    }}
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
                    (reasonLength > 0 && reasonLength < 10) ||
                    issueMutation.isError ||
                    undefined
                  }
                >
                  <FieldLabel htmlFor="ratio-reason">调整原因</FieldLabel>
                  <Textarea
                    id="ratio-reason"
                    value={reason}
                    maxLength={1000}
                    placeholder="说明调整依据、影响范围和回退条件（至少 10 个字符）"
                    onChange={(event) => {
                      setReason(event.target.value)
                      issueMutation.reset()
                      ratioWatchPolicyRequestId.current = undefined
                    }}
                  />
                  <FieldDescription>
                    {reasonLength}/1000 个字符
                  </FieldDescription>
                  {issueMutation.isError ? (
                    <FieldError>
                      {requestErrorDescription(
                        issueMutation.error,
                        "规则未签发，请刷新后核对生效时间与当前版本。"
                      )}
                    </FieldError>
                  ) : null}
                </Field>
              </FieldGroup>
            </FieldSet>

            {previewMutation.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>影响预览失败</AlertTitle>
                <AlertDescription>
                  {requestErrorDescription(
                    previewMutation.error,
                    "请检查规则参数后重试。"
                  )}
                </AlertDescription>
              </Alert>
            ) : null}
            {preview ? <ImpactPreview preview={preview} /> : null}
          </FieldGroup>

          <DialogFooter>
            <DialogClose render={<Button variant="outline" />}>
              取消
            </DialogClose>
            <Button
              type="button"
              variant="outline"
              disabled={!policy || previewMutation.isPending}
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
              预览影响
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
              签发规则
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
  description,
  value,
  min,
  max,
  step,
  onChange,
}: {
  id: string
  label: string
  description: string
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
        inputMode="decimal"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
      <FieldDescription>{description}</FieldDescription>
    </Field>
  )
}

function ImpactPreview({
  preview,
}: {
  preview: RatioWatchPolicyImpactPreview
}) {
  return (
    <Alert>
      <CalculatorIcon />
      <AlertTitle>当前数据影响预览</AlertTitle>
      <AlertDescription>
        符合基础条件 {formatInteger(preview.eligible_users)} 人，其中约有{" "}
        {formatInteger(preview.would_enter_watch)} 人会进入观察；按当前总量已有{" "}
        {formatInteger(preview.would_restrict_at_deadline)} 人低于限制线。VIP
        豁免 {formatInteger(preview.vip_exempt_users)} 人，旧限制{" "}
        {formatInteger(preview.legacy_restricted_users)} 人不会被覆盖。
      </AlertDescription>
    </Alert>
  )
}

function defaultEffectiveAt(minimum: string) {
  const earliest = new Date(minimum)
  const rounded = new Date(Math.ceil(earliest.getTime() / 60_000) * 60_000)
  return localDateTimeValue(rounded)
}

function localDateTimeValue(value: Date) {
  const local = new Date(value.getTime() - value.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}
