import * as React from "react"
import { CircleAlertIcon, MedalIcon, ShieldCheckIcon } from "lucide-react"

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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import {
  type MedalDefinition,
  type MedalDefinitionWriteRequest,
  useCreateMedalDefinition,
  useUpdateMedalDefinition,
} from "~/features/staff/api/medal-administration.queries"
import type { components } from "~/generated/api"
import { requestErrorDescription } from "~/shared/api/problem"

type AcquisitionMethod = components["schemas"]["MedalAcquisitionMethod"]
type RewardCycle = NonNullable<MedalDefinitionWriteRequest["reward_cycle"]>

const acquisitionItems: Array<{
  value: AcquisitionMethod
  label: string
  description: string
}> = [
  { value: "purchase", label: "魔力值购买", description: "用户按后台价格购买" },
  { value: "grant", label: "后台颁发", description: "由管理员或规则主动颁发" },
  { value: "sponsor", label: "站点贡献", description: "用于旧站赞助类勋章" },
  { value: "workgroup", label: "工作组", description: "随工作组资格生效" },
  { value: "developer", label: "开发维护", description: "用于开发与维护贡献" },
]

const rewardCycleItems: Array<{ value: "none" | RewardCycle; label: string }> =
  [
    { value: "none", label: "不发放" },
    { value: "daily", label: "每天" },
    { value: "weekly", label: "每周" },
    { value: "monthly", label: "每月" },
  ]

type MedalDraft = {
  name: string
  description: string
  imageLargePath: string
  imageSmallPath: string
  acquisitionMethod: AcquisitionMethod
  price: string
  durationDays: string
  displayOnPage: boolean
  priority: string
  uploadBonusPercent: string
  downloadDiscountPercent: string
  magicBonusPercent: string
  inviteBonus: string
  poolEligible: boolean
  periodicRewardMagic: string
  rewardCycle: "none" | RewardCycle
  inventory: string
  saleBeginAt: string | null
  saleEndAt: string | null
  reason: string
}

export function MedalDefinitionDialog({
  open,
  onOpenChange,
  medal,
  csrfToken,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  medal?: MedalDefinition
  csrfToken: string
}) {
  const [draft, setDraft] = React.useState<MedalDraft>(() => toDraft(medal))
  const createMutation = useCreateMedalDefinition()
  const updateMutation = useUpdateMedalDefinition()
  const mutation = medal ? updateMutation : createMutation
  const validation = validateDraft(draft)

  React.useEffect(() => {
    if (!open) return
    setDraft(toDraft(medal))
    createMutation.reset()
    updateMutation.reset()
  }, [open, medal])

  function update<Key extends keyof MedalDraft>(
    key: Key,
    value: MedalDraft[Key]
  ) {
    setDraft((current) => ({ ...current, [key]: value }))
    createMutation.reset()
    updateMutation.reset()
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!validation.body) return
    try {
      if (medal) {
        await updateMutation.mutateAsync({
          csrfToken,
          medalId: Number(medal.id),
          body: { ...validation.body, expected_version: medal.version },
        })
      } else {
        await createMutation.mutateAsync({
          csrfToken,
          body: validation.body,
        })
      }
      onOpenChange(false)
    } catch {
      // Keep the draft visible so the operator can resolve the problem.
    }
  }

  const selectedMethod = acquisitionItems.find(
    (item) => item.value === draft.acquisitionMethod
  )
  const previewImage = draft.imageSmallPath || draft.imageLargePath

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !mutation.isPending && onOpenChange(next)}
    >
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-4xl">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>
              {medal ? "编辑「" + medal.name + "」" : "新增勋章"}
            </DialogTitle>
            <DialogDescription>
              设置勋章图片、获取方式和实际权益。保存会新增修订记录，不改写用户历史账目。
            </DialogDescription>
          </DialogHeader>

          <FieldGroup className="py-5">
            {mutation.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>勋章设置未保存</AlertTitle>
                <AlertDescription>
                  {requestErrorDescription(
                    mutation.error,
                    "请重新载入列表，并检查图片地址、权益范围和修改说明。"
                  )}
                </AlertDescription>
              </Alert>
            ) : null}

            <FieldSet>
              <FieldLegend>基本信息</FieldLegend>
              <div className="grid gap-4 md:grid-cols-[1fr_8rem]">
                <FieldGroup>
                  <Field
                    data-invalid={validation.nameError !== null || undefined}
                  >
                    <FieldLabel htmlFor="medal-name">勋章名称</FieldLabel>
                    <Input
                      id="medal-name"
                      value={draft.name}
                      maxLength={100}
                      aria-invalid={validation.nameError !== null}
                      onChange={(event) => update("name", event.target.value)}
                    />
                    {validation.nameError ? (
                      <FieldError>{validation.nameError}</FieldError>
                    ) : null}
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="medal-description">说明</FieldLabel>
                    <Textarea
                      id="medal-description"
                      rows={3}
                      maxLength={500}
                      value={draft.description}
                      onChange={(event) =>
                        update("description", event.target.value)
                      }
                    />
                  </Field>
                </FieldGroup>
                <div className="flex min-h-32 items-center justify-center rounded-lg border bg-muted/20 p-3">
                  {previewImage ? (
                    <img
                      src={previewImage}
                      alt="勋章图片预览"
                      className="max-h-24 max-w-24 object-contain"
                    />
                  ) : (
                    <div className="flex flex-col items-center gap-2 text-muted-foreground">
                      <MedalIcon className="size-10" />
                      <span className="text-xs">图片预览</span>
                    </div>
                  )}
                </div>
              </div>
              <div className="grid gap-4 md:grid-cols-2">
                <ImagePathField
                  id="medal-small-image"
                  label="小图地址"
                  value={draft.imageSmallPath}
                  error={validation.smallImageError}
                  onChange={(value) => update("imageSmallPath", value)}
                />
                <ImagePathField
                  id="medal-large-image"
                  label="大图地址"
                  value={draft.imageLargePath}
                  error={validation.largeImageError}
                  onChange={(value) => update("imageLargePath", value)}
                />
              </div>
            </FieldSet>

            <FieldSet>
              <FieldLegend>获取与展示</FieldLegend>
              <div className="grid gap-4 md:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="medal-acquisition">获取方式</FieldLabel>
                  <Select
                    items={acquisitionItems}
                    value={draft.acquisitionMethod}
                    onValueChange={(value) =>
                      value &&
                      update("acquisitionMethod", value as AcquisitionMethod)
                    }
                  >
                    <SelectTrigger id="medal-acquisition" className="w-full">
                      <SelectValue>{selectedMethod?.label}</SelectValue>
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {acquisitionItems.map((item) => (
                          <SelectItem key={item.value} value={item.value}>
                            {item.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {selectedMethod?.description}
                  </FieldDescription>
                </Field>
                <NumberField
                  id="medal-price"
                  label="价格（魔力值）"
                  value={draft.price}
                  error={validation.priceError}
                  onChange={(value) => update("price", value)}
                />
                <NumberField
                  id="medal-duration"
                  label="有效期（天）"
                  value={draft.durationDays}
                  error={validation.durationError}
                  description="0 表示永久有效。"
                  onChange={(value) => update("durationDays", value)}
                />
                <NumberField
                  id="medal-priority"
                  label="显示顺序"
                  value={draft.priority}
                  error={validation.priorityError}
                  onChange={(value) => update("priority", value)}
                />
                <NumberField
                  id="medal-inventory"
                  label="库存"
                  value={draft.inventory}
                  error={validation.inventoryError}
                  description="留空表示不限量。"
                  onChange={(value) => update("inventory", value)}
                />
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <SwitchField
                  id="medal-display"
                  title="在勋章页面显示"
                  description="关闭只隐藏入口，不会删除用户已持有的勋章。"
                  checked={draft.displayOnPage}
                  onCheckedChange={(value) => update("displayOnPage", value)}
                />
                <SwitchField
                  id="medal-pool"
                  title="允许进入奖池"
                  description="供随机勋章或活动奖励选择使用。"
                  checked={draft.poolEligible}
                  onCheckedChange={(value) => update("poolEligible", value)}
                />
              </div>
            </FieldSet>

            <FieldSet>
              <FieldLegend>勋章权益</FieldLegend>
              <FieldDescription>
                百分比最多保留两位小数。魔力值加成修改后会为受影响用户追加当前权益版本，历史奖励不回算。
              </FieldDescription>
              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                <PercentField
                  id="medal-upload-bonus"
                  label="上传加成（%）"
                  value={draft.uploadBonusPercent}
                  error={validation.uploadBonusError}
                  onChange={(value) => update("uploadBonusPercent", value)}
                />
                <PercentField
                  id="medal-download-discount"
                  label="下载减免（%）"
                  value={draft.downloadDiscountPercent}
                  error={validation.downloadDiscountError}
                  onChange={(value) => update("downloadDiscountPercent", value)}
                />
                <PercentField
                  id="medal-magic-bonus"
                  label="做种魔力加成（%）"
                  value={draft.magicBonusPercent}
                  error={validation.magicBonusError}
                  onChange={(value) => update("magicBonusPercent", value)}
                />
                <NumberField
                  id="medal-invite-bonus"
                  label="邀请奖励"
                  value={draft.inviteBonus}
                  error={validation.inviteBonusError}
                  onChange={(value) => update("inviteBonus", value)}
                />
                <NumberField
                  id="medal-periodic-magic"
                  label="周期魔力值"
                  value={draft.periodicRewardMagic}
                  error={validation.periodicRewardError}
                  onChange={(value) => update("periodicRewardMagic", value)}
                />
                <Field>
                  <FieldLabel htmlFor="medal-reward-cycle">发放周期</FieldLabel>
                  <Select
                    items={rewardCycleItems}
                    value={draft.rewardCycle}
                    onValueChange={(value) =>
                      value &&
                      update("rewardCycle", value as MedalDraft["rewardCycle"])
                    }
                  >
                    <SelectTrigger id="medal-reward-cycle" className="w-full">
                      <SelectValue>
                        {
                          rewardCycleItems.find(
                            (item) => item.value === draft.rewardCycle
                          )?.label
                        }
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {rewardCycleItems.map((item) => (
                          <SelectItem key={item.value} value={item.value}>
                            {item.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
              </div>
              {medal &&
              (BigInt(medal.conditions_count) > 0n ||
                BigInt(medal.privileges_count) > 0n) ? (
                <Alert>
                  <ShieldCheckIcon />
                  <AlertTitle>已迁移的资格与权限会继续保留</AlertTitle>
                  <AlertDescription>
                    当前包含 {medal.conditions_count} 条资格条件和{" "}
                    {medal.privileges_count}{" "}
                    条权限定义；本表单不会清空这些旧站规则。
                  </AlertDescription>
                </Alert>
              ) : null}
            </FieldSet>

            <FieldSet>
              <FieldLegend>变更留痕</FieldLegend>
              <Field
                data-invalid={validation.reasonError !== null || undefined}
              >
                <FieldLabel htmlFor="medal-reason">修改说明</FieldLabel>
                <Textarea
                  id="medal-reason"
                  rows={3}
                  minLength={10}
                  maxLength={500}
                  value={draft.reason}
                  aria-invalid={validation.reasonError !== null}
                  onChange={(event) => update("reason", event.target.value)}
                />
                <FieldDescription>
                  {Array.from(draft.reason.trim()).length} / 500，至少 10
                  个字符。
                </FieldDescription>
                {validation.reasonError ? (
                  <FieldError>{validation.reasonError}</FieldError>
                ) : null}
              </Field>
            </FieldSet>
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
              {medal ? "保存修改" : "创建勋章"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ImagePathField({
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
    <Field data-invalid={error !== null || undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        value={value}
        aria-invalid={error !== null}
        placeholder="/uploads/medals/example.webp"
        onChange={(event) => onChange(event.target.value)}
      />
      <FieldDescription>支持站内 / 路径或 HTTPS 地址。</FieldDescription>
      {error ? <FieldError>{error}</FieldError> : null}
    </Field>
  )
}

function NumberField({
  id,
  label,
  value,
  error,
  description,
  onChange,
}: {
  id: string
  label: string
  value: string
  error: string | null
  description?: string
  onChange: (value: string) => void
}) {
  return (
    <Field data-invalid={error !== null || undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        inputMode="numeric"
        value={value}
        aria-invalid={error !== null}
        onChange={(event) => onChange(event.target.value)}
      />
      {description ? <FieldDescription>{description}</FieldDescription> : null}
      {error ? <FieldError>{error}</FieldError> : null}
    </Field>
  )
}

function PercentField(props: Parameters<typeof NumberField>[0]) {
  return <NumberField {...props} />
}

function SwitchField({
  id,
  title,
  description,
  checked,
  onCheckedChange,
}: {
  id: string
  title: string
  description: string
  checked: boolean
  onCheckedChange: (value: boolean) => void
}) {
  return (
    <Field
      orientation="horizontal"
      className="rounded-lg border bg-muted/15 p-3"
    >
      <FieldContent>
        <FieldTitle>{title}</FieldTitle>
        <FieldDescription>{description}</FieldDescription>
      </FieldContent>
      <Switch
        id={id}
        checked={checked}
        onCheckedChange={onCheckedChange}
        aria-label={title}
      />
    </Field>
  )
}

function toDraft(medal?: MedalDefinition): MedalDraft {
  return {
    name: medal?.name ?? "",
    description: medal?.description ?? "",
    imageLargePath: medal?.image_large_path ?? "",
    imageSmallPath: medal?.image_small_path ?? "",
    acquisitionMethod: medal?.acquisition_method ?? "grant",
    price: medal?.price ?? "0",
    durationDays: String(medal?.duration_days ?? 0),
    displayOnPage: medal?.display_on_page ?? true,
    priority: String(medal?.priority ?? 0),
    uploadBonusPercent: basisPointsToPercent(medal?.upload_bonus_bps ?? 0),
    downloadDiscountPercent: basisPointsToPercent(
      medal?.download_discount_bps ?? 0
    ),
    magicBonusPercent: basisPointsToPercent(medal?.magic_bonus_bps ?? 0),
    inviteBonus: medal?.invite_bonus ?? "0",
    poolEligible: medal?.pool_eligible ?? false,
    periodicRewardMagic: medal?.periodic_reward_magic ?? "0",
    rewardCycle: medal?.reward_cycle ?? "none",
    inventory: medal?.inventory ?? "",
    saleBeginAt: medal?.sale_begin_at ?? null,
    saleEndAt: medal?.sale_end_at ?? null,
    reason: "",
  }
}

function validateDraft(draft: MedalDraft) {
  const nameLength = Array.from(draft.name.trim()).length
  const nameError =
    nameLength < 1 || nameLength > 100 ? "名称应为 1–100 个字符。" : null
  const smallImageError = imagePathError(draft.imageSmallPath)
  const largeImageError = imagePathError(draft.imageLargePath)
  const priceError = unsignedError(draft.price)
  const durationError = boundedIntegerError(draft.durationDays, 0, 36_500)
  const priorityError = boundedIntegerError(draft.priority, 0, 1_000_000)
  const inventoryError = draft.inventory.trim()
    ? unsignedError(draft.inventory)
    : null
  const inviteBonusError = unsignedError(draft.inviteBonus)
  const periodicRewardError = unsignedError(draft.periodicRewardMagic)
  const uploadBonusBPS = percentToBasisPoints(draft.uploadBonusPercent)
  const downloadDiscountBPS = percentToBasisPoints(
    draft.downloadDiscountPercent
  )
  const magicBonusBPS = percentToBasisPoints(draft.magicBonusPercent)
  const uploadBonusError =
    uploadBonusBPS === null ? "请输入 0–1000，最多两位小数。" : null
  const downloadDiscountError =
    downloadDiscountBPS === null ? "请输入 0–1000，最多两位小数。" : null
  const magicBonusError =
    magicBonusBPS === null ? "请输入 0–1000，最多两位小数。" : null
  const reasonLength = Array.from(draft.reason.trim()).length
  const reasonError =
    reasonLength < 10 || reasonLength > 500
      ? "修改说明应为 10–500 个字符。"
      : null
  const errors = [
    nameError,
    smallImageError,
    largeImageError,
    priceError,
    durationError,
    priorityError,
    inventoryError,
    inviteBonusError,
    periodicRewardError,
    uploadBonusError,
    downloadDiscountError,
    magicBonusError,
    reasonError,
  ]
  const body: MedalDefinitionWriteRequest | null = errors.some(Boolean)
    ? null
    : {
        name: draft.name.trim(),
        description: optionalText(draft.description),
        image_large_path: optionalText(draft.imageLargePath),
        image_small_path: optionalText(draft.imageSmallPath),
        acquisition_method: draft.acquisitionMethod,
        price: draft.price.trim(),
        duration_days: Number(draft.durationDays),
        display_on_page: draft.displayOnPage,
        priority: Number(draft.priority),
        upload_bonus_bps: uploadBonusBPS!,
        download_discount_bps: downloadDiscountBPS!,
        magic_bonus_bps: magicBonusBPS!,
        invite_bonus: draft.inviteBonus.trim(),
        pool_eligible: draft.poolEligible,
        periodic_reward_magic: draft.periodicRewardMagic.trim(),
        reward_cycle: draft.rewardCycle === "none" ? null : draft.rewardCycle,
        sale_begin_at: draft.saleBeginAt,
        sale_end_at: draft.saleEndAt,
        inventory: draft.inventory.trim() || null,
        reason: draft.reason.trim(),
      }

  return {
    body,
    nameError,
    smallImageError,
    largeImageError,
    priceError,
    durationError,
    priorityError,
    inventoryError,
    inviteBonusError,
    periodicRewardError,
    uploadBonusError,
    downloadDiscountError,
    magicBonusError,
    reasonError,
  }
}

function imagePathError(value: string) {
  const normalized = value.trim()
  if (!normalized) return null
  if (
    normalized.startsWith("/") &&
    !normalized.startsWith("//") &&
    !normalized.includes("..")
  ) {
    return null
  }
  try {
    const parsed = new URL(normalized)
    return parsed.protocol === "https:" && !parsed.username && !parsed.password
      ? null
      : "请使用站内 / 路径或 HTTPS 地址。"
  } catch {
    return "请使用站内 / 路径或 HTTPS 地址。"
  }
}

function unsignedError(value: string) {
  const normalized = value.trim()
  if (!/^\d{1,19}$/.test(normalized)) return "请输入非负整数。"
  return BigInt(normalized) <= 9_223_372_036_854_775_807n
    ? null
    : "数值超出允许范围。"
}

function boundedIntegerError(value: string, minimum: number, maximum: number) {
  if (!/^\d+$/.test(value.trim())) {
    return "请输入 " + minimum + "–" + maximum + " 的整数。"
  }
  const numeric = Number(value)
  return Number.isSafeInteger(numeric) &&
    numeric >= minimum &&
    numeric <= maximum
    ? null
    : "请输入 " + minimum + "–" + maximum + " 的整数。"
}

function percentToBasisPoints(value: string) {
  if (!/^\d{1,4}(?:\.\d{1,2})?$/.test(value.trim())) return null
  const percentage = Number(value)
  if (!Number.isFinite(percentage) || percentage < 0 || percentage > 1000) {
    return null
  }
  return Math.round(percentage * 100)
}

function basisPointsToPercent(value: number) {
  return new Intl.NumberFormat("zh-CN", {
    useGrouping: false,
    maximumFractionDigits: 2,
  }).format(value / 100)
}

function optionalText(value: string) {
  return value.trim() || null
}
