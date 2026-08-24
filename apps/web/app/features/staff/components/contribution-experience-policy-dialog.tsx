import * as React from "react"
import { CircleAlertIcon, Settings2Icon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Dialog,
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
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import {
  type ContributionExperiencePolicyPage,
  useIssueContributionExperiencePolicy,
} from "~/features/staff/api/seeding-reward-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"

type Draft = {
  uploadGiB: string
  torrent: string
  accountDay: string
}

export function ContributionExperiencePolicyDialog({
  policies,
  csrfToken,
  disabled,
}: {
  policies: ContributionExperiencePolicyPage
  csrfToken: string
  disabled: boolean
}) {
  const latest = policies.items[0]
  const [open, setOpen] = React.useState(false)
  const [draft, setDraft] = React.useState<Draft>(() => ({
    uploadGiB: formatMilli(latest?.experience_per_upload_gib_milli ?? 100),
    torrent: formatMilli(latest?.experience_per_torrent_milli ?? 2_000),
    accountDay: formatMilli(latest?.experience_per_account_day_milli ?? 1_000),
  }))
  const [effectiveFrom, setEffectiveFrom] = React.useState(() =>
    toDateTimeLocal(policies.minimum_effective_from)
  )
  const [reason, setReason] = React.useState("")
  const mutation = useIssueContributionExperiencePolicy()
  const validation = validateDraft(
    draft,
    effectiveFrom,
    reason,
    policies.minimum_effective_from,
    latest?.effective_from
  )

  function updateDraft(key: keyof Draft, value: string) {
    setDraft((current) => ({ ...current, [key]: value }))
    mutation.reset()
  }

  async function submit() {
    if (!validation.valid || !validation.values) return
    const effective = new Date(effectiveFrom)
    await mutation.mutateAsync({
      csrfToken,
      body: {
        policy: {
          revision: contributionRevision(effective),
          effective_from: effective.toISOString(),
          experience_per_upload_gib_milli: validation.values.uploadGiB,
          experience_per_torrent_milli: validation.values.torrent,
          experience_per_account_day_milli: validation.values.accountDay,
        },
        reason: reason.trim(),
      },
    })
    setOpen(false)
    setReason("")
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !mutation.isPending && setOpen(next)}
    >
      <DialogTrigger
        render={<Button type="button" size="sm" disabled={disabled} />}
      >
        <Settings2Icon data-icon="inline-start" />
        调整经验来源
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>签发经验获取规则</DialogTitle>
          <DialogDescription>
            调整上传、发布种子与账号时长经验。做种经验和签到经验分别跟随各自的结算政策。
          </DialogDescription>
        </DialogHeader>

        {mutation.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>经验获取规则未签发</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(
                mutation.error,
                "请重新加载页面并核对生效时间和经验数值。"
              )}
            </AlertDescription>
          </Alert>
        ) : null}

        <FieldGroup>
          <div className="grid gap-4 sm:grid-cols-3">
            <ExperienceField
              id="contribution-experience-upload"
              label="每实际上传 1 GiB"
              value={draft.uploadGiB}
              error={validation.fieldErrors.uploadGiB}
              onChange={(value) => updateDraft("uploadGiB", value)}
            />
            <ExperienceField
              id="contribution-experience-torrent"
              label="每发布 1 个种子"
              value={draft.torrent}
              error={validation.fieldErrors.torrent}
              onChange={(value) => updateDraft("torrent", value)}
            />
            <ExperienceField
              id="contribution-experience-account-day"
              label="账号每存续 1 天"
              value={draft.accountDay}
              error={validation.fieldErrors.accountDay}
              onChange={(value) => updateDraft("accountDay", value)}
            />
          </div>

          <Field data-invalid={validation.timeError !== null}>
            <FieldLabel htmlFor="contribution-experience-effective">
              生效时间
            </FieldLabel>
            <Input
              id="contribution-experience-effective"
              type="datetime-local"
              min={toDateTimeLocal(policies.minimum_effective_from)}
              value={effectiveFrom}
              aria-invalid={validation.timeError !== null}
              onChange={(event) => {
                setEffectiveFrom(event.target.value)
                mutation.reset()
              }}
            />
            <FieldDescription>
              必须选择未来整点；历史经验账本不会按新参数回算。
            </FieldDescription>
            {validation.timeError ? (
              <FieldError>{validation.timeError}</FieldError>
            ) : null}
          </Field>

          <Field data-invalid={validation.reasonError !== null}>
            <FieldLabel htmlFor="contribution-experience-reason">
              调整说明
            </FieldLabel>
            <Textarea
              id="contribution-experience-reason"
              rows={3}
              maxLength={1000}
              value={reason}
              aria-invalid={validation.reasonError !== null}
              onChange={(event) => {
                setReason(event.target.value)
                mutation.reset()
              }}
            />
            <FieldDescription>
              {Array.from(reason.trim()).length} / 1000；留空时由系统自动记录。
            </FieldDescription>
            {validation.reasonError ? (
              <FieldError>{validation.reasonError}</FieldError>
            ) : null}
          </Field>
        </FieldGroup>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={mutation.isPending}
            onClick={() => setOpen(false)}
          >
            取消
          </Button>
          <Button
            type="button"
            disabled={!validation.valid || mutation.isPending}
            onClick={() => void submit()}
          >
            {mutation.isPending ? <Spinner data-icon="inline-start" /> : null}
            签发定时版本
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ExperienceField({
  id,
  label,
  value,
  error,
  onChange,
}: {
  id: string
  label: string
  value: string
  error: string | null
  onChange: (value: string) => void
}) {
  return (
    <Field data-invalid={error !== null}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        inputMode="decimal"
        value={value}
        aria-invalid={error !== null}
        onChange={(event) => onChange(event.target.value)}
      />
      <FieldDescription>经验，最多 3 位小数。</FieldDescription>
      {error ? <FieldError>{error}</FieldError> : null}
    </Field>
  )
}

function validateDraft(
  draft: Draft,
  effectiveFrom: string,
  reason: string,
  minimumEffectiveFrom: string,
  latestEffectiveFrom?: string
) {
  const values = {
    uploadGiB: parseMilli(draft.uploadGiB),
    torrent: parseMilli(draft.torrent),
    accountDay: parseMilli(draft.accountDay),
  }
  const fieldErrors = {
    uploadGiB: values.uploadGiB === null ? "请输入有效的非负经验值。" : null,
    torrent: values.torrent === null ? "请输入有效的非负经验值。" : null,
    accountDay: values.accountDay === null ? "请输入有效的非负经验值。" : null,
  }
  const effective = new Date(effectiveFrom)
  const minimum = new Date(minimumEffectiveFrom)
  const latest = latestEffectiveFrom ? new Date(latestEffectiveFrom) : null
  let timeError: string | null = null
  if (
    Number.isNaN(effective.valueOf()) ||
    effective.getUTCMinutes() !== 0 ||
    effective.getUTCSeconds() !== 0
  ) {
    timeError = "生效时间必须选择整点。"
  } else if (effective < minimum || (latest && effective <= latest)) {
    timeError = "生效时间过早，或没有排在现有版本之后。"
  }
  const reasonLength = Array.from(reason.trim()).length
  const reasonError =
    reasonLength > 1000 ? "调整说明不能超过 1000 个字符。" : null
  const completeValues =
    values.uploadGiB !== null &&
    values.torrent !== null &&
    values.accountDay !== null
      ? {
          uploadGiB: values.uploadGiB,
          torrent: values.torrent,
          accountDay: values.accountDay,
        }
      : null
  return {
    values: completeValues,
    fieldErrors,
    timeError,
    reasonError,
    valid:
      completeValues !== null && timeError === null && reasonError === null,
  }
}

function parseMilli(value: string) {
  const match = /^(\d{1,9})(?:\.(\d{1,3}))?$/.exec(value.trim())
  if (!match) return null
  const milli =
    Number(match[1]) * 1_000 + Number((match[2] ?? "").padEnd(3, "0"))
  return Number.isSafeInteger(milli) && milli <= 1_000_000_000 ? milli : null
}

function formatMilli(value: number) {
  return new Intl.NumberFormat("zh-CN", {
    useGrouping: false,
    maximumFractionDigits: 3,
  }).format(value / 1_000)
}

function toDateTimeLocal(value: string) {
  const date = new Date(value)
  const local = new Date(date.valueOf() - date.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

function contributionRevision(effective: Date) {
  const timestamp = effective.toISOString().slice(0, 13).replaceAll(/[-T]/g, "")
  return `peergo-contribution-${timestamp}`
}
