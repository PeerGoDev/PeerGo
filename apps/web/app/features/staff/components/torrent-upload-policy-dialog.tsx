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
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type TorrentSettingsOverview,
  useIssueTorrentUploadPolicyRevision,
} from "~/features/staff/api/operations.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatBytes } from "~/shared/formatters/bytes"

const MiB = 1024 * 1024

export function TorrentUploadPolicyDialog({
  settings,
  csrfToken,
  disabled,
}: {
  settings: TorrentSettingsOverview
  csrfToken: string
  disabled: boolean
}) {
  const [open, setOpen] = React.useState(false)
  const active = settings.active_upload_policy
  const [metainfoMiB, setMetainfoMiB] = React.useState(
    active.settings.metainfo_max_bytes / MiB
  )
  const [maxFiles, setMaxFiles] = React.useState(active.settings.max_files)
  const [maxCount, setMaxCount] = React.useState(
    active.settings.screenshot_max_count
  )
  const [screenshotMiB, setScreenshotMiB] = React.useState(
    active.settings.screenshot_max_bytes / MiB
  )
  const [maxPixels, setMaxPixels] = React.useState(
    active.settings.screenshot_max_pixels
  )
  const [formats, setFormats] = React.useState<string[]>(
    active.settings.screenshot_formats
  )
  const [effectiveAt, setEffectiveAt] = React.useState(defaultEffectiveAt)
  const [reason, setReason] = React.useState("")
  const requestId = React.useRef<string | undefined>(undefined)
  const mutation = useIssueTorrentUploadPolicyRevision()
  const reasonLength = Array.from(reason.trim()).length
  const valid =
    Number.isInteger(metainfoMiB) &&
    metainfoMiB >= 1 &&
    metainfoMiB <= 4 &&
    Number.isInteger(maxFiles) &&
    maxFiles >= 1 &&
    maxFiles <= 100_000 &&
    Number.isInteger(maxCount) &&
    maxCount >= 0 &&
    maxCount <= 6 &&
    Number.isInteger(screenshotMiB) &&
    screenshotMiB >= 1 &&
    screenshotMiB <= 2 &&
    Number.isInteger(maxPixels) &&
    maxPixels >= 65_536 &&
    maxPixels <= 25_000_000 &&
    formats.length > 0 &&
    reasonLength <= 1000 &&
    new Date(effectiveAt).getTime() >= Date.now() + 60_000

  async function submit() {
    if (!valid) return
    requestId.current ??= globalThis.crypto.randomUUID()
    await mutation.mutateAsync({
      csrfToken,
      idempotencyKey: requestId.current,
      body: {
        expected_sequence: Math.max(
          active.sequence,
          ...settings.scheduled_upload_policies.map((item) => item.sequence)
        ),
        effective_at: new Date(effectiveAt).toISOString(),
        reason: reason.trim(),
        settings: {
          metainfo_max_bytes: metainfoMiB * MiB,
          max_files: maxFiles,
          screenshot_max_count: maxCount,
          screenshot_max_bytes: screenshotMiB * MiB,
          screenshot_max_pixels: maxPixels,
          screenshot_formats: formats as Array<"jpeg" | "png" | "webp">,
        },
      },
    })
    setOpen(false)
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
        调整上传规则
      </DialogTrigger>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>签发上传规则版本</DialogTitle>
          <DialogDescription>
            规则不会覆盖历史记录；到达生效时间后，新种子上传和已发布截图修改共同绑定该版本。
          </DialogDescription>
        </DialogHeader>
        {mutation.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>规则未签发</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(
                mutation.error,
                "请核对版本和限制后重试。"
              )}
            </AlertDescription>
          </Alert>
        ) : null}
        <Alert>
          <Settings2Icon />
          <AlertTitle>安全上限不可突破</AlertTitle>
          <AlertDescription>
            本部署最多接收 4 MiB .torrent、100,000 个文件、6 张截图，每张最多 2
            MiB / 25MP。规则只能收紧这些边界。
          </AlertDescription>
        </Alert>
        <FieldGroup>
          <div className="grid gap-4 sm:grid-cols-2">
            <NumberField
              label=".torrent 上限（MiB）"
              value={metainfoMiB}
              min={1}
              max={4}
              onChange={setMetainfoMiB}
            />
            <NumberField
              label="文件树条目上限"
              value={maxFiles}
              min={1}
              max={100000}
              onChange={setMaxFiles}
            />
            <NumberField
              label="截图数量上限"
              value={maxCount}
              min={0}
              max={6}
              onChange={setMaxCount}
            />
            <NumberField
              label="单张截图上限（MiB）"
              value={screenshotMiB}
              min={1}
              max={2}
              onChange={setScreenshotMiB}
            />
            <NumberField
              label="单张像素上限"
              value={maxPixels}
              min={65536}
              max={25000000}
              onChange={setMaxPixels}
            />
            <Field>
              <FieldLabel htmlFor="upload-policy-effective-at">
                生效时间
              </FieldLabel>
              <Input
                id="upload-policy-effective-at"
                type="datetime-local"
                value={effectiveAt}
                onChange={(event) => {
                  setEffectiveAt(event.target.value)
                  requestId.current = undefined
                }}
              />
              <FieldDescription>至少晚于当前时间 1 分钟</FieldDescription>
            </Field>
          </div>
          <Field data-invalid={formats.length === 0}>
            <FieldLabel>允许的截图格式</FieldLabel>
            <ToggleGroup
              value={formats}
              onValueChange={(values) => {
                if (values.length) {
                  setFormats(values)
                  requestId.current = undefined
                }
              }}
              variant="outline"
            >
              <ToggleGroupItem value="jpeg">JPEG</ToggleGroupItem>
              <ToggleGroupItem value="png">PNG</ToggleGroupItem>
              <ToggleGroupItem value="webp">WebP</ToggleGroupItem>
            </ToggleGroup>
            {formats.length === 0 ? (
              <FieldError>至少保留一种图片格式。</FieldError>
            ) : null}
          </Field>
          <Field data-invalid={reasonLength > 1000}>
            <FieldLabel htmlFor="upload-policy-reason">修改说明</FieldLabel>
            <Textarea
              id="upload-policy-reason"
              value={reason}
              maxLength={1000}
              rows={3}
              onChange={(event) => {
                setReason(event.target.value)
                requestId.current = undefined
                mutation.reset()
              }}
            />
            <FieldDescription>
              {reasonLength} / 1000；留空时由系统自动记录。当前 .torrent 上限为{" "}
              {formatBytes(active.settings.metainfo_max_bytes)}。
            </FieldDescription>
            {reasonLength > 1000 ? (
              <FieldError>修改说明不能超过 1000 个字符。</FieldError>
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
            disabled={!valid || mutation.isPending}
            onClick={() => void submit()}
          >
            {mutation.isPending ? <Spinner /> : null}签发定时版本
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function NumberField({
  label,
  value,
  min,
  max,
  onChange,
}: {
  label: string
  value: number
  min: number
  max: number
  onChange: (value: number) => void
}) {
  const id = React.useId()
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        value={value}
        min={min}
        max={max}
        step={1}
        onChange={(event) => onChange(event.currentTarget.valueAsNumber)}
      />
      <FieldDescription>
        {min.toLocaleString("zh-CN")}–{max.toLocaleString("zh-CN")}
      </FieldDescription>
    </Field>
  )
}

function defaultEffectiveAt() {
  const value = new Date(Date.now() + 5 * 60_000)
  value.setSeconds(0, 0)
  return `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, "0")}-${String(value.getDate()).padStart(2, "0")}T${String(value.getHours()).padStart(2, "0")}:${String(value.getMinutes()).padStart(2, "0")}`
}
