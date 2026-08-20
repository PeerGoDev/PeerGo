import { type FormEvent, useMemo, useState } from "react"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  HandCoinsIcon,
  RefreshCwIcon,
  SendIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { Textarea } from "~/components/ui/textarea"
import {
  useCreateMemberGift,
  useMemberGiftOverview,
} from "~/features/economy/api/member-gifts.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function MemberGiftCard({
  userId,
  csrfToken,
  magicBalance,
}: {
  userId: string
  csrfToken: string
  magicBalance: string
}) {
  const overview = useMemberGiftOverview(userId)
  const mutation = useCreateMemberGift(userId)
  const [recipientNumericId, setRecipientNumericId] = useState("")
  const [amount, setAmount] = useState("")
  const [message, setMessage] = useState("")
  const [validationError, setValidationError] = useState("")

  const feePreview = useMemo(() => {
    if (!overview.data || !positiveInteger(amount)) return null
    const gross = BigInt(amount)
    const feeBps = BigInt(overview.data.policy.settings.fee_bps)
    const fee = feeBps === 0n ? 0n : (gross * feeBps + 9_999n) / 10_000n
    return { fee: fee.toString(), net: (gross - fee).toString() }
  }, [amount, overview.data])

  if (overview.isPending) {
    return <Skeleton className="h-80 rounded-lg" />
  }
  if (overview.isError || !overview.data) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>成员赠送暂时无法读取</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(overview.error, "请稍后重试。")}
            </AlertDescription>
          </Alert>
          <Button
            variant="outline"
            size="sm"
            className="mt-3"
            onClick={() => void overview.refetch()}
          >
            <RefreshCwIcon data-icon="inline-start" />
            重试
          </Button>
        </CardContent>
      </Card>
    )
  }

  const data = overview.data
  const policy = data.policy.settings

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidationError("")
    mutation.reset()
    const error = validateGift({
      recipientNumericId,
      ownNumericId: data.my_numeric_id,
      amount,
      message,
      balance: magicBalance,
      minimum: policy.minimum_amount,
      maximum: policy.maximum_amount,
      remaining: data.remaining_today,
      feePreview,
    })
    if (error) {
      setValidationError(error)
      return
    }
    mutation.mutate(
      {
        csrfToken,
        idempotencyKey: globalThis.crypto.randomUUID(),
        recipientNumericId,
        amount,
        message: message.trim(),
      },
      {
        onSuccess: () => {
          setRecipientNumericId("")
          setAmount("")
          setMessage("")
        },
      }
    )
  }

  return (
    <Card className="gap-0 py-0">
      <CardHeader className="border-b p-6 pb-4">
        <CardTitle className="flex items-center gap-2">
          <HandCoinsIcon data-icon="inline-start" />
          成员赠送
        </CardTitle>
        <CardDescription>
          使用对方的成员数字 ID 赠送整数魔力值；赠送不会增加经验。
        </CardDescription>
        <div className="mt-2 flex flex-wrap gap-2 text-xs text-muted-foreground">
          <Badge variant="outline">我的 ID：{data.my_numeric_id}</Badge>
          <Badge variant="secondary">
            今日剩余 {formatInteger(data.remaining_today)}
          </Badge>
          <span>
            单笔 {formatInteger(policy.minimum_amount)}–
            {formatInteger(policy.maximum_amount)}，手续费{" "}
            {formatFee(policy.fee_bps)}
          </span>
        </div>
      </CardHeader>
      <CardContent className="grid gap-5 p-6 xl:grid-cols-[minmax(18rem,0.8fr)_minmax(0,1.2fr)]">
        <div>
          {!policy.enabled ? (
            <Alert>
              <CircleAlertIcon />
              <AlertTitle>成员赠送暂未开放</AlertTitle>
              <AlertDescription>
                当前规则和历史记录仍可查看，管理员启用后才能新增赠送。
              </AlertDescription>
            </Alert>
          ) : (
            <form onSubmit={submit} noValidate>
              <FieldGroup>
                <Field data-invalid={Boolean(validationError)}>
                  <FieldLabel htmlFor="member-gift-recipient">
                    对方成员 ID
                  </FieldLabel>
                  <Input
                    id="member-gift-recipient"
                    value={recipientNumericId}
                    onChange={(event) =>
                      setRecipientNumericId(event.target.value.trim())
                    }
                    inputMode="numeric"
                    pattern="[0-9]*"
                    autoComplete="off"
                    placeholder="例如 1234"
                    disabled={mutation.isPending}
                  />
                  <FieldDescription>
                    请让对方从本页复制“我的 ID”；这里不填写 UUID。
                  </FieldDescription>
                </Field>
                <Field data-invalid={Boolean(validationError)}>
                  <FieldLabel htmlFor="member-gift-amount">
                    赠送魔力值
                  </FieldLabel>
                  <Input
                    id="member-gift-amount"
                    value={amount}
                    onChange={(event) => setAmount(event.target.value.trim())}
                    inputMode="numeric"
                    pattern="[0-9]*"
                    autoComplete="off"
                    placeholder={`余额 ${formatInteger(magicBalance)}`}
                    disabled={mutation.isPending}
                  />
                  {feePreview ? (
                    <FieldDescription>
                      对方到账 {formatInteger(feePreview.net)}，手续费{" "}
                      {formatInteger(feePreview.fee)}
                    </FieldDescription>
                  ) : null}
                </Field>
                <Field data-invalid={Boolean(validationError)}>
                  <FieldLabel htmlFor="member-gift-message">
                    留言（可选）
                  </FieldLabel>
                  <Textarea
                    id="member-gift-message"
                    value={message}
                    onChange={(event) => setMessage(event.target.value)}
                    rows={3}
                    maxLength={200}
                    placeholder="写一句简短说明…"
                    disabled={mutation.isPending}
                  />
                  <FieldError>{validationError}</FieldError>
                </Field>
                {mutation.isSuccess ? (
                  <Alert>
                    <CircleCheckIcon />
                    <AlertTitle>赠送成功</AlertTitle>
                    <AlertDescription>
                      {mutation.data.counterparty.display_name} 已收到{" "}
                      {formatInteger(mutation.data.net_amount)} 魔力值。
                    </AlertDescription>
                  </Alert>
                ) : null}
                {mutation.isError ? (
                  <Alert variant="destructive">
                    <CircleAlertIcon />
                    <AlertTitle>赠送未完成</AlertTitle>
                    <AlertDescription>
                      {requestErrorDescription(
                        mutation.error,
                        "请核对信息后重试。"
                      )}
                    </AlertDescription>
                  </Alert>
                ) : null}
                <Button type="submit" disabled={mutation.isPending}>
                  {mutation.isPending ? (
                    <Spinner data-icon="inline-start" />
                  ) : (
                    <SendIcon data-icon="inline-start" />
                  )}
                  {mutation.isPending ? "提交中…" : "确认赠送"}
                </Button>
              </FieldGroup>
            </form>
          )}
        </div>

        <div className="min-w-0">
          <h3 className="mb-3 text-sm font-medium">最近收发记录</h3>
          {data.history.length === 0 ? (
            <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
              还没有成员赠送记录。
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>时间</TableHead>
                  <TableHead>成员</TableHead>
                  <TableHead>留言</TableHead>
                  <TableHead className="text-right">变动</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.history.map((gift) => {
                  const received = gift.direction === "received"
                  return (
                    <TableRow key={gift.id}>
                      <TableCell className="text-xs whitespace-nowrap text-muted-foreground">
                        {formatCompactDateTime(gift.occurred_at)}
                      </TableCell>
                      <TableCell>
                        <div className="font-medium">
                          {gift.counterparty.display_name}
                        </div>
                        <div className="text-xs text-muted-foreground">
                          #{gift.counterparty.numeric_id} ·{" "}
                          {gift.counterparty.username}
                        </div>
                      </TableCell>
                      <TableCell className="max-w-52 truncate text-muted-foreground">
                        {gift.message || "—"}
                      </TableCell>
                      <TableCell
                        className={
                          received
                            ? "text-right text-emerald-600 dark:text-emerald-400"
                            : "text-right text-destructive"
                        }
                      >
                        {received ? "+" : "−"}
                        {formatInteger(
                          received ? gift.net_amount : gift.gross_amount
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function validateGift(input: {
  recipientNumericId: string
  ownNumericId: string
  amount: string
  message: string
  balance: string
  minimum: string
  maximum: string
  remaining: string
  feePreview: { fee: string; net: string } | null
}) {
  if (!positiveInteger(input.recipientNumericId))
    return "请输入有效的成员数字 ID。"
  if (input.recipientNumericId === input.ownNumericId)
    return "不能给自己赠送魔力值。"
  if (!positiveInteger(input.amount)) return "请输入大于 0 的整数魔力值。"
  const amount = BigInt(input.amount)
  if (amount < BigInt(input.minimum) || amount > BigInt(input.maximum)) {
    return `单笔金额必须在 ${formatInteger(input.minimum)} 到 ${formatInteger(input.maximum)} 之间。`
  }
  if (amount > BigInt(input.balance)) return "赠送金额超过当前魔力值余额。"
  if (amount > BigInt(input.remaining)) return "赠送金额超过今天的剩余额度。"
  if (!input.feePreview || BigInt(input.feePreview.net) < 1n)
    return "扣除手续费后，对方到账金额必须大于 0。"
  if ([...input.message.trim()].length > 200) return "留言不能超过 200 个字符。"
  return ""
}

function positiveInteger(value: string) {
  return /^[1-9][0-9]*$/.test(value)
}

function formatFee(feeBps: number) {
  return `${(feeBps / 100).toFixed(feeBps % 100 === 0 ? 0 : 2)}%`
}
