import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  CrownIcon,
  GiftIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  UserRoundIcon,
  UsersRoundIcon,
} from "lucide-react"
import { Link } from "react-router"

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
import { Skeleton } from "~/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { vipProfileSettingsQueryOptions } from "~/features/staff/api/operations.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffSettingsValueTable } from "~/features/staff/components/staff-settings-value-table"
import { fromSeedingRewardPolicyUnit } from "~/features/economy/model/seeding-reward-policy-units"
import { MetricCard } from "~/shared/components/metric-card"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatInteger } from "~/shared/formatters/integer"

export function StaffVIPProfileSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="operations.monitor.read"
      pageHeader={{
        title: "VIP 与用户资料",
        description: "查看 VIP 生效人数、实际权益和用户资料限制。",
      }}
    >
      {() => <VIPProfileSettingsContent />}
    </StaffAccessGate>
  )
}

function VIPProfileSettingsContent() {
  const settings = useQuery(vipProfileSettingsQueryOptions())
  if (settings.isPending) return <VIPProfileSettingsSkeleton />
  if (settings.isError || !settings.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>VIP 与用户资料设置暂时无法读取</AlertTitle>
          <AlertDescription>
            请检查 Core 数据库与后台会话后重试。
          </AlertDescription>
        </Alert>
        <Button
          variant="outline"
          className="w-fit"
          onClick={() => void settings.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </StaffPageFrame>
    )
  }

  const { stats, profile, benefits } = settings.data
  const vipBonus = fromSeedingRewardPolicyUnit(
    benefits.seeding_reward_bonus_bps,
    "percent"
  )
  const benefitRows = [
    {
      label: "做种奖励加成",
      enabled: benefits.seeding_reward_policy_revision !== "",
      value:
        benefits.seeding_reward_policy_revision === ""
          ? "尚无已生效策略"
          : `+${vipBonus.toLocaleString("zh-CN", { maximumFractionDigits: 2 })}%`,
    },
    {
      label: "VIP 免费下载",
      enabled: benefits.free_download_enabled,
      value: benefits.free_download_enabled
        ? "按 announce 时刻进入结算时间线，原始下载量仍保留"
        : "当前未接入结算规则",
    },
    {
      label: "分享率豁免",
      enabled: benefits.share_ratio_exempt,
      value: benefits.share_ratio_exempt
        ? "分享率观察与下载限制均豁免"
        : "当前分享率策略未启用豁免",
    },
    {
      label: "新人考核豁免",
      enabled: benefits.newcomer_assessment_exempt,
      value: benefits.newcomer_assessment_exempt
        ? "注册时跳过；签发 VIP 时自动豁免进行中的考核"
        : "当前未接入考核规则",
    },
    {
      label: "速度限制豁免",
      enabled: benefits.speed_limit_exempt,
      value: benefits.speed_limit_exempt
        ? "超速观察按 VIP 历史时段自动豁免"
        : "当前未接入 Tracker 控制规则",
    },
    {
      label: "VIP 兼容盒子政策",
      enabled: !benefits.seedbox_no_discount,
      value: benefits.seedbox_no_discount
        ? "VIP 盒子免除流量折算"
        : "VIP 免费下载等权益照常生效，随后应用盒子上传/下载倍率",
    },
  ]

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">VIP 与用户资料</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            这里显示服务端真实执行的规则；没有接入 Core
            的权益不会显示为可用开关。
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={settings.isFetching}
          onClick={() => void settings.refetch()}
        >
          <RefreshCwIcon
            data-icon="inline-start"
            className={settings.isFetching ? "animate-spin" : undefined}
          />
          刷新
        </Button>
      </header>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          title="总用户"
          value={formatInteger(stats.total_users)}
          description="PeerGo 用户目录"
          icon={<UsersRoundIcon />}
          tone="primary"
        />
        <MetricCard
          title="有效 VIP"
          value={formatInteger(stats.active_vip)}
          description="永久与限期 VIP 合计"
          icon={<CrownIcon />}
          tone="positive"
        />
        <MetricCard
          title="永久 VIP"
          value={formatInteger(stats.permanent_vip)}
          description="没有到期时间"
          icon={<ShieldCheckIcon />}
          tone="default"
        />
        <MetricCard
          title="限期 VIP"
          value={formatInteger(stats.expiring_vip)}
          description={`已过期 ${formatInteger(stats.expired_vip)}`}
          icon={<GiftIcon />}
          tone="muted"
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card className="gap-0 py-0">
          <CardHeader className="p-6 pb-4">
            <CardTitle>VIP 权益</CardTitle>
            <CardDescription>
              每项都对应 Core 已执行的结算或限制逻辑
            </CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>权益</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className="text-right">当前规则</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {benefitRows.map((row) => (
                  <TableRow key={row.label}>
                    <TableCell className="font-medium">{row.label}</TableCell>
                    <TableCell>
                      <Badge variant={row.enabled ? "secondary" : "outline"}>
                        {row.enabled ? "已生效" : "未接入"}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground">
                      {row.value}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <Card className="gap-0 py-0">
          <CardHeader className="p-6 pb-4">
            <CardTitle>用户资料规则</CardTitle>
            <CardDescription>本人资料接口当前执行的限制</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <StaffSettingsValueTable
              rows={[
                {
                  label: "昵称长度",
                  value: `${profile.display_name_min_characters}–${profile.display_name_max_characters} 个字符`,
                },
                {
                  label: "头像格式",
                  value: profile.avatar_format.toUpperCase(),
                },
                {
                  label: "头像尺寸",
                  value: `${profile.avatar_min_pixels}–${profile.avatar_max_pixels} px 正方形`,
                },
                {
                  label: "头像文件上限",
                  value: formatBytes(profile.avatar_max_bytes),
                },
              ]}
            />
          </CardContent>
        </Card>
      </div>

      <Alert>
        <UserRoundIcon />
        <AlertTitle>VIP 身份沿用用户访问状态</AlertTitle>
        <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
          <span>
            Rousi 迁移的永久和限期 VIP 已保留；过期记录不会计入当前有效人数。
          </span>
          <Button
            nativeButton={false}
            render={<Link to="/staff/users?filter=vip" />}
            variant="outline"
            size="sm"
          >
            <UsersRoundIcon data-icon="inline-start" />
            查看 VIP 用户
          </Button>
        </AlertDescription>
      </Alert>
    </StaffPageFrame>
  )
}

function VIPProfileSettingsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载 VIP 与用户资料设置">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <Skeleton className="h-80 rounded-lg" />
        <Skeleton className="h-80 rounded-lg" />
      </div>
    </StaffPageFrame>
  )
}
