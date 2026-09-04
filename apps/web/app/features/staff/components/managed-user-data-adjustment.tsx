import * as React from "react"
import {
  CircleAlertIcon,
  DatabaseZapIcon,
  MinusIcon,
  PlusIcon,
} from "lucide-react"

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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type ManagedUserAdjustmentRequest,
  type ManagedUserDetail,
  useAdjustManagedUserData,
} from "~/features/staff/api/user-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatInteger } from "~/shared/formatters/integer"

type AdjustmentField = ManagedUserAdjustmentRequest["field"]
type AdjustmentOperation = ManagedUserAdjustmentRequest["operation"]

const adjustmentFields: Array<{
  value: AdjustmentField
  label: string
  unit: string
  description: string
}> = [
  {
    value: "uploaded_bytes",
    label: "上传量",
    unit: "GiB",
    description: "同时调整实际上传和计奖上传。",
  },
  {
    value: "downloaded_bytes",
    label: "下载量",
    unit: "GiB",
    description: "同时调整实际下载和计费下载。",
  },
  {
    value: "magic_balance",
    label: "魔力值",
    unit: "点",
    description: "通过平衡账本写入，不直接改余额投影。",
  },
  {
    value: "experience",
    label: "经验值",
    unit: "点",
    description: "写入经验账本，并按当前等级规则重新计算。",
  },
  {
    value: "remaining_invites",
    label: "可用邀请",
    unit: "个",
    description: "调整用户当前可以创建的邀请数量。",
  },
  {
    value: "donation_amount",
    label: "捐赠金额",
    unit: "CNY",
    description: "调整累计捐赠金额，最多保留两位小数。",
  },
]

export function ManagedUserDataAdjustment({
  detail,
  csrfToken,
  disabled,
}: {
  detail: ManagedUserDetail
  csrfToken: string
  disabled: boolean
}) {
  const [open, setOpen] = React.useState(false)
  const [field, setField] = React.useState<AdjustmentField>("uploaded_bytes")
  const [operation, setOperation] =
    React.useState<AdjustmentOperation>("increase")
  const [amount, setAmount] = React.useState("")
  const [reason, setReason] = React.useState("")
  const mutation = useAdjustManagedUserData()
  const metadata = adjustmentFields.find((item) => item.value === field)!
  const normalizedAmount = normalizeAdjustmentAmount(field, amount)
  const amountError =
    amount.trim() && !normalizedAmount
      ? adjustmentAmountError(field)
      : amount.trim()
        ? null
        : "请输入大于 0 的调整数量。"

  function resetAttempt() {
    mutation.reset()
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!normalizedAmount || mutation.isPending) return
    await mutation.mutateAsync({
      userId: detail.id,
      csrfToken,
      idempotencyKey: crypto.randomUUID(),
      body: {
        field,
        operation,
        amount: normalizedAmount,
        reason: reason.trim(),
        expected_user_version: detail.version,
      },
    })
    setOpen(false)
    setAmount("")
    setReason("")
  }

  return (
    <section className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-muted/20 p-3">
      <div>
        <h2 className="font-heading font-medium">账户数据调整</h2>
        <p className="text-xs text-muted-foreground">
          增减流量、魔力、经验、邀请或捐赠；每次操作均写入不可变审计记录。
        </p>
      </div>
      <Dialog
        open={open}
        onOpenChange={(next) => !mutation.isPending && setOpen(next)}
      >
        <DialogTrigger
          render={
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={disabled}
              title={disabled ? "不能调整当前管理员自己的账户数据" : undefined}
            />
          }
        >
          <DatabaseZapIcon data-icon="inline-start" />
          增减数据
        </DialogTrigger>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>调整 {detail.display_name} 的账户数据</DialogTitle>
            <DialogDescription>
              当前值会在提交前再次按版本校验，避免覆盖其他管理员刚完成的变更。
            </DialogDescription>
          </DialogHeader>

          {mutation.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>账户数据未调整</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(
                  mutation.error,
                  "请刷新账户详情，核对当前余额后重试。"
                )}
              </AlertDescription>
            </Alert>
          ) : null}

          <form id="managed-user-adjustment-form" onSubmit={submit} noValidate>
            <FieldGroup className="gap-4">
              <Field>
                <FieldLabel htmlFor="managed-user-adjustment-field">
                  数据项目
                </FieldLabel>
                <Select
                  value={field}
                  onValueChange={(value) => {
                    if (!value) return
                    setField(value as AdjustmentField)
                    setAmount("")
                    resetAttempt()
                  }}
                  disabled={mutation.isPending}
                >
                  <SelectTrigger id="managed-user-adjustment-field">
                    <SelectValue>{metadata.label}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {adjustmentFields.map((item) => (
                        <SelectItem key={item.value} value={item.value}>
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldDescription>
                  当前：{currentValue(detail, field)}。{metadata.description}
                </FieldDescription>
              </Field>

              <Field>
                <FieldLabel>调整方向</FieldLabel>
                <ToggleGroup
                  value={[operation]}
                  onValueChange={(values) => {
                    const value = values[0] as AdjustmentOperation | undefined
                    if (!value) return
                    setOperation(value)
                    resetAttempt()
                  }}
                  variant="outline"
                  spacing={1}
                  className="grid w-full grid-cols-2"
                  disabled={mutation.isPending}
                >
                  <ToggleGroupItem
                    value="increase"
                    className="h-10 w-full data-pressed:bg-success data-pressed:text-success-foreground"
                  >
                    <PlusIcon data-icon="inline-start" />
                    增加
                  </ToggleGroupItem>
                  <ToggleGroupItem
                    value="decrease"
                    className="data-pressed:text-destructive-foreground h-10 w-full data-pressed:bg-destructive"
                  >
                    <MinusIcon data-icon="inline-start" />
                    减少
                  </ToggleGroupItem>
                </ToggleGroup>
              </Field>

              <Field data-invalid={Boolean(amountError)}>
                <FieldLabel htmlFor="managed-user-adjustment-amount">
                  数量（{metadata.unit}）
                </FieldLabel>
                <Input
                  id="managed-user-adjustment-amount"
                  value={amount}
                  inputMode="decimal"
                  autoComplete="off"
                  disabled={mutation.isPending}
                  aria-invalid={Boolean(amountError)}
                  placeholder={
                    field.endsWith("_bytes") ? "例如 10.5" : "例如 10"
                  }
                  onChange={(event) => {
                    setAmount(event.target.value.trim())
                    resetAttempt()
                  }}
                />
                <FieldDescription>
                  {field.endsWith("_bytes")
                    ? "流量按 GiB 输入，服务端以整数字节保存。"
                    : "请输入正数，增减方向由上方选项决定。"}
                </FieldDescription>
                {amountError ? <FieldError>{amountError}</FieldError> : null}
              </Field>

              <Field>
                <FieldLabel htmlFor="managed-user-adjustment-reason">
                  变更说明（可留空）
                </FieldLabel>
                <Textarea
                  id="managed-user-adjustment-reason"
                  rows={3}
                  maxLength={500}
                  value={reason}
                  disabled={mutation.isPending}
                  placeholder="留空时系统自动生成对应的审计理由"
                  onChange={(event) => {
                    setReason(event.target.value)
                    resetAttempt()
                  }}
                />
                <FieldDescription>
                  {Array.from(reason.trim()).length} / 500
                </FieldDescription>
              </Field>
            </FieldGroup>
          </form>

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
              type="submit"
              form="managed-user-adjustment-form"
              disabled={Boolean(amountError) || mutation.isPending}
            >
              {mutation.isPending ? <Spinner data-icon="inline-start" /> : null}
              确认调整
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}

function normalizeAdjustmentAmount(field: AdjustmentField, raw: string) {
  const value = raw.trim()
  if (field === "uploaded_bytes" || field === "downloaded_bytes") {
    const match = /^(\d+)(?:\.(\d{1,6}))?$/.exec(value)
    if (!match) return null
    const fraction = match[2] ?? ""
    const scale = 10n ** BigInt(fraction.length)
    const units = BigInt(match[1]) * scale + BigInt(fraction || "0")
    const bytes = (units * 1_073_741_824n) / scale
    return bytes > 0n && bytes <= 9_223_372_036_854_775_807n
      ? bytes.toString()
      : null
  }
  if (field === "magic_balance") {
    return validInteger(value, 9_223_372_036_854_775_807n)
  }
  if (field === "remaining_invites") {
    return validInteger(value, 1_000_000n)
  }
  if (field === "donation_amount") {
    return /^(?:0|[1-9]\d{0,9})(?:\.\d{1,2})?$/.test(value) && Number(value) > 0
      ? value
      : null
  }
  return /^(?:0|[1-9]\d{0,17})(?:\.\d{1,20})?$/.test(value) &&
    !/^0(?:\.0+)?$/.test(value)
    ? value
    : null
}

function validInteger(value: string, maximum: bigint) {
  if (!/^[1-9]\d*$/.test(value)) return null
  try {
    return BigInt(value) <= maximum ? value : null
  } catch {
    return null
  }
}

function adjustmentAmountError(field: AdjustmentField) {
  if (field === "uploaded_bytes" || field === "downloaded_bytes") {
    return "请输入大于 0 的 GiB 数量，最多保留 6 位小数。"
  }
  if (field === "donation_amount") {
    return "请输入大于 0 的金额，最多 10 位整数和 2 位小数。"
  }
  if (field === "experience") {
    return "请输入大于 0 的经验值，最多 18 位整数和 20 位小数。"
  }
  return "请输入允许范围内的大于 0 的整数。"
}

function currentValue(detail: ManagedUserDetail, field: AdjustmentField) {
  switch (field) {
    case "uploaded_bytes":
      return formatBytes(detail.uploaded_bytes)
    case "downloaded_bytes":
      return formatBytes(detail.downloaded_bytes)
    case "magic_balance":
      return `${formatInteger(detail.magic_balance)} 点`
    case "experience":
      return `${detail.experience} 点`
    case "remaining_invites":
      return `${formatInteger(detail.remaining_invites)} 个`
    case "donation_amount":
      return `¥${detail.donation_amount}`
  }
}
