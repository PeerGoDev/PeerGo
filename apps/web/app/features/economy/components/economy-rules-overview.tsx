import {
  CalculatorIcon,
  CalendarCheck2Icon,
  CalendarDaysIcon,
  FilePlus2Icon,
  GiftIcon,
  InfoIcon,
  SparklesIcon,
  SproutIcon,
  StarIcon,
  TrophyIcon,
  UploadIcon,
} from "lucide-react"
import type { ReactNode } from "react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Separator } from "~/components/ui/separator"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import {
  type AttendanceOverview,
  useAttendanceOverview,
} from "~/features/economy/api/attendance.queries"
import type { EconomyOverview } from "~/features/economy/api/economy.queries"
import { fromSeedingRewardPolicyUnit } from "~/features/economy/model/seeding-reward-policy-units"
import { formatBytes } from "~/shared/formatters/bytes"
import {
  formatCompactDateTime,
  formatDateTime,
} from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

type AttendanceSettings = NonNullable<AttendanceOverview["policy"]>["settings"]

export function EconomyRulesOverview({
  overview,
  userId,
}: {
  overview: EconomyOverview
  userId: string
}) {
  const attendance = useAttendanceOverview(userId)

  return (
    <>
      <div className="grid items-start gap-4 lg:grid-cols-2">
        <MagicAcquisitionCard
          overview={overview}
          attendance={attendance.data}
          attendancePending={attendance.isPending}
        />
        <SeedingFormulaCard overview={overview} />
      </div>
      <ExperienceAcquisitionCard
        overview={overview}
        attendanceExperience={
          attendance.data?.policy?.settings.experience_reward
        }
      />
      <LevelRulesCard overview={overview} />
    </>
  )
}

function MagicAcquisitionCard({
  overview,
  attendance,
  attendancePending,
}: {
  overview: EconomyOverview
  attendance: AttendanceOverview | undefined
  attendancePending: boolean
}) {
  const policy = overview.rules.seeding_reward
  const attendanceRule = attendance?.policy?.settings

  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle className="flex items-center gap-2">
          <SparklesIcon />
          魔力值获取方式
        </CardTitle>
        <CardDescription>
          这里只展示当前实际生效的规则，历史收支仍以账本为准。
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-0 px-6 pb-6">
        <RuleRow
          icon={<SproutIcon />}
          title="做种奖励"
          description="Tracker 有效做种按关闭的整点窗口自动结算。"
          value={
            policy
              ? `最高 ${formatInteger(policy.maximum_hourly_reward)} / 小时`
              : "当前未启用"
          }
        />
        <Separator />
        <RuleRow
          icon={<CalendarCheck2Icon />}
          title="每日签到"
          description={attendanceDescription(attendanceRule, attendancePending)}
          value={
            attendanceRule?.enabled
              ? `${attendanceRewardLabel(attendanceRule)} 魔力值`
              : attendancePending
                ? "正在读取"
                : "当前未启用"
          }
        />
        <Separator />
        <RuleRow
          icon={<TrophyIcon />}
          title="活动与考核"
          description="活动、考核及工作组任务按各自生效规则记入统一账本。"
          value="按任务规则"
        />
        <Separator />
        <RuleRow
          icon={<GiftIcon />}
          title="成员赠送与内容打赏"
          description="到账金额受当前手续费与每日限额约束，不会额外增发魔力值。"
          value="成员间转账"
        />
      </CardContent>
    </Card>
  )
}

function RuleRow({
  icon,
  title,
  description,
  value,
}: {
  icon: ReactNode
  title: string
  description: string
  value: string
}) {
  return (
    <div className="flex gap-3 py-4 first:pt-0 last:pb-0">
      <div className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground [&>svg]:size-4">
        {icon}
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-sm font-medium">{title}</p>
          <span className="text-sm font-medium tabular-nums">{value}</span>
        </div>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
    </div>
  )
}

function SeedingFormulaCard({ overview }: { overview: EconomyOverview }) {
  const policy = overview.rules.seeding_reward
  const currentLevel = overview.rules.levels.find(
    (item) => item.level === overview.progress.level
  )

  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle className="flex items-center gap-2">
          <CalculatorIcon />
          做种奖励公式
        </CardTitle>
        <CardDescription>
          每个关闭的 UTC 整点窗口只计算一次，结果写入不可变账本。
        </CardDescription>
        {policy ? (
          <CardAction className="flex items-center gap-2">
            <Badge variant="outline">当前规则</Badge>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className="flex flex-col gap-5 px-6 pb-6">
        {!policy ? (
          <Alert>
            <InfoIcon />
            <AlertTitle>做种奖励当前未启用</AlertTitle>
            <AlertDescription>
              后台签发生效政策后，这里会自动显示公式和参数。
            </AlertDescription>
          </Alert>
        ) : (
          <>
            <div className="flex flex-col gap-2 rounded-lg border bg-muted/30 p-4 text-sm">
              <FormulaLine>
                单种价值 A = 时间因子 × 体积 × 稀缺度 × 在线比例 × 资源加成
              </FormulaLine>
              <FormulaLine>
                价值奖励 = 曲线上限 × (2 / π) × arctan(ΣA / 曲线尺度)
              </FormulaLine>
              <FormulaLine>
                数量奖励 = 单种固定奖励 × min(有效做种量，等级计数上限)
              </FormulaLine>
              <Separator />
              <FormulaLine strong>
                最终到账 = min(每小时上限，基础奖励 + VIP + 勋章 + 等级加成)
              </FormulaLine>
            </div>

            <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
              <Parameter
                label="价值奖励曲线上限"
                value={`${formatDisplayNumber(fromSeedingRewardPolicyUnit(policy.curve_hourly_cap_milli, "milli"))} / 小时`}
              />
              <Parameter
                label="每种固定奖励"
                value={`${formatDisplayNumber(fromSeedingRewardPolicyUnit(policy.per_torrent_hourly_milli, "milli"))} / 小时`}
              />
              <Parameter
                label="体积系数"
                value={`×${formatDisplayNumber(fromSeedingRewardPolicyUnit(policy.size_multiplier_bps, "ratio"))} / GB`}
              />
              <Parameter
                label="时间饱和系数"
                value={`${formatDisplayNumber(fromSeedingRewardPolicyUnit(policy.age_saturation_seconds, "weeks"))} 周`}
              />
              <Parameter
                label="做种人数惩罚系数"
                value={formatInteger(policy.seeder_decay)}
              />
              <Parameter
                label="曲线平滑参数"
                value={formatDisplayNumber(
                  fromSeedingRewardPolicyUnit(policy.curve_scale_milli, "milli")
                )}
              />
              <Parameter
                label="当前等级计数上限"
                value={`${policy.base_linear_torrent_limit + (currentLevel?.seeding_count_bonus ?? 0)} 个`}
              />
              <Parameter
                label="最小有效种子"
                value={formatBytes(policy.minimum_torrent_bytes)}
              />
              <Parameter
                label="最少活跃时长"
                value={`${formatDisplayNumber(fromSeedingRewardPolicyUnit(policy.minimum_active_seconds, "minutes"))} 分钟 / 小时`}
              />
              <Parameter
                label="每小时奖励上限"
                value={`${formatInteger(policy.maximum_hourly_reward)} 魔力值`}
              />
            </div>

            <div className="flex flex-wrap gap-2">
              <Badge variant="secondary">
                官种 +{formatPercent(policy.official_bonus_bps)}
              </Badge>
              <Badge variant="secondary">
                有上传贡献 +
                {formatPercent(policy.upload_contribution_bonus_bps)}
              </Badge>
              <Badge variant="secondary">
                VIP +{formatPercent(policy.vip_bonus_bps)}
              </Badge>
              <Badge variant="outline">
                勋章最高 +{formatPercent(policy.maximum_medal_bonus_bps)}
              </Badge>
              <Badge variant="outline">
                等级最高 +{formatPercent(policy.maximum_level_bonus_bps)}
              </Badge>
            </div>

            <p className="text-xs leading-relaxed text-muted-foreground">
              官种与上传贡献按同一单种基础独立相加；VIP、勋章与等级加成按基础奖励分别计算，避免加成顺序改变结果。政策自
              {formatDateTime(policy.effective_from)} 生效。
            </p>

            <LatestCalculation overview={overview} />
          </>
        )}
      </CardContent>
    </Card>
  )
}

function FormulaLine({
  children,
  strong = false,
}: {
  children: ReactNode
  strong?: boolean
}) {
  return (
    <p className={strong ? "font-medium text-foreground" : undefined}>
      {children}
    </p>
  )
}

function Parameter({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-4 border-b pb-2 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right font-medium tabular-nums">{value}</span>
    </div>
  )
}

function LatestCalculation({ overview }: { overview: EconomyOverview }) {
  const calculation = overview.latest_seeding_reward

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-sm font-medium">最近一次做种结算</p>
          <p className="text-xs text-muted-foreground">
            {calculation
              ? `${formatCompactDateTime(calculation.window_start)}–${formatCompactDateTime(calculation.window_end)}`
              : "暂无已经完成的整点结算窗口"}
          </p>
        </div>
        {calculation?.capped ? (
          <Badge variant="secondary">已触发每小时上限</Badge>
        ) : null}
      </div>
      {calculation ? (
        <div className="overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>有效种子</TableHead>
                <TableHead className="text-right">价值奖励</TableHead>
                <TableHead className="text-right">数量奖励</TableHead>
                <TableHead className="text-right">各项加成</TableHead>
                <TableHead className="text-right">到账</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow>
                <TableCell>{calculation.eligible_torrent_count}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatMilliMagic(calculation.curve_reward_milli)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatMilliMagic(calculation.linear_reward_milli)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatMilliMagic(
                    sumExactIntegers(
                      calculation.vip_bonus_milli,
                      calculation.medal_bonus_milli,
                      calculation.level_bonus_milli
                    )
                  )}
                </TableCell>
                <TableCell className="text-right font-medium tabular-nums">
                  {formatInteger(calculation.reward)}
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      ) : null}
    </div>
  )
}

function ExperienceAcquisitionCard({
  overview,
  attendanceExperience,
}: {
  overview: EconomyOverview
  attendanceExperience?: string
}) {
  const policy = overview.rules.seeding_reward
  const contribution = overview.rules.contribution_experience
  const items = [
    {
      label: "做种奖励",
      value: policy
        ? `每获得 1 魔力值 × ${formatDisplayNumber(fromSeedingRewardPolicyUnit(policy.experience_per_magic_bps, "ratio"))}`
        : "当前未启用",
      icon: <SproutIcon />,
    },
    {
      label: "实际上传",
      value: `每 1 GiB +${formatMilliExperience(contribution.experience_per_upload_gib_milli)}`,
      icon: <UploadIcon />,
    },
    {
      label: "发布种子",
      value: `每个 +${formatMilliExperience(contribution.experience_per_torrent_milli)}`,
      icon: <FilePlus2Icon />,
    },
    {
      label: "每日签到",
      value: attendanceExperience
        ? `每次 +${formatInteger(attendanceExperience)}`
        : "按签到政策",
      icon: <CalendarCheck2Icon />,
    },
    {
      label: "账号存续",
      value: `每天 +${formatMilliExperience(contribution.experience_per_account_day_milli)}`,
      icon: <CalendarDaysIcon />,
    },
  ]

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <StarIcon />
          经验获取方式
        </CardTitle>
        <CardDescription>
          当前有效政策与 Rousi
          的五项基础来源一致；经验只用于等级计算，不可赠送、消费或兑换。
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
        {items.map((item) => (
          <div
            key={item.label}
            className="flex flex-col gap-1 rounded-lg border bg-muted/20 p-4"
          >
            <span className="flex items-center gap-2 text-sm text-muted-foreground [&>svg]:size-4">
              {item.icon}
              {item.label}
            </span>
            <span className="font-medium tabular-nums">{item.value}</span>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function LevelRulesCard({ overview }: { overview: EconomyOverview }) {
  const policy = overview.rules.seeding_reward

  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle className="flex items-center gap-2">
          <TrophyIcon />
          等级一览
        </CardTitle>
        <CardDescription>
          等级与经验门槛按当前生效规则计算；经验达到门槛后自动晋级。
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        <Table containerClassName="px-3">
          <TableHeader>
            <TableRow>
              <TableHead>等级</TableHead>
              <TableHead className="text-right">所需经验</TableHead>
              <TableHead className="text-right">魔力值加成</TableHead>
              <TableHead className="text-right">做种计数上限</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {overview.rules.levels.map((level) => {
              const current = level.level === overview.progress.level
              return (
                <TableRow
                  key={level.level}
                  data-state={current ? "selected" : undefined}
                  className="h-[52px]"
                >
                  <TableCell className="font-medium">
                    <div className="flex items-center gap-2">
                      Lv.{level.level}
                      {current ? <Badge variant="secondary">当前</Badge> : null}
                    </div>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatExperience(level.minimum_experience)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {level.karma_bonus_bps > 0
                      ? `+${formatPercent(level.karma_bonus_bps)}`
                      : "—"}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {policy
                      ? `${policy.base_linear_torrent_limit + level.seeding_count_bonus} 个`
                      : "—"}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
        <div className="flex items-start gap-2 border-t px-6 py-4 text-xs text-muted-foreground">
          <InfoIcon className="mt-0.5 size-3.5 shrink-0" />
          <p>
            魔力值加成作用于每小时做种基础奖励；做种计数上限只限制“数量奖励”，价值奖励仍会计算全部符合条件的种子。
          </p>
        </div>
      </CardContent>
    </Card>
  )
}

function attendanceDescription(
  settings: AttendanceSettings | undefined,
  pending: boolean
) {
  if (pending) return "正在读取当前签到政策。"
  if (!settings?.enabled) return "签到活动当前未开放。"
  return settings.streak_enabled && settings.streak_milestones.length > 0
    ? `连续签到里程碑另有加奖，并获得 ${formatInteger(settings.experience_reward)} 经验。`
    : `每次签到同时获得 ${formatInteger(settings.experience_reward)} 经验。`
}

function attendanceRewardLabel(settings: AttendanceSettings) {
  const rewards: string[] = []
  if (settings.fixed_enabled) rewards.push(formatInteger(settings.fixed_reward))
  if (settings.random_enabled) {
    rewards.push(
      `${formatInteger(settings.random_min)}–${formatInteger(settings.random_max)}`
    )
  }
  return rewards.join(" 或 ") || "0"
}

function formatPercent(basisPoints: number) {
  return `${formatDisplayNumber(fromSeedingRewardPolicyUnit(basisPoints, "percent"))}%`
}

function formatMilliExperience(value: number) {
  return new Intl.NumberFormat("zh-CN", {
    maximumFractionDigits: 3,
  }).format(value / 1_000)
}

function formatDisplayNumber(value: number) {
  return new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 6 }).format(
    value
  )
}

function formatMilliMagic(value: string) {
  const milli = BigInt(value)
  const whole = milli / 1_000n
  const fraction = (milli % 1_000n)
    .toString()
    .padStart(3, "0")
    .replace(/0+$/, "")
  return fraction ? `${formatInteger(whole)}.${fraction}` : formatInteger(whole)
}

function sumExactIntegers(...values: string[]) {
  return values.reduce((sum, value) => sum + BigInt(value), 0n).toString()
}

function formatExperience(value: string) {
  const [integer, fraction = ""] = value.split(".")
  const visibleFraction = fraction.slice(0, 2).replace(/0+$/, "")
  return visibleFraction
    ? `${formatInteger(integer)}.${visibleFraction}`
    : formatInteger(integer)
}
