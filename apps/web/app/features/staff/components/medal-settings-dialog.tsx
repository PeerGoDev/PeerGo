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
  type MedalSettings,
  type MedalSettingsWriteRequest,
  useUpdateMedalSettings,
} from "~/features/staff/api/medal-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"

type Draft = {
  enabled: boolean
  maximumWearCount: string
  maximumUploadBonusPercent: string
  maximumDownloadDiscountPercent: string
  maximumMagicBonusPercent: string
  maximumInviteBonus: string
  reason: string
}

export function MedalSettingsDialog({
  open,
  onOpenChange,
  settings,
  csrfToken,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  settings: MedalSettings
  csrfToken: string
}) {
  const mutation = useUpdateMedalSettings()
  const [draft, setDraft] = React.useState(() => toDraft(settings))
  const validation = validateDraft(draft, settings.version)

  React.useEffect(() => {
    if (!open) return
    setDraft(toDraft(settings))
    mutation.reset()
  }, [open, settings])

  function update<Key extends keyof Draft>(key: Key, value: Draft[Key]) {
    setDraft((current) => ({ ...current, [key]: value }))
    mutation.reset()
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!validation.body) return
    try {
      await mutation.mutateAsync({ csrfToken, body: validation.body })
      onOpenChange(false)
    } catch {
      // Keep the complete draft visible after a version conflict or API error.
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !mutation.isPending && onOpenChange(next)}
    >
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-3xl">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>编辑全站勋章规则</DialogTitle>
            <DialogDescription>
              控制勋章入口、佩戴数量与多枚勋章叠加后的权益上限。保存后立即用于新请求，历史账目不会回算。
            </DialogDescription>
          </DialogHeader>

          <FieldGroup className="py-5">
            {mutation.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>全站勋章规则未保存</AlertTitle>
                <AlertDescription>
                  {requestErrorDescription(
                    mutation.error,
                    "请重新载入页面，并检查权益范围、当前版本和变更理由。"
                  )}
                </AlertDescription>
              </Alert>
            ) : null}

            <Field orientation="horizontal">
              <div className="min-w-0 flex-1">
                <FieldTitle>启用勋章系统</FieldTitle>
                <FieldDescription>
                  关闭后停止购买和佩戴入口，不会删除用户持有记录。
                </FieldDescription>
              </div>
              <Switch
                checked={draft.enabled}
                onCheckedChange={(value) => update("enabled", value)}
              />
            </Field>

            <div className="grid gap-4 sm:grid-cols-2">
              <IntegerField
                id="medal-settings-wear-count"
                label="最多佩戴（枚）"
                value={draft.maximumWearCount}
                error={validation.maximumWearCountError}
                min={0}
                max={100}
                onChange={(value) => update("maximumWearCount", value)}
              />
              <IntegerField
                id="medal-settings-invite-bonus"
                label="邀请奖励叠加上限"
                value={draft.maximumInviteBonus}
                error={validation.maximumInviteBonusError}
                min={0}
                max={1_000_000}
                onChange={(value) => update("maximumInviteBonus", value)}
              />
              <PercentField
                id="medal-settings-upload-bonus"
                label="上传加成上限（%）"
                value={draft.maximumUploadBonusPercent}
                error={validation.maximumUploadBonusError}
                onChange={(value) => update("maximumUploadBonusPercent", value)}
              />
              <PercentField
                id="medal-settings-download-discount"
                label="下载减免上限（%）"
                value={draft.maximumDownloadDiscountPercent}
                error={validation.maximumDownloadDiscountError}
                onChange={(value) =>
                  update("maximumDownloadDiscountPercent", value)
                }
              />
              <PercentField
                id="medal-settings-magic-bonus"
                label="做种魔力加成上限（%）"
                value={draft.maximumMagicBonusPercent}
                error={validation.maximumMagicBonusError}
                onChange={(value) => update("maximumMagicBonusPercent", value)}
              />
            </div>

            <Field data-invalid={Boolean(validation.reasonError)}>
              <FieldLabel htmlFor="medal-settings-reason">变更理由</FieldLabel>
              <Textarea
                id="medal-settings-reason"
                value={draft.reason}
                rows={3}
                maxLength={500}
                aria-invalid={Boolean(validation.reasonError)}
                placeholder="可留空；系统会自动记录全站勋章规则的变更理由"
                onChange={(event) => update("reason", event.target.value)}
              />
              <FieldDescription>
                理由与第 {settings.version + 1} 版规则一同进入不可变修订记录。
              </FieldDescription>
              {validation.reasonError ? (
                <FieldError>{validation.reasonError}</FieldError>
              ) : null}
            </Field>

            <Alert>
              <Settings2Icon />
              <AlertTitle>魔力加成上限使用历史时间线</AlertTitle>
              <AlertDescription>
                修改魔力加成上限时，Core
                会为受影响用户追加新的未来权益输入；既有做种奖励结算保持不变。
              </AlertDescription>
            </Alert>
          </FieldGroup>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={mutation.isPending}
              onClick={() => onOpenChange(false)}
            >
              取消
            </Button>
            <Button
              type="submit"
              disabled={!validation.body || mutation.isPending}
            >
              {mutation.isPending ? <Spinner data-icon="inline-start" /> : null}
              {mutation.isPending ? "保存中…" : "保存并立即生效"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function IntegerField({
  id,
  label,
  value,
  error,
  min,
  max,
  onChange,
}: {
  id: string
  label: string
  value: string
  error: string | null
  min: number
  max: number
  onChange: (value: string) => void
}) {
  return (
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        inputMode="numeric"
        min={min}
        max={max}
        step={1}
        value={value}
        aria-invalid={Boolean(error)}
        onChange={(event) => onChange(event.target.value)}
      />
      {error ? <FieldError>{error}</FieldError> : null}
    </Field>
  )
}

function PercentField({
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
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        inputMode="decimal"
        min={0}
        max={1000}
        step="0.01"
        value={value}
        aria-invalid={Boolean(error)}
        onChange={(event) => onChange(event.target.value)}
      />
      {error ? <FieldError>{error}</FieldError> : null}
    </Field>
  )
}

function toDraft(settings: MedalSettings): Draft {
  return {
    enabled: settings.enabled,
    maximumWearCount: String(settings.maximum_wear_count),
    maximumUploadBonusPercent: bpsToPercent(settings.maximum_upload_bonus_bps),
    maximumDownloadDiscountPercent: bpsToPercent(
      settings.maximum_download_discount_bps
    ),
    maximumMagicBonusPercent: bpsToPercent(settings.maximum_magic_bonus_bps),
    maximumInviteBonus: settings.maximum_invite_bonus,
    reason: "",
  }
}

function validateDraft(draft: Draft, expectedVersion: number) {
  const maximumWearCount = parseInteger(draft.maximumWearCount, 0, 100)
  const maximumInviteBonus = parseInteger(
    draft.maximumInviteBonus,
    0,
    1_000_000
  )
  const maximumUploadBonusBps = percentToBps(draft.maximumUploadBonusPercent)
  const maximumDownloadDiscountBps = percentToBps(
    draft.maximumDownloadDiscountPercent
  )
  const maximumMagicBonusBps = percentToBps(draft.maximumMagicBonusPercent)
  const reason = draft.reason.trim()
  const reasonLength = Array.from(reason).length

  const result = {
    maximumWearCountError:
      maximumWearCount === null ? "请输入 0–100 的整数。" : null,
    maximumInviteBonusError:
      maximumInviteBonus === null ? "请输入 0–1,000,000 的整数。" : null,
    maximumUploadBonusError:
      maximumUploadBonusBps === null ? "请输入 0–1000%，最多两位小数。" : null,
    maximumDownloadDiscountError:
      maximumDownloadDiscountBps === null
        ? "请输入 0–1000%，最多两位小数。"
        : null,
    maximumMagicBonusError:
      maximumMagicBonusBps === null ? "请输入 0–1000%，最多两位小数。" : null,
    reasonError: reasonLength > 500 ? "变更理由不能超过 500 个字符。" : null,
    body: undefined as MedalSettingsWriteRequest | undefined,
  }
  if (
    Object.entries(result).some(
      ([key, value]) => key !== "body" && value !== null
    ) ||
    maximumWearCount === null ||
    maximumInviteBonus === null ||
    maximumUploadBonusBps === null ||
    maximumDownloadDiscountBps === null ||
    maximumMagicBonusBps === null
  ) {
    return result
  }
  result.body = {
    enabled: draft.enabled,
    maximum_wear_count: maximumWearCount,
    maximum_upload_bonus_bps: maximumUploadBonusBps,
    maximum_download_discount_bps: maximumDownloadDiscountBps,
    maximum_magic_bonus_bps: maximumMagicBonusBps,
    maximum_invite_bonus: String(maximumInviteBonus),
    expected_version: expectedVersion,
    reason,
  }
  return result
}

function parseInteger(value: string, min: number, max: number) {
  if (!/^(0|[1-9][0-9]*)$/.test(value)) return null
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed >= min && parsed <= max
    ? parsed
    : null
}

function percentToBps(value: string) {
  if (!/^(?:0|[1-9][0-9]{0,3})(?:\.[0-9]{1,2})?$/.test(value)) return null
  const parsed = Number(value)
  if (!Number.isFinite(parsed) || parsed < 0 || parsed > 1000) return null
  return Math.round(parsed * 100)
}

function bpsToPercent(value: number) {
  return (value / 100).toLocaleString("en-US", {
    useGrouping: false,
    maximumFractionDigits: 2,
  })
}
