import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  CircleDashedIcon,
  CircleHelpIcon,
  RefreshCwIcon,
  RocketIcon,
  TriangleAlertIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Progress } from "~/components/ui/progress"
import { managedCategoryListQueryOptions } from "~/features/staff/api/category-administration.queries"
import { newcomerPolicyListQueryOptions } from "~/features/staff/api/newcomer-administration.queries"
import {
  emailSettingsQueryOptions,
  settlementSettingsQueryOptions,
  storageOperationsQueryOptions,
  torrentSettingsQueryOptions,
  trackerSettingsQueryOptions,
  workerOperationsQueryOptions,
} from "~/features/staff/api/operations.queries"
import { registrationPolicySettingsQueryOptions } from "~/features/staff/api/registration-policy-settings.queries"
import { rssSettingsQueryOptions } from "~/features/staff/api/rss-settings.queries"
import {
  levelPolicyListQueryOptions,
  seedingRewardPolicyListQueryOptions,
} from "~/features/staff/api/seeding-reward-administration.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import { useSiteInfo } from "~/features/site/api/site.queries"
import type { components } from "~/generated/api"
import { cn } from "~/lib/utils"
import { isSecurePublicOrigin } from "~/shared/validation/public-origin"

type CapabilityList = components["schemas"]["CapabilityList"]
type CheckStatus = "ready" | "warning" | "blocked" | "loading" | "unavailable"
type SetupCheckGroup = "identity" | "content" | "tracker" | "growth"

type SetupCheck = {
  id: string
  group: SetupCheckGroup
  title: string
  description: string
  detail: string
  href: string
  status: CheckStatus
  required: boolean
}

const setupCheckGroups: Array<{
  id: SetupCheckGroup
  title: string
  description: string
}> = [
  {
    id: "identity",
    title: "基础与身份",
    description: "站点入口、注册安全、邮件和新人准入。",
  },
  {
    id: "content",
    title: "内容与存储",
    description: "种子上传、分类、RSS、对象存储和图片派生。",
  },
  {
    id: "tracker",
    title: "Tracker 与结算",
    description: "Tracker 热加载、H&R 规则和后台任务积压。",
  },
  {
    id: "growth",
    title: "成长与经济",
    description: "做种奖励、经验等级与用户长期成长规则。",
  },
]

type QueryState<T> = {
  data: T | undefined
  isPending: boolean
  isError: boolean
}

export function StaffSetupReadinessPage() {
  return (
    <StaffAccessGate
      pageHeader={{
        title: "上线检查",
        description: "逐项确认首次部署后真正生效的站点设置。",
      }}
    >
      {({ capabilities }) => (
        <SetupReadinessContent capabilities={capabilities} />
      )}
    </StaffAccessGate>
  )
}

function SetupReadinessContent({
  capabilities,
}: {
  capabilities: CapabilityList
}) {
  const canReadRegistration = hasCapability(
    capabilities,
    "site.registration.manage.read"
  )
  const canReadCategories = hasCapability(capabilities, "category.manage.read")
  const canReadNewcomer = hasCapability(capabilities, "newcomer.policy.read")
  const canReadOperations = hasCapability(
    capabilities,
    "operations.monitor.read"
  )
  const canReadTracker = hasCapability(capabilities, "tracker.policy.read")
  const canReadTorrents = hasCapability(capabilities, "torrent.manage.read")
  const canReadRSS = hasCapability(capabilities, "rss.settings.manage.read")
  const canReadSeeding = hasCapability(
    capabilities,
    "economy.seedingreward.policy.read"
  )
  const canReadLevels = hasCapability(
    capabilities,
    "progression.level.policy.read"
  )

  const site = useSiteInfo()
  const registration = useQuery({
    ...registrationPolicySettingsQueryOptions,
    enabled: canReadRegistration,
  })
  const categories = useQuery({
    ...managedCategoryListQueryOptions,
    enabled: canReadCategories,
  })
  const newcomer = useQuery({
    ...newcomerPolicyListQueryOptions(),
    enabled: canReadNewcomer,
  })
  const email = useQuery({
    ...emailSettingsQueryOptions(),
    enabled: canReadOperations,
  })
  const settlement = useQuery({
    ...settlementSettingsQueryOptions(),
    enabled: canReadOperations,
  })
  const storage = useQuery({
    ...storageOperationsQueryOptions(),
    enabled: canReadOperations,
  })
  const workers = useQuery({
    ...workerOperationsQueryOptions(),
    enabled: canReadOperations,
  })
  const torrentSettings = useQuery({
    ...torrentSettingsQueryOptions(),
    enabled: canReadTorrents,
  })
  const rss = useQuery({
    ...rssSettingsQueryOptions,
    enabled: canReadRSS,
  })
  const tracker = useQuery({
    ...trackerSettingsQueryOptions(),
    enabled: canReadTracker,
  })
  const seeding = useQuery({
    ...seedingRewardPolicyListQueryOptions(),
    enabled: canReadSeeding,
  })
  const levels = useQuery({
    ...levelPolicyListQueryOptions(),
    enabled: canReadLevels,
  })

  const checks: SetupCheck[] = [
    queryCheck(
      "site",
      "identity",
      "站点基本信息",
      "确认站点名称、说明和首页展示方式。",
      "/staff/settings/site",
      true,
      true,
      site,
      (data) => ({
        status: data.name.trim() ? "ready" : "blocked",
        detail: data.name.trim()
          ? `当前站点名称：${data.name}`
          : "站点名称尚未配置。",
      })
    ),
    queryCheck(
      "registration",
      "identity",
      "注册方式",
      "明确选择关闭、邀请制或开放注册。",
      "/staff/settings/registration",
      true,
      canReadRegistration,
      registration,
      (data) => ({
        status: data.mode === "closed" ? "warning" : "ready",
        detail:
          data.mode === "closed"
            ? "当前关闭注册；这是安全状态，但切流前需确认是否符合运营计划。"
            : `当前使用${data.mode === "invite" ? "邀请注册" : "开放注册"}。`,
      })
    ),
    queryCheck(
      "human-verification",
      "identity",
      "人机验证",
      "核对注册、登录和密码找回的自动化请求防护。",
      "/staff/settings/registration",
      false,
      canReadRegistration,
      registration,
      (data) => {
        if (data.human_verification_provider === "disabled") {
          return {
            status: "warning",
            detail:
              "当前未启用人机验证；小范围内测可接受，公开注册前应重新确认。",
          }
        }
        const protectedFlows = [
          data.human_verification_registration_enabled,
          data.human_verification_login_enabled,
          data.human_verification_password_recovery_enabled,
        ].filter(Boolean).length
        return {
          status:
            data.human_verification_secret_configured && protectedFlows > 0
              ? "ready"
              : "blocked",
          detail: data.human_verification_secret_configured
            ? `Turnstile 已保护 ${protectedFlows} 个身份入口。`
            : "已选择 Turnstile，但运行环境没有服务端 secret。",
        }
      }
    ),
    queryCheck(
      "categories",
      "content",
      "种子分类",
      "至少保留一个启用分类，上传和列表才能正常归类。",
      "/staff/content/categories",
      true,
      canReadCategories,
      categories,
      (data) => {
        const enabled = data.filter((item) => item.enabled).length
        return {
          status: enabled > 0 ? "ready" : "blocked",
          detail:
            enabled > 0
              ? `已有 ${enabled} 个启用分类。`
              : "没有启用的种子分类。",
        }
      }
    ),
    queryCheck(
      "email",
      "identity",
      "邮件投递",
      "核对验证邮件、密码找回和实际投递链路。",
      "/staff/settings/email",
      true,
      canReadOperations,
      email,
      (data) => {
        const secureOrigins =
          isSecurePublicOrigin(data.verification_public_origin) &&
          isSecurePublicOrigin(data.password_recovery_public_origin)
        if (data.delivery_mode !== "https_relay") {
          return {
            status: "warning",
            detail: "当前是本地开发信箱，不能用于正式站点发信。",
          }
        }
        return {
          status: secureOrigins ? "ready" : "blocked",
          detail: secureOrigins
            ? "生产 Relay 与 HTTPS 操作链接已接入；上线前仍应发送一封测试邮件。"
            : "Relay 已接入，但邮箱验证或密码找回链接仍不是公开 HTTPS 来源。",
        }
      }
    ),
    queryCheck(
      "newcomer",
      "identity",
      "新人考核",
      "确认新注册账户是否进入上传量与做种时长考核。",
      "/staff/settings/registration",
      true,
      canReadNewcomer,
      newcomer,
      (data) => ({
        status: !data.current
          ? "blocked"
          : data.current.enabled
            ? "ready"
            : "warning",
        detail: !data.current
          ? "没有当前生效的新人考核规则。"
          : data.current.enabled
            ? "新人考核已启用。"
            : "新人考核已明确关闭；切流前需确认这是有意设置。",
      })
    ),
    queryCheck(
      "torrent-upload",
      "content",
      "种子上传策略",
      "确认新上传使用私有种子、审核队列和图片安全限制。",
      "/staff/settings/torrents",
      true,
      canReadTorrents,
      torrentSettings,
      (data) => {
        const policy = data.active_upload_policy.settings
        if (
          !data.upload.required_private ||
          data.upload.initial_state !== "pending_review" ||
          policy.metainfo_max_bytes <= 0 ||
          policy.max_files <= 0 ||
          policy.screenshot_formats.length === 0
        ) {
          return {
            status: "blocked",
            detail: "新上传的私有属性、审核入口或文件安全限制不完整。",
          }
        }
        if (policy.screenshot_max_bytes > 2 * 1024 * 1024) {
          return {
            status: "warning",
            detail: `单张原图当前允许 ${formatMegabytes(policy.screenshot_max_bytes)}，高于建议的 2 MB。`,
          }
        }
        return {
          status: "ready",
          detail: `原图最多 ${formatMegabytes(policy.screenshot_max_bytes)}，每个种子最多 ${policy.screenshot_max_count} 张；新种子先进入审核。`,
        }
      }
    ),
    queryCheck(
      "storage",
      "content",
      "图片与种子存储",
      "确认对象后端、上传限制和图片派生队列可读取。",
      "/staff/settings/storage",
      true,
      canReadOperations,
      storage,
      (data) => {
        const failedMigrations = BigInt(data.inventory.failed_migration_items)
        const deadDerivatives = BigInt(data.image_derivatives.dead)
        if (failedMigrations > 0n || deadDerivatives > 0n) {
          return {
            status: "blocked",
            detail: `存在 ${failedMigrations} 个迁移失败项、${deadDerivatives} 个图片派生死信，需先处理。`,
          }
        }

        const activeMigrations = BigInt(data.inventory.active_migrations)
        const retryingDerivatives = BigInt(data.image_derivatives.retrying)
        if (activeMigrations > 0n || retryingDerivatives > 0n) {
          return {
            status: "warning",
            detail: `当前有 ${activeMigrations} 个存储迁移、${retryingDerivatives} 个图片派生任务正在重试。`,
          }
        }

        return {
          status: "ready",
          detail: `当前使用 ${data.runtime.driver === "filesystem" ? "本地文件存储" : "S3 兼容存储"}，迁移与图片派生无失败项。`,
        }
      }
    ),
    queryCheck(
      "rss",
      "content",
      "RSS 订阅",
      "确认订阅上限、请求频率与优惠边界缓存策略已经生效。",
      "/staff/settings/rss",
      false,
      canReadRSS,
      rss,
      (data) => ({
        status: data.enabled ? "ready" : "warning",
        detail: data.enabled
          ? `RSS 已启用：每分钟 ${data.requests_per_minute} 次、每份最多 ${data.max_items_per_feed} 项，缓存 ${data.cache_ttl_seconds} 秒。`
          : "RSS 已明确关闭；如需兼容老站订阅，请在切流前开启。",
      })
    ),
    queryCheck(
      "tracker",
      "tracker",
      "Tracker 参数",
      "确认已配置策略与 Tracker 实际加载版本一致。",
      "/staff/settings/tracker",
      true,
      canReadTracker,
      tracker,
      (data) => ({
        status: data.activation_pending ? "blocked" : "ready",
        detail: data.activation_pending
          ? "新策略仍在等待 Tracker 热加载。"
          : "Tracker 已加载当前策略。",
      })
    ),
    queryCheck(
      "hnr",
      "tracker",
      "分享率与 H&R",
      "确认结算侧存在明确的 H&R 时间线。",
      "/staff/settings/ratio-hnr",
      true,
      canReadOperations,
      settlement,
      (data) => ({
        status: !data.hnr.configured
          ? "blocked"
          : data.hnr.mode === "disabled"
            ? "warning"
            : "ready",
        detail: !data.hnr.configured
          ? "尚未配置 H&R 规则。"
          : data.hnr.mode === "disabled"
            ? "H&R 已明确关闭。"
            : "H&R 规则已配置并接入结算。",
      })
    ),
    queryCheck(
      "workers",
      "tracker",
      "后台任务",
      "检查奖励、优惠、Tracker 控制和审计投递队列。",
      "/staff/operations/workers",
      true,
      canReadOperations,
      workers,
      (data) => {
        if (data.queues.length === 0) {
          return {
            status: "blocked",
            detail: "没有读取到任何后台任务队列。",
          }
        }
        const dead = data.queues.reduce(
          (total, queue) => total + BigInt(queue.dead),
          0n
        )
        if (dead > 0n) {
          return {
            status: "blocked",
            detail: `存在 ${dead} 个停止自动重试的任务，需人工核对后处理。`,
          }
        }
        const retrying = data.queues.reduce(
          (total, queue) => total + BigInt(queue.retrying),
          0n
        )
        return {
          status: retrying > 0n ? "warning" : "ready",
          detail:
            retrying > 0n
              ? `当前有 ${retrying} 个失败任务正在自动重试。`
              : `${data.queues.length} 类后台任务均无死信或重试积压。`,
        }
      }
    ),
    queryCheck(
      "seeding",
      "growth",
      "做种奖励",
      "确认至少存在一版可审计的魔力值奖励公式。",
      "/staff/settings/seeding-rewards",
      false,
      canReadSeeding,
      seeding,
      (data) => ({
        status: BigInt(data.total) > 0n ? "ready" : "blocked",
        detail:
          BigInt(data.total) > 0n
            ? `已有 ${data.total} 版奖励规则。`
            : "尚未签发做种奖励规则。",
      })
    ),
    queryCheck(
      "levels",
      "growth",
      "经验与等级",
      "确认等级门槛和加成已经生效。",
      "/staff/settings/progression/levels",
      false,
      canReadLevels,
      levels,
      (data) => {
        const applied = data.items.some(
          (item) => item.activation_status === "applied"
        )
        return {
          status: applied ? "ready" : "blocked",
          detail: applied ? "经验等级规则已生效。" : "没有已生效的等级规则。",
        }
      }
    ),
  ]

  const visibleChecks = checks.filter((check) => check.status !== "unavailable")
  const readyCount = visibleChecks.filter(
    (check) => check.status === "ready"
  ).length
  const blockedCount = visibleChecks.filter(
    (check) => check.required && check.status === "blocked"
  ).length
  const pending = visibleChecks.some((check) => check.status === "loading")
  const progress = visibleChecks.length
    ? Math.round((readyCount / visibleChecks.length) * 100)
    : 0

  async function refreshAll() {
    await Promise.all([
      site.refetch(),
      ...(canReadRegistration ? [registration.refetch()] : []),
      ...(canReadCategories ? [categories.refetch()] : []),
      ...(canReadNewcomer ? [newcomer.refetch()] : []),
      ...(canReadOperations
        ? [
            email.refetch(),
            settlement.refetch(),
            storage.refetch(),
            workers.refetch(),
          ]
        : []),
      ...(canReadTracker ? [tracker.refetch()] : []),
      ...(canReadTorrents ? [torrentSettings.refetch()] : []),
      ...(canReadRSS ? [rss.refetch()] : []),
      ...(canReadSeeding ? [seeding.refetch()] : []),
      ...(canReadLevels ? [levels.refetch()] : []),
    ])
  }

  return (
    <StaffPageFrame className="gap-4">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <RocketIcon className="size-5 text-primary" />
            首次上线准备
          </CardTitle>
          <CardDescription>
            这里汇总后台真实接口状态，不会自动替你开启注册、考核或处罚规则。
          </CardDescription>
          <CardAction>
            <Button
              variant="outline"
              size="sm"
              disabled={pending}
              onClick={() => void refreshAll()}
            >
              <RefreshCwIcon
                data-icon="inline-start"
                className={pending ? "animate-spin" : undefined}
              />
              刷新
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex items-center justify-between gap-4 text-sm">
            <span>
              已确认 {readyCount} / {visibleChecks.length} 项
            </span>
            <Badge variant={blockedCount > 0 ? "destructive" : "outline"}>
              {blockedCount > 0 ? `${blockedCount} 项阻塞` : "无硬阻塞"}
            </Badge>
          </div>
          <Progress value={progress} aria-label="上线设置完成度" />
        </CardContent>
      </Card>

      {setupCheckGroups.map((group) => {
        const groupChecks = visibleChecks.filter(
          (check) => check.group === group.id
        )
        if (groupChecks.length === 0) return null
        return (
          <section key={group.id} className="space-y-3">
            <div>
              <h2 className="font-heading text-lg font-semibold">
                {group.title}
              </h2>
              <p className="text-sm text-muted-foreground">
                {group.description}
              </p>
            </div>
            <div className="grid gap-3 xl:grid-cols-2">
              {groupChecks.map((check) => (
                <SetupCheckCard key={check.id} check={check} />
              ))}
            </div>
          </section>
        )
      })}

      <Alert variant={blockedCount > 0 ? "destructive" : "default"}>
        {blockedCount > 0 ? <CircleAlertIcon /> : <CircleCheckIcon />}
        <AlertTitle>
          {blockedCount > 0 ? "仍有必需设置未就绪" : "页面检查未发现硬阻塞"}
        </AlertTitle>
        <AlertDescription>
          页面检查用于配置引导，正式切流前仍必须在部署主机运行 make
          production-activation-check，核对三库 migration、邮件 Relay、公开
          HTTPS 操作链接、结算优惠和 H&R 时间线。
        </AlertDescription>
      </Alert>
    </StaffPageFrame>
  )
}

function queryCheck<T>(
  id: string,
  group: SetupCheckGroup,
  title: string,
  description: string,
  href: string,
  required: boolean,
  permitted: boolean,
  query: QueryState<T>,
  evaluate: (data: T) => Pick<SetupCheck, "status" | "detail">
): SetupCheck {
  if (!permitted) {
    return {
      id,
      group,
      title,
      description,
      href,
      required,
      status: "unavailable",
      detail: "当前管理员无权读取此设置。",
    }
  }
  if (query.isError) {
    return {
      id,
      group,
      title,
      description,
      href,
      required,
      status: required ? "blocked" : "warning",
      detail: "真实设置接口暂时无法读取。",
    }
  }
  if (query.isPending || !query.data) {
    return {
      id,
      group,
      title,
      description,
      href,
      required,
      status: "loading",
      detail: "正在核对…",
    }
  }
  return {
    id,
    group,
    title,
    description,
    href,
    required,
    ...evaluate(query.data),
  }
}

function formatMegabytes(bytes: number) {
  const megabytes = bytes / (1024 * 1024)
  return `${Number.isInteger(megabytes) ? megabytes : megabytes.toFixed(1)} MB`
}

const statusPresentation: Record<
  CheckStatus,
  { label: string; icon: typeof CircleCheckIcon; className: string }
> = {
  ready: {
    label: "已就绪",
    icon: CircleCheckIcon,
    className: "text-success",
  },
  warning: {
    label: "待确认",
    icon: TriangleAlertIcon,
    className: "text-warning-foreground",
  },
  blocked: {
    label: "未就绪",
    icon: CircleAlertIcon,
    className: "text-destructive",
  },
  loading: {
    label: "核对中",
    icon: CircleDashedIcon,
    className: "animate-spin text-muted-foreground",
  },
  unavailable: {
    label: "不可查看",
    icon: CircleHelpIcon,
    className: "text-muted-foreground",
  },
}

function SetupCheckCard({ check }: { check: SetupCheck }) {
  const presentation = statusPresentation[check.status]
  const Icon = presentation.icon
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Icon className={cn("size-4", presentation.className)} />
          {check.title}
        </CardTitle>
        <CardDescription>{check.description}</CardDescription>
        <CardAction>
          <Badge
            variant={check.status === "blocked" ? "destructive" : "outline"}
          >
            {presentation.label}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="text-sm text-muted-foreground">
        {check.detail}
      </CardContent>
      <CardFooter>
        <Link
          to={check.href}
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          打开设置
        </Link>
      </CardFooter>
    </Card>
  )
}
