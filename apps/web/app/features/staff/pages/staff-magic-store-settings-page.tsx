import * as React from "react"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  FlameIcon,
  Gamepad2Icon,
  PlusIcon,
  RefreshCwIcon,
  SaveIcon,
  ShieldCheckIcon,
  StoreIcon,
  TicketIcon,
  Trash2Icon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Card,
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
  type MagicStoreAdminOverview,
  type MagicStoreSettings,
  useMagicStoreAdmin,
  useUpdateMagicStoreSettings,
} from "~/features/economy/api/magic-store.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import { requestErrorDescription } from "~/shared/api/problem"
import { MetricCard } from "~/shared/components/metric-card"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

type FieldSpec = {
  path: string[]
  label: string
  description?: string
}

type Section = {
  title: string
  description: string
  toggle: string[]
  fields: FieldSpec[]
}

const SHOP_SECTIONS: Section[] = [
  {
    title: "永久邀请名额",
    description: "购买后直接增加永久邀请名额，受成员邀请资格约束。",
    toggle: ["invite", "enabled"],
    fields: [
      { path: ["invite", "price"], label: "价格" },
      {
        path: ["invite", "monthly_limit"],
        label: "每月限购",
        description: "0 表示不限",
      },
    ],
  },
  {
    title: "临时邀请",
    description: "到期作废，生成邀请码时优先消耗。",
    toggle: ["temporary_invite", "enabled"],
    fields: [
      { path: ["temporary_invite", "price"], label: "价格" },
      { path: ["temporary_invite", "valid_days"], label: "有效天数" },
      {
        path: ["temporary_invite", "monthly_limit"],
        label: "每月限购",
        description: "0 表示不限",
      },
    ],
  },
  {
    title: "VIP",
    description: "在现有 VIP 到期时间上顺延，永久 VIP 不可购买。",
    toggle: ["vip", "enabled"],
    fields: [
      { path: ["vip", "price"], label: "价格" },
      { path: ["vip", "duration_days"], label: "天数" },
    ],
  },
  {
    title: "彩色 ID",
    description: "预设色板，纯色与渐变两档定价。",
    toggle: ["name_color", "enabled"],
    fields: [
      { path: ["name_color", "solid_price"], label: "纯色价格" },
      { path: ["name_color", "gradient_price"], label: "渐变价格" },
      { path: ["name_color", "duration_days"], label: "有效天数" },
    ],
  },
  {
    title: "补签卡",
    description: "补签窗口内最早漏签的一天。",
    toggle: ["makeup_card", "enabled"],
    fields: [
      { path: ["makeup_card", "price"], label: "价格" },
      { path: ["makeup_card", "window_days"], label: "可补签天数" },
      { path: ["makeup_card", "maximum_holding"], label: "持有上限" },
    ],
  },
]

const GAME_SECTIONS: Section[] = [
  {
    title: "刮刮乐",
    description: "按权重随机抽取奖项，奖金来自游戏庄家账户。",
    toggle: ["scratch", "enabled"],
    fields: [
      { path: ["scratch", "price"], label: "单张价格" },
      { path: ["scratch", "daily_limit"], label: "每人每日上限" },
    ],
  },
  {
    title: "每日彩票",
    description: "每天定时开奖，种子摘要提前公布；无人中奖的奖金滚存。",
    toggle: ["lottery", "enabled"],
    fields: [
      { path: ["lottery", "ticket_price"], label: "单注价格" },
      { path: ["lottery", "maximum_tickets_per_round"], label: "每人每期上限" },
      {
        path: ["lottery", "draw_hour"],
        label: "开奖整点",
        description: "0–23，按站点时区",
      },
      {
        path: ["lottery", "burn_bps"],
        label: "销毁比例（基点）",
        description: "10000 = 100%",
      },
    ],
  },
  {
    title: "擂台",
    description: "两名成员押注对决，平台从奖池抽取手续费。",
    toggle: ["pvp", "enabled"],
    fields: [
      { path: ["pvp", "minimum_stake"], label: "最低押注" },
      { path: ["pvp", "maximum_stake"], label: "最高押注" },
      {
        path: ["pvp", "fee_bps"],
        label: "手续费（基点）",
        description: "最高 5000",
      },
      { path: ["pvp", "maximum_open_per_member"], label: "每人同时开擂数" },
      { path: ["pvp", "expiry_hours"], label: "无人应战过期（小时）" },
    ],
  },
]

export function StaffMagicStoreSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="economy.magicstore.manage.read"
      pageHeader={{
        title: "魔力商店与游戏",
        description: "设置商店价格、限购和游戏规则。",
      }}
    >
      {({ session, capabilities }) => (
        <MagicStoreSettingsContent
          csrfToken={session.csrf_token}
          canUpdate={hasCapability(capabilities, "economy.magicstore.update")}
        />
      )}
    </StaffAccessGate>
  )
}

function MagicStoreSettingsContent({
  csrfToken,
  canUpdate,
}: {
  csrfToken: string
  canUpdate: boolean
}) {
  const overview = useMagicStoreAdmin()
  if (overview.isPending) return <SettingsSkeleton />
  if (overview.isError || !overview.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>魔力商店设置暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              overview.error,
              "请检查 Core 与后台会话后重试。"
            )}
          </AlertDescription>
        </Alert>
        <Button
          variant="outline"
          className="w-fit"
          onClick={() => void overview.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </StaffPageFrame>
    )
  }
  return (
    <SettingsEditor
      key={overview.data.version}
      overview={overview.data}
      csrfToken={csrfToken}
      canUpdate={canUpdate}
    />
  )
}

function SettingsEditor({
  overview,
  csrfToken,
  canUpdate,
}: {
  overview: MagicStoreAdminOverview
  csrfToken: string
  canUpdate: boolean
}) {
  const update = useUpdateMagicStoreSettings()
  const [draft, setDraft] = React.useState<MagicStoreSettings>(() =>
    structuredClone(overview.settings)
  )
  const [reason, setReason] = React.useState("")
  const [validationError, setValidationError] = React.useState("")
  const disabled = !canUpdate || update.isPending
  const expectedReturn = scratchReturnBPS(draft)

  const setPath = (path: string[], value: unknown) =>
    setDraft((current) => {
      const next = structuredClone(current) as unknown as Record<
        string,
        unknown
      >
      let cursor = next
      for (const key of path.slice(0, -1))
        cursor = cursor[key] as Record<string, unknown>
      cursor[path[path.length - 1]!] = value
      return next as unknown as MagicStoreSettings
    })

  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    update.reset()
    const error = validateSettings(draft, reason)
    setValidationError(error)
    if (error) return
    update.mutate({
      csrfToken,
      settings: draft,
      expectedVersion: overview.version,
      reason: reason.trim(),
    })
  }

  return (
    <StaffPageFrame className="gap-4">
      <header>
        <h1 className="font-heading text-lg font-semibold tracking-title">
          魔力商店与游戏
        </h1>
        <p className="mt-1 text-xs text-muted-foreground">
          当前版本 v{overview.version}，更新于{" "}
          {formatDateTime(overview.updated_at)}
          。所有改动立即生效并保留修订记录。
        </p>
      </header>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          title="商店回收"
          value={formatInteger(overview.accounts.shop_sink)}
          description="商店消耗的魔力值总额"
          icon={<StoreIcon />}
          tone="primary"
        />
        <MetricCard
          title="游戏庄家"
          value={formatInteger(overview.accounts.game_house)}
          description="刮刮乐与擂台手续费净额"
          icon={<FlameIcon />}
          tone="muted"
        />
        <MetricCard
          title="彩票奖池"
          value={formatInteger(overview.accounts.lottery_pool)}
          description="含本期销售与滚存"
          icon={<TicketIcon />}
          tone="positive"
        />
        <MetricCard
          title="擂台托管"
          value={formatInteger(overview.accounts.pvp_escrow)}
          description="进行中擂台冻结的押注"
          icon={<Gamepad2Icon />}
          tone="muted"
        />
      </div>

      <form
        id="magic-store-settings"
        onSubmit={submit}
        noValidate
        className="flex flex-col gap-4"
      >
        <Card>
          <CardHeader>
            <CardTitle>总开关</CardTitle>
            <CardDescription>
              关闭后成员仍可查看页面与历史记录，但不能购买或参与。
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-3">
            <ToggleField
              id="shop-enabled"
              label="开放魔力商店"
              checked={draft.shop_enabled}
              disabled={disabled}
              onChange={(value) => setPath(["shop_enabled"], value)}
            />
            <ToggleField
              id="games-enabled"
              label="开放游戏中心"
              checked={draft.games_enabled}
              disabled={disabled}
              onChange={(value) => setPath(["games_enabled"], value)}
            />
            <Field>
              <FieldLabel htmlFor="magic-timezone">站点时区</FieldLabel>
              <Input
                id="magic-timezone"
                value={draft.timezone}
                disabled={disabled}
                onChange={(event) =>
                  setPath(["timezone"], event.target.value.trim())
                }
              />
              <FieldDescription>
                用于限购月份、每日次数与开奖时间。
              </FieldDescription>
            </Field>
          </CardContent>
        </Card>

        <div className="grid gap-4 xl:grid-cols-2">
          {SHOP_SECTIONS.map((section) => (
            <SectionCard
              key={section.title}
              section={section}
              draft={draft}
              disabled={disabled}
              setPath={setPath}
            />
          ))}
        </div>

        <div className="grid gap-4 xl:grid-cols-2">
          {GAME_SECTIONS.map((section) => (
            <SectionCard
              key={section.title}
              section={section}
              draft={draft}
              disabled={disabled}
              setPath={setPath}
            >
              {section.toggle[0] === "scratch" ? (
                <RowsEditor
                  title={`奖项（理论返还率 ${(expectedReturn / 100).toFixed(2)}%）`}
                  columns={[
                    { key: "label", label: "名称", text: true },
                    { key: "payout", label: "奖金" },
                    { key: "weight", label: "权重" },
                  ]}
                  rows={draft.scratch.prizes}
                  disabled={disabled}
                  maxRows={12}
                  blank={{ label: "新奖项", payout: 0, weight: 1 }}
                  onChange={(rows) => setPath(["scratch", "prizes"], rows)}
                />
              ) : section.toggle[0] === "lottery" ? (
                <RowsEditor
                  title="奖级（占比合计不得超过 100%）"
                  columns={[
                    { key: "label", label: "名称", text: true },
                    { key: "winners", label: "名额" },
                    { key: "share_bps", label: "占比（基点）" },
                  ]}
                  rows={draft.lottery.tiers}
                  disabled={disabled}
                  maxRows={10}
                  blank={{ label: "新奖级", winners: 1, share_bps: 100 }}
                  onChange={(rows) => setPath(["lottery", "tiers"], rows)}
                />
              ) : null}
            </SectionCard>
          ))}
        </div>

        <Card>
          <CardHeader>
            <CardTitle>保存</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {canUpdate ? (
              <Field data-invalid={Boolean(validationError)}>
                <FieldLabel htmlFor="magic-store-reason">变更原因</FieldLabel>
                <Textarea
                  id="magic-store-reason"
                  value={reason}
                  rows={3}
                  maxLength={500}
                  disabled={disabled}
                  onChange={(event) => setReason(event.target.value)}
                  placeholder="10–500 字，会记录在修订历史中"
                />
                <FieldError>{validationError}</FieldError>
              </Field>
            ) : (
              <Alert>
                <ShieldCheckIcon />
                <AlertTitle>当前权限为只读</AlertTitle>
                <AlertDescription>
                  只有拥有魔力商店更新权限的管理员可以保存。
                </AlertDescription>
              </Alert>
            )}
            {update.isSuccess ? (
              <Alert>
                <CircleCheckIcon />
                <AlertTitle>设置已生效</AlertTitle>
                <AlertDescription>
                  新的价格与规则已立即对成员生效。
                </AlertDescription>
              </Alert>
            ) : null}
            {update.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>设置未保存</AlertTitle>
                <AlertDescription>
                  {requestErrorDescription(update.error, "请刷新页面后重试。")}
                </AlertDescription>
              </Alert>
            ) : null}
          </CardContent>
          {canUpdate ? (
            <CardFooter className="justify-end">
              <Button
                type="submit"
                form="magic-store-settings"
                disabled={update.isPending}
              >
                {update.isPending ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <SaveIcon data-icon="inline-start" />
                )}
                {update.isPending ? "保存中…" : "保存并立即生效"}
              </Button>
            </CardFooter>
          ) : null}
        </Card>
      </form>

      <Card>
        <CardHeader>
          <CardTitle>修订记录</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>版本</TableHead>
                <TableHead>操作人</TableHead>
                <TableHead>原因</TableHead>
                <TableHead className="text-right">时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {overview.revisions.map((revision) => (
                <TableRow key={revision.version}>
                  <TableCell className="tabular-nums">
                    v{revision.version}
                  </TableCell>
                  <TableCell>{revision.actor_username ?? "系统"}</TableCell>
                  <TableCell className="max-w-96 truncate text-muted-foreground">
                    {revision.reason}
                  </TableCell>
                  <TableCell className="text-right text-xs whitespace-nowrap text-muted-foreground">
                    {formatDateTime(revision.occurred_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </StaffPageFrame>
  )
}

function SectionCard({
  section,
  draft,
  disabled,
  setPath,
  children,
}: {
  section: Section
  draft: MagicStoreSettings
  disabled: boolean
  setPath: (path: string[], value: unknown) => void
  children?: React.ReactNode
}) {
  const id = section.toggle.join("-")
  return (
    <Card>
      <CardHeader>
        <CardTitle>{section.title}</CardTitle>
        <CardDescription>{section.description}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <ToggleField
          id={id}
          label={`开放${section.title}`}
          checked={Boolean(readPath(draft, section.toggle))}
          disabled={disabled}
          onChange={(value) => setPath(section.toggle, value)}
        />
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {section.fields.map((field) => {
            const fieldId = field.path.join("-")
            return (
              <Field key={fieldId}>
                <FieldLabel htmlFor={fieldId}>{field.label}</FieldLabel>
                <Input
                  id={fieldId}
                  inputMode="numeric"
                  value={String(readPath(draft, field.path) ?? "")}
                  disabled={disabled}
                  onChange={(event) => {
                    const value = event.target.value.trim()
                    if (/^\d{0,12}$/.test(value))
                      setPath(field.path, Number(value || 0))
                  }}
                />
                {field.description ? (
                  <FieldDescription>{field.description}</FieldDescription>
                ) : null}
              </Field>
            )
          })}
        </div>
        {children}
      </CardContent>
    </Card>
  )
}

function ToggleField({
  id,
  label,
  checked,
  disabled,
  onChange,
}: {
  id: string
  label: string
  checked: boolean
  disabled: boolean
  onChange: (value: boolean) => void
}) {
  return (
    <Field
      orientation="horizontal"
      className="rounded-lg border bg-muted/15 p-3"
      data-disabled={disabled}
    >
      <FieldContent>
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
      </FieldContent>
      <Switch
        id={id}
        checked={checked}
        onCheckedChange={onChange}
        disabled={disabled}
      />
    </Field>
  )
}

type Row = Record<string, string | number>

function RowsEditor<T extends Row>({
  title,
  columns,
  rows,
  disabled,
  maxRows,
  blank,
  onChange,
}: {
  title: string
  columns: { key: keyof T & string; label: string; text?: boolean }[]
  rows: T[]
  disabled: boolean
  maxRows: number
  blank: T
  onChange: (rows: T[]) => void
}) {
  const patch = (index: number, key: string, value: string | number) =>
    onChange(
      rows.map((row, current) =>
        current === index ? { ...row, [key]: value } : row
      )
    )
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-medium">{title}</h3>
        <Button
          type="button"
          variant="outline"
          size="xs"
          disabled={disabled || rows.length >= maxRows}
          onClick={() => onChange([...rows, { ...blank }])}
        >
          <PlusIcon data-icon="inline-start" />
          添加
        </Button>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            {columns.map((column) => (
              <TableHead key={column.key}>{column.label}</TableHead>
            ))}
            <TableHead className="w-10" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row, index) => (
            <TableRow key={index}>
              {columns.map((column) => (
                <TableCell key={column.key} className="py-1">
                  <Input
                    aria-label={`${column.label} ${index + 1}`}
                    value={String(row[column.key])}
                    disabled={disabled}
                    inputMode={column.text ? undefined : "numeric"}
                    maxLength={column.text ? 20 : 12}
                    onChange={(event) => {
                      const value = event.target.value
                      if (column.text) patch(index, column.key, value)
                      else if (/^\d{0,12}$/.test(value.trim()))
                        patch(index, column.key, Number(value.trim() || 0))
                    }}
                  />
                </TableCell>
              ))}
              <TableCell className="py-1">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`删除第 ${index + 1} 行`}
                  disabled={disabled || rows.length <= 1}
                  onClick={() =>
                    onChange(rows.filter((_, current) => current !== index))
                  }
                >
                  <Trash2Icon />
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function readPath(settings: MagicStoreSettings, path: string[]) {
  let cursor: unknown = settings
  for (const key of path) cursor = (cursor as Record<string, unknown>)[key]
  return cursor as number | boolean | string | undefined
}

function scratchReturnBPS(settings: MagicStoreSettings) {
  const total = settings.scratch.prizes.reduce(
    (sum, prize) => sum + prize.weight,
    0
  )
  if (total <= 0 || settings.scratch.price <= 0) return 0
  const expected = settings.scratch.prizes.reduce(
    (sum, prize) => sum + prize.payout * prize.weight,
    0
  )
  return Math.floor((expected * 10000) / total / settings.scratch.price)
}

function validateSettings(settings: MagicStoreSettings, reason: string) {
  const length = [...reason.trim()].length
  if (length < 10 || length > 500) return "变更原因需要 10–500 个字符。"
  if (settings.pvp.minimum_stake > settings.pvp.maximum_stake)
    return "擂台最低押注不能大于最高押注。"
  if (
    settings.lottery.tiers.reduce((sum, tier) => sum + tier.share_bps, 0) >
    10000
  )
    return "彩票奖级占比合计不能超过 10000 基点。"
  if (settings.scratch.prizes.length < 2) return "刮刮乐至少需要两个奖项。"
  if (settings.scratch.prizes.some((prize) => !prize.label.trim()))
    return "刮刮乐奖项名称不能为空。"
  if (settings.lottery.tiers.some((tier) => !tier.label.trim()))
    return "彩票奖级名称不能为空。"
  if (scratchReturnBPS(settings) > 10000)
    return "刮刮乐理论返还率不能超过 100%。"
  return ""
}

function SettingsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载魔力商店设置">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-32 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-[32rem] rounded-lg" />
    </StaffPageFrame>
  )
}
