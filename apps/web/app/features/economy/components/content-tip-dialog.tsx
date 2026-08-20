import * as React from "react"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  CoinsIcon,
  GiftIcon,
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
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type ContentTipTarget,
  useContentTipOverview,
  useCreateContentTip,
} from "~/features/economy/api/content-tips.queries"
import { useEconomyOverview } from "~/features/economy/api/economy.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatInteger } from "~/shared/formatters/integer"
import { cn } from "~/lib/utils"

const presetAmounts = [1, 5, 10, 50, 100, 500] as const

export function ContentTipDialog({
  target,
  userId,
  csrfToken,
  buttonVariant = "ghost",
  buttonSize = "xs",
  className,
  iconOnly = false,
}: {
  target: ContentTipTarget
  userId?: string
  csrfToken?: string
  buttonVariant?: React.ComponentProps<typeof Button>["variant"]
  buttonSize?: React.ComponentProps<typeof Button>["size"]
  className?: string
  iconOnly?: boolean
}) {
  const [open, setOpen] = React.useState(false)
  const [amount, setAmount] = React.useState("10")
  const [validationError, setValidationError] = React.useState("")
  const requestId = React.useRef<string>(undefined)
  const overview = useContentTipOverview(userId, open)
  const economy = useEconomyOverview(open ? userId : undefined)
  const mutation = useCreateContentTip(userId)

  if (!userId || !csrfToken) return null

  function changeAmount(value: string) {
    setAmount(value)
    setValidationError("")
    requestId.current = undefined
    mutation.reset()
  }

  function changeOpen(next: boolean) {
    if (!next && mutation.isPending) return
    setOpen(next)
    if (!next) {
      setValidationError("")
      requestId.current = undefined
      mutation.reset()
    }
  }

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!overview.data || !economy.data || !csrfToken) return
    const error = validateTipAmount(
      amount,
      overview.data.policy.settings.minimum_amount,
      overview.data.policy.settings.maximum_amount,
      overview.data.remaining_today,
      economy.data.magic_balance
    )
    if (error) {
      setValidationError(error)
      return
    }
    const idempotencyKey = requestId.current ?? globalThis.crypto.randomUUID()
    requestId.current = idempotencyKey
    mutation.mutate({
      target,
      amount,
      csrfToken,
      idempotencyKey,
    })
  }

  return (
    <>
      <Button
        type="button"
        variant={buttonVariant}
        size={buttonSize}
        className={cn(className, iconOnly && "border-0")}
        aria-label={iconOnly ? "打赏" : undefined}
        onClick={() => setOpen(true)}
      >
        <GiftIcon data-icon="inline-start" />
        {iconOnly ? null : "打赏"}
      </Button>
      <Dialog open={open} onOpenChange={changeOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <CoinsIcon className="text-warning" />
              打赏内容作者
            </DialogTitle>
            <DialogDescription className="line-clamp-2">
              {targetLabel(target.kind)} · {target.title}
            </DialogDescription>
          </DialogHeader>

          {overview.isPending || economy.isPending ? (
            <div className="space-y-3 py-2">
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : overview.isError ||
            !overview.data ||
            economy.isError ||
            !economy.data ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>打赏规则暂时无法读取</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(
                  overview.error ?? economy.error,
                  "请稍后重试，当前不会扣除魔力值。"
                )}
              </AlertDescription>
            </Alert>
          ) : !overview.data.policy.settings.enabled ? (
            <Alert>
              <CircleAlertIcon />
              <AlertTitle>内容打赏暂未开放</AlertTitle>
              <AlertDescription>
                管理员启用内容打赏后，这里才会允许提交。
              </AlertDescription>
            </Alert>
          ) : (
            <form id="content-tip-form" onSubmit={submit} noValidate>
              <FieldGroup className="gap-4">
                <Field data-invalid={Boolean(validationError) || undefined}>
                  <FieldLabel>快捷金额</FieldLabel>
                  <ToggleGroup
                    value={
                      presetAmounts.some((value) => String(value) === amount)
                        ? [amount]
                        : []
                    }
                    onValueChange={(values) => {
                      const value = values[0]
                      if (value) changeAmount(value)
                    }}
                    variant="outline"
                    spacing={1}
                    className="grid w-full grid-cols-3"
                    disabled={mutation.isPending}
                  >
                    {presetAmounts.map((value) => (
                      <ToggleGroupItem
                        key={value}
                        value={String(value)}
                        className="w-full"
                      >
                        {value}
                      </ToggleGroupItem>
                    ))}
                  </ToggleGroup>
                </Field>
                <Field data-invalid={Boolean(validationError) || undefined}>
                  <FieldLabel htmlFor="content-tip-amount">
                    自定义整数金额
                  </FieldLabel>
                  <Input
                    id="content-tip-amount"
                    value={amount}
                    inputMode="numeric"
                    pattern="[0-9]*"
                    autoComplete="off"
                    disabled={mutation.isPending}
                    onChange={(event) =>
                      changeAmount(event.target.value.trim())
                    }
                  />
                  <FieldDescription>
                    余额 {formatInteger(economy.data.magic_balance)} · 今日剩余{" "}
                    {formatInteger(overview.data.remaining_today)} · 单笔{" "}
                    {formatInteger(
                      overview.data.policy.settings.minimum_amount
                    )}
                    –
                    {formatInteger(
                      overview.data.policy.settings.maximum_amount
                    )}
                  </FieldDescription>
                  <FieldError>{validationError}</FieldError>
                </Field>
                {mutation.isSuccess ? (
                  <Alert>
                    <CircleCheckIcon />
                    <AlertTitle>打赏成功</AlertTitle>
                    <AlertDescription>
                      {mutation.data.counterparty.display_name} 已收到{" "}
                      {formatInteger(mutation.data.net_amount)} 魔力值。
                    </AlertDescription>
                  </Alert>
                ) : null}
                {mutation.isError ? (
                  <Alert variant="destructive">
                    <CircleAlertIcon />
                    <AlertTitle>打赏未完成</AlertTitle>
                    <AlertDescription>
                      {requestErrorDescription(
                        mutation.error,
                        "请核对金额后重试。"
                      )}
                    </AlertDescription>
                  </Alert>
                ) : null}
              </FieldGroup>
            </form>
          )}

          <DialogFooter>
            <DialogClose
              render={<Button variant="outline" />}
              disabled={mutation.isPending}
            >
              {mutation.isSuccess ? "完成" : "取消"}
            </DialogClose>
            {overview.data?.policy.settings.enabled &&
            economy.data &&
            !mutation.isSuccess ? (
              <Button
                form="content-tip-form"
                type="submit"
                disabled={mutation.isPending || economy.isPending}
              >
                {mutation.isPending ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <GiftIcon data-icon="inline-start" />
                )}
                {mutation.isPending ? "提交中…" : "确认打赏"}
              </Button>
            ) : null}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function validateTipAmount(
  amount: string,
  minimum: string,
  maximum: string,
  remaining: string,
  balance: string
) {
  if (!/^[1-9]\d*$/.test(amount)) return "请输入大于 0 的整数魔力值。"
  const value = BigInt(amount)
  if (value < BigInt(minimum) || value > BigInt(maximum)) {
    return `单笔金额须在 ${formatInteger(minimum)}–${formatInteger(maximum)} 之间。`
  }
  if (value > BigInt(remaining)) return "该金额超过今天剩余的打赏额度。"
  if (value > BigInt(balance)) return "当前魔力值余额不足。"
  return ""
}

function targetLabel(kind: ContentTipTarget["kind"]) {
  if (kind === "torrent") return "种子"
  if (kind === "post") return "动态"
  return "评论"
}
