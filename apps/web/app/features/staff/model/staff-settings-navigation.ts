import type { LucideIcon } from "lucide-react"
import {
  BadgePercentIcon,
  CalendarCheck2Icon,
  ChartNoAxesColumnIncreasingIcon,
  CoinsIcon,
  CrownIcon,
  HardDriveIcon,
  ImagesIcon,
  MailIcon,
  MedalIcon,
  RouterIcon,
  RssIcon,
  ServerCogIcon,
  Settings2Icon,
  ShieldCheckIcon,
  UserRoundIcon,
  WalletCardsIcon,
} from "lucide-react"

import type { components } from "~/generated/api"

type CapabilityAction = components["schemas"]["CapabilityAction"]

export type StaffSettingsNavigationItem = {
  label: string
  description: string
  to: string
  icon: LucideIcon
  action: CapabilityAction
}

export type StaffSettingsNavigationGroup = {
  id: "site" | "torrent" | "economy"
  label: string
  description: string
  icon: LucideIcon
  items: StaffSettingsNavigationItem[]
}

export const staffSettingsNavigationGroups: StaffSettingsNavigationGroup[] = [
  {
    id: "site",
    label: "站点与用户",
    description: "管理站点展示、注册流程、外发邮件和用户资料。",
    icon: Settings2Icon,
    items: [
      {
        label: "基础设置",
        description: "站点名称、首页展示、下载前缀和自定义菜单。",
        to: "/staff/settings/site",
        icon: Settings2Icon,
        action: "site.display.manage.read",
      },
      {
        label: "注册与认证",
        description: "注册模式、账号规则、邀请与人机验证。",
        to: "/staff/settings/registration",
        icon: UserRoundIcon,
        action: "site.registration.manage.read",
      },
      {
        label: "邮件设置",
        description: "SMTP、发件身份、模板连通性与测试邮件。",
        to: "/staff/settings/email",
        icon: MailIcon,
        action: "operations.monitor.read",
      },
      {
        label: "图片与存储",
        description: "上传图片、附件存储和容量边界。",
        to: "/staff/settings/storage",
        icon: ImagesIcon,
        action: "operations.monitor.read",
      },
      {
        label: "VIP 与用户资料",
        description: "VIP 展示、用户资料字段和公开信息策略。",
        to: "/staff/settings/vip-profile",
        icon: CrownIcon,
        action: "operations.monitor.read",
      },
      {
        label: "RSS 设置",
        description: "订阅访问、筛选能力和 RSS 安全策略。",
        to: "/staff/settings/rss",
        icon: RssIcon,
        action: "rss.settings.manage.read",
      },
    ],
  },
  {
    id: "torrent",
    label: "种子与 Tracker",
    description: "集中管理发布、优惠、Tracker、盒子与考核规则。",
    icon: HardDriveIcon,
    items: [
      {
        label: "种子规则",
        description: "发布约束、文件检查和审核相关策略。",
        to: "/staff/settings/torrents",
        icon: HardDriveIcon,
        action: "torrent.manage.read",
      },
      {
        label: "优惠规则",
        description: "站点优惠模板、计划和自动结束策略。",
        to: "/staff/settings/promotions",
        icon: BadgePercentIcon,
        action: "promotion.manage.read",
      },
      {
        label: "Tracker 参数",
        description: "汇报间隔、连接策略和运行时限制。",
        to: "/staff/settings/tracker",
        icon: RouterIcon,
        action: "tracker.policy.read",
      },
      {
        label: "盒子设置",
        description: "可信网络、上传折算和盒子申报审核。",
        to: "/staff/settings/seedbox",
        icon: ServerCogIcon,
        action: "tracker.policy.read",
      },
      {
        label: "分享率与 H&R",
        description: "分享率考核、H&R 门槛和申诉策略。",
        to: "/staff/settings/ratio-hnr",
        icon: ShieldCheckIcon,
        action: "hnr.policy.read",
      },
    ],
  },
  {
    id: "economy",
    label: "等级与经济",
    description: "管理勋章、做种奖励、经验等级与魔力值去向。",
    icon: CoinsIcon,
    items: [
      {
        label: "勋章管理",
        description: "勋章目录、购买条件、展示和佩戴规则。",
        to: "/staff/settings/medals",
        icon: MedalIcon,
        action: "economy.medal.manage.read",
      },
      {
        label: "做种奖励",
        description: "做种时长、体积和奖励结算策略。",
        to: "/staff/settings/seeding-rewards",
        icon: CoinsIcon,
        action: "economy.seedingreward.policy.read",
      },
      {
        label: "经验与等级",
        description: "等级门槛、经验来源和成长进度。",
        to: "/staff/settings/progression/levels",
        icon: ChartNoAxesColumnIncreasingIcon,
        action: "progression.level.policy.read",
      },
      {
        label: "签到与活动奖励",
        description: "签到周期、补签和活动奖励规则。",
        to: "/staff/settings/activity-rewards",
        icon: CalendarCheck2Icon,
        action: "economy.attendance.policy.read",
      },
      {
        label: "魔力值使用规则",
        description: "魔力消费、赠送和内容激励的边界。",
        to: "/staff/settings/magic-usage",
        icon: WalletCardsIcon,
        action: "economy.seedingreward.policy.read",
      },
    ],
  },
]

export const staffSettingsNavigationItems =
  staffSettingsNavigationGroups.flatMap((group) => group.items)
