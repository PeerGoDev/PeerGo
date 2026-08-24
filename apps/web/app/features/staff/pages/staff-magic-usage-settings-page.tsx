import { type FormEvent, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  CoinsIcon,
  GiftIcon,
  HandCoinsIcon,
  RefreshCwIcon,
  SaveIcon,
  ShieldCheckIcon,
  ShoppingCartIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
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
  type ContentTipPolicy,
  contentTipPolicyListQueryOptions,
  useIssueContentTipPolicy,
} from "~/features/staff/api/content-tip-administration.queries"
import {
  type MemberGiftPolicy,
  memberGiftPolicyListQueryOptions,
  useIssueMemberGiftPolicy,
} from "~/features/staff/api/member-gift-administration.queries"
import { economySettingsQueryOptions } from "~/features/staff/api/operations.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffSettingsValueTable } from "~/features/staff/components/staff-settings-value-table"
import { hasCapability } from "~/features/staff/model/capability"
import { requestErrorDescription } from "~/shared/api/problem"
import { MetricCard } from "~/shared/components/metric-card"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function StaffMagicUsageSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="economy.seedingreward.policy.read"
      pageHeader={{
        title: "魔力值使用规则",
        description: "查看种子购买、赠送、打赏和退款的实际开放状态。",
      }}
    >
      {({ session, capabilities }) => (
        <MagicUsageContent
          csrfToken={session.csrf_token}
          canIssueMemberGiftPolicy={hasCapability(
            capabilities,
            "economy.membergift.policy.issue"
          )}
          canIssueContentTipPolicy={hasCapability(
            capabilities,
            "economy.contenttip.policy.issue"
          )}
        />
      )}
    </StaffAccessGate>
  )
}

function MagicUsageContent({
  csrfToken,
  canIssueMemberGiftPolicy,
  canIssueContentTipPolicy,
}: {
  csrfToken: string
  canIssueMemberGiftPolicy: boolean
  canIssueContentTipPolicy: boolean
}) {
  const settings = useQuery(economySettingsQueryOptions())
  const giftPolicies = useQuery(memberGiftPolicyListQueryOptions())
  const tipPolicies = useQuery(contentTipPolicyListQueryOptions())
  if (settings.isPending || giftPolicies.isPending || tipPolicies.isPending)
    return <SettingsSkeleton />
  if (
    settings.isError ||
    !settings.data ||
    giftPolicies.isError ||
    !giftPolicies.data ||
    tipPolicies.isError ||
    !tipPolicies.data
  ) {
    return (
      <SettingsError
        retry={() => {
          void settings.refetch()
          void giftPolicies.refetch()
          void tipPolicies.refetch()
        }}
      />
    )
  }
  const { usage, transactions } = settings.data
  const currentGiftPolicy = giftPolicies.data.items[0]
  const currentTipPolicy = tipPolicies.data.items[0]
  if (!currentGiftPolicy || !currentTipPolicy)
    return (
      <SettingsError
        retry={() => {
          void giftPolicies.refetch()
          void tipPolicies.refetch()
        }}
      />
    )

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">魔力值使用规则</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            PeerGo
            只保留一种站内资产；购买、赠送和打赏分别控制，不再提供魔力值与 PT
            币兑换。
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={
            settings.isFetching ||
            giftPolicies.isFetching ||
            tipPolicies.isFetching
          }
          onClick={() => {
            void settings.refetch()
            void giftPolicies.refetch()
            void tipPolicies.refetch()
          }}
        >
          <RefreshCwIcon
            data-icon="inline-start"
            className={
              settings.isFetching ||
              giftPolicies.isFetching ||
              tipPolicies.isFetching
                ? "animate-spin"
                : undefined
            }
          />
          刷新
        </Button>
      </header>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <MetricCard
          title="统一资产"
          value={usage.currency_name}
          description={
            usage.pt_coin_enabled ? "同时启用 PT 币" : "不再启用 PT 币"
          }
          icon={<CoinsIcon />}
          tone="primary"
        />
        <MetricCard
          title="计量方式"
          value={usage.whole_units_only ? "整数" : "小数"}
          description="展示、入账和迁移使用同一口径"
          icon={<ShieldCheckIcon />}
          tone="positive"
        />
        <MetricCard
          title="种子购买"
          value={usage.torrent_purchase_connected ? "已接通" : "暂未接通"}
          description="是否收费由各个种子单独决定"
          icon={<ShoppingCartIcon />}
          tone={usage.torrent_purchase_connected ? "positive" : "muted"}
        />
        <MetricCard
          title="成员赠送"
          value={
            !usage.member_gift_connected
              ? "暂未接通"
              : currentGiftPolicy.settings.enabled
                ? "已开放"
                : "已关闭"
          }
          description="打赏仍是独立功能"
          icon={<HandCoinsIcon />}
          tone={currentGiftPolicy.settings.enabled ? "positive" : "muted"}
        />
        <MetricCard
          title="内容打赏"
          value={
            !usage.content_tip_connected
              ? "暂未接通"
              : currentTipPolicy.settings.enabled
                ? "已开放"
                : "已关闭"
          }
          description="种子、动态与评论共用同一政策"
          icon={<GiftIcon />}
          tone={currentTipPolicy.settings.enabled ? "positive" : "muted"}
        />
      </div>

      <Alert>
        <ShieldCheckIcon />
        <AlertTitle>余额规则已经统一</AlertTitle>
        <AlertDescription>
          用户余额只能是整数且不能透支；Rousi 的魔力值与 PT
          币在迁移时合并为一次可核对的期初余额。账目只追加新记录，退款使用反向记录，不直接修改旧记录。
        </AlertDescription>
      </Alert>

      <TransferPolicyEditor
        kind="member-gift"
        key={currentGiftPolicy.revision}
        current={currentGiftPolicy}
        history={giftPolicies.data.items}
        csrfToken={csrfToken}
        canIssue={canIssueMemberGiftPolicy}
      />

      <TransferPolicyEditor
        kind="content-tip"
        key={currentTipPolicy.revision}
        current={currentTipPolicy}
        history={tipPolicies.data.items}
        csrfToken={csrfToken}
        canIssue={canIssueContentTipPolicy}
      />

      <div className="grid gap-4 xl:grid-cols-2">
        <Card className="gap-0 py-0">
          <CardHeader className="p-6 pb-4">
            <CardTitle>成员可使用方式</CardTitle>
            <CardDescription>接通状态和运营开关分开显示</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <StaffSettingsValueTable
              rows={[
                {
                  label: "购买付费种子",
                  value: usage.torrent_purchase_connected ? (
                    <Badge variant="outline">已接通</Badge>
                  ) : (
                    readinessBadge(usage.torrent_purchase_supported)
                  ),
                },
                {
                  label: "用户互赠魔力值",
                  value: (
                    <Badge
                      variant={
                        currentGiftPolicy.settings.enabled
                          ? "outline"
                          : "secondary"
                      }
                    >
                      {currentGiftPolicy.settings.enabled ? "已开放" : "已关闭"}
                    </Badge>
                  ),
                },
                {
                  label: "评论或动态打赏",
                  value: (
                    <Badge
                      variant={
                        currentTipPolicy.settings.enabled
                          ? "outline"
                          : "secondary"
                      }
                    >
                      {currentTipPolicy.settings.enabled ? "已开放" : "已关闭"}
                    </Badge>
                  ),
                },
                {
                  label: "退款记录",
                  value: readinessBadge(usage.refund_supported),
                },
              ]}
            />
          </CardContent>
        </Card>

        <Card className="gap-0 py-0">
          <CardHeader className="p-6 pb-4">
            <CardTitle>当前账目</CardTitle>
            <CardDescription>只显示笔数，不包含任何用户资料</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <StaffSettingsValueTable
              valueHeader="记录数"
              rows={[
                {
                  label: "Rousi 迁移期初余额",
                  value: formatInteger(transactions.legacy_opening),
                },
                {
                  label: "做种奖励",
                  value: formatInteger(transactions.seeding_reward),
                },
                {
                  label: "活动奖励",
                  value: formatInteger(transactions.activity_reward),
                },
                {
                  label: "种子购买",
                  value: formatInteger(transactions.torrent_purchase),
                },
                {
                  label: "成员赠送",
                  value: formatInteger(transactions.member_gift),
                },
                { label: "内容打赏", value: formatInteger(transactions.tip) },
                { label: "退款", value: formatInteger(transactions.refund) },
                {
                  label: "管理员调整",
                  value: formatInteger(transactions.adjustment),
                },
              ]}
            />
          </CardContent>
        </Card>
      </div>
    </StaffPageFrame>
  )
}

type TransferPolicy = MemberGiftPolicy | ContentTipPolicy

function TransferPolicyEditor({
  kind,
  current,
  history,
  csrfToken,
  canIssue,
}: {
  kind: "member-gift" | "content-tip"
  current: TransferPolicy
  history: TransferPolicy[]
  csrfToken: string
  canIssue: boolean
}) {
  const memberGiftMutation = useIssueMemberGiftPolicy()
  const contentTipMutation = useIssueContentTipPolicy()
  const mutation =
    kind === "member-gift" ? memberGiftMutation : contentTipMutation
  const isGift = kind === "member-gift"
  const featureName = isGift ? "成员赠送" : "内容打赏"
  const featureDescription = isGift
    ? "保留 PtYes 熟悉的开关、单笔范围和手续费，并增加每日赠送总额上限。新修订只影响之后的赠送。"
    : "种子、动态和评论共用同一套整数魔力值规则；关闭只阻止新增打赏，历史账本保持不变。"
  const fieldPrefix = isGift ? "member-gift" : "content-tip"
  const [enabled, setEnabled] = useState(current.settings.enabled)
  const [minimumAmount, setMinimumAmount] = useState(
    current.settings.minimum_amount
  )
  const [maximumAmount, setMaximumAmount] = useState(
    current.settings.maximum_amount
  )
  const [dailyLimit, setDailyLimit] = useState(
    current.settings.daily_gross_limit
  )
  const [feePercent, setFeePercent] = useState(
    formatFeePercent(current.settings.fee_bps)
  )
  const [reason, setReason] = useState("")
  const [validationError, setValidationError] = useState("")
  const disabled = !canIssue || mutation.isPending

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidationError("")
    mutation.reset()
    const error = validatePolicy({
      minimumAmount,
      maximumAmount,
      dailyLimit,
      feePercent,
      reason,
    })
    if (error) {
      setValidationError(error)
      return
    }
    mutation.mutate({
      csrfToken,
      idempotencyKey: globalThis.crypto.randomUUID(),
      settings: {
        enabled,
        minimum_amount: minimumAmount,
        maximum_amount: maximumAmount,
        daily_gross_limit: dailyLimit,
        fee_bps: Math.round(Number(feePercent) * 100),
      },
      reason: reason.trim(),
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{featureName}</CardTitle>
        <CardDescription>{featureDescription}</CardDescription>
        <CardAction>
          <Badge variant="outline">当前规则</Badge>
        </CardAction>
      </CardHeader>
      <CardContent>
        <form id={`${fieldPrefix}-policy-form`} onSubmit={submit} noValidate>
          <FieldGroup>
            <Field
              orientation="horizontal"
              className="rounded-lg border bg-muted/15 p-3"
              data-disabled={disabled}
            >
              <FieldContent>
                <FieldLabel htmlFor={`${fieldPrefix}-policy-enabled`}>
                  开放{featureName}
                </FieldLabel>
                <FieldDescription>
                  关闭只会阻止新增{isGift ? "赠送" : "打赏"}
                  ，不影响历史账本和接收方已有余额。
                </FieldDescription>
              </FieldContent>
              <Switch
                id={`${fieldPrefix}-policy-enabled`}
                checked={enabled}
                onCheckedChange={setEnabled}
                disabled={disabled}
              />
            </Field>

            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
              <PolicyNumberField
                id={`${fieldPrefix}-minimum`}
                label="单笔最小魔力值"
                value={minimumAmount}
                onChange={setMinimumAmount}
                disabled={disabled}
              />
              <PolicyNumberField
                id={`${fieldPrefix}-maximum`}
                label="单笔最大魔力值"
                value={maximumAmount}
                onChange={setMaximumAmount}
                disabled={disabled}
              />
              <PolicyNumberField
                id={`${fieldPrefix}-daily-limit`}
                label={`每日${isGift ? "赠送" : "打赏"}上限`}
                value={dailyLimit}
                onChange={setDailyLimit}
                disabled={disabled}
                description="按中国标准时间零点重置"
              />
              <Field data-invalid={Boolean(validationError)}>
                <FieldLabel htmlFor={`${fieldPrefix}-fee`}>
                  手续费（%）
                </FieldLabel>
                <Input
                  id={`${fieldPrefix}-fee`}
                  value={feePercent}
                  onChange={(event) => setFeePercent(event.target.value.trim())}
                  inputMode="decimal"
                  disabled={disabled}
                />
                <FieldDescription>0–50%，不足 1 点时向上取整</FieldDescription>
              </Field>
            </div>

            {canIssue ? (
              <Field data-invalid={Boolean(validationError)}>
                <FieldLabel htmlFor={`${fieldPrefix}-policy-reason`}>
                  变更原因
                </FieldLabel>
                <Textarea
                  id={`${fieldPrefix}-policy-reason`}
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  maxLength={1000}
                  rows={3}
                  disabled={disabled}
                  placeholder="可留空；系统会自动记录变更原因"
                />
                <FieldError>{validationError}</FieldError>
              </Field>
            ) : (
              <Alert>
                <ShieldCheckIcon />
                <AlertTitle>当前权限为只读</AlertTitle>
                <AlertDescription>
                  只有{featureName}政策签发权限可以保存新规则。
                </AlertDescription>
              </Alert>
            )}

            {mutation.isSuccess ? (
              <Alert>
                <CircleCheckIcon />
                <AlertTitle>{featureName}设置已生效</AlertTitle>
                <AlertDescription>
                  新规则已追加，历史赠送继续使用原规则。
                </AlertDescription>
              </Alert>
            ) : null}
            {mutation.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>{featureName}设置未保存</AlertTitle>
                <AlertDescription>
                  {requestErrorDescription(
                    mutation.error,
                    "请刷新页面后重试。"
                  )}
                </AlertDescription>
              </Alert>
            ) : null}
          </FieldGroup>
        </form>

        <div className="mt-6">
          <h3 className="mb-2 text-sm font-medium">最近政策记录</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>创建时间</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>单笔范围</TableHead>
                <TableHead>每日上限</TableHead>
                <TableHead>手续费</TableHead>
                <TableHead>原因</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {history.slice(0, 5).map((policy) => (
                <TableRow key={policy.revision}>
                  <TableCell className="text-xs whitespace-nowrap text-muted-foreground">
                    {formatDateTime(policy.created_at)}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        policy.settings.enabled ? "outline" : "secondary"
                      }
                    >
                      {policy.settings.enabled ? "开放" : "关闭"}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {formatInteger(policy.settings.minimum_amount)}–
                    {formatInteger(policy.settings.maximum_amount)}
                  </TableCell>
                  <TableCell>
                    {formatInteger(policy.settings.daily_gross_limit)}
                  </TableCell>
                  <TableCell>
                    {formatFeePercent(policy.settings.fee_bps)}%
                  </TableCell>
                  <TableCell className="max-w-80 truncate text-muted-foreground">
                    {policy.reason}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </CardContent>
      {canIssue ? (
        <CardFooter className="justify-end">
          <Button
            type="submit"
            form={`${fieldPrefix}-policy-form`}
            disabled={mutation.isPending}
          >
            {mutation.isPending ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <SaveIcon data-icon="inline-start" />
            )}
            {mutation.isPending ? "保存中…" : "保存并立即生效"}
          </Button>
        </CardFooter>
      ) : null}
    </Card>
  )
}

function PolicyNumberField({
  id,
  label,
  value,
  onChange,
  disabled,
  description,
}: {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  disabled: boolean
  description?: string
}) {
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        value={value}
        onChange={(event) => onChange(event.target.value.trim())}
        inputMode="numeric"
        pattern="[0-9]*"
        disabled={disabled}
      />
      {description ? <FieldDescription>{description}</FieldDescription> : null}
    </Field>
  )
}

function validatePolicy(input: {
  minimumAmount: string
  maximumAmount: string
  dailyLimit: string
  feePercent: string
  reason: string
}) {
  if (
    ![input.minimumAmount, input.maximumAmount, input.dailyLimit].every(
      positiveInteger
    )
  ) {
    return "单笔范围和每日上限必须是大于 0 的整数。"
  }
  const minimum = BigInt(input.minimumAmount)
  const maximum = BigInt(input.maximumAmount)
  const daily = BigInt(input.dailyLimit)
  if (minimum > maximum) return "单笔最小值不能大于单笔最大值。"
  if (maximum > 1_000_000_000n) return "单笔最大值不能超过 1,000,000,000。"
  if (daily < maximum || daily > 1_000_000_000_000n) {
    return "每日上限不能小于单笔最大值，也不能超过 1,000,000,000,000。"
  }
  if (!/^(?:[0-9]|[1-4][0-9]|50)(?:\.[0-9]{1,2})?$/.test(input.feePercent)) {
    return "手续费必须是 0–50 之间、最多两位小数的百分比。"
  }
  if ([...input.reason.trim()].length > 1000)
    return "变更原因不能超过 1000 个字符。"
  return ""
}

function positiveInteger(value: string) {
  return /^[1-9][0-9]*$/.test(value)
}

function formatFeePercent(feeBps: number) {
  return (feeBps / 100).toFixed(feeBps % 100 === 0 ? 0 : 2)
}

function readinessBadge(supported: boolean) {
  return (
    <Badge variant={supported ? "outline" : "secondary"}>
      {supported ? "账务已准备，入口未开放" : "未开放"}
    </Badge>
  )
}

function SettingsError({ retry }: { retry: () => void }) {
  return (
    <StaffPageFrame className="gap-4">
      <Alert variant="destructive">
        <CircleAlertIcon />
        <AlertTitle>魔力值使用规则暂时无法读取</AlertTitle>
        <AlertDescription>请检查 Core 与后台会话后重试。</AlertDescription>
      </Alert>
      <Button variant="outline" className="w-fit" onClick={retry}>
        <RefreshCwIcon data-icon="inline-start" />
        重试
      </Button>
    </StaffPageFrame>
  )
}

function SettingsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载魔力值使用规则">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-[36rem] rounded-lg" />
      <div className="grid gap-4 xl:grid-cols-2">
        <Skeleton className="h-80 rounded-lg" />
        <Skeleton className="h-80 rounded-lg" />
      </div>
    </StaffPageFrame>
  )
}
