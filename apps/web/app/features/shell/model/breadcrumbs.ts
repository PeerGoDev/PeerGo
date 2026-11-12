export type BreadcrumbItem = {
  label: string
  to?: string
}

const home: BreadcrumbItem = { label: "首页", to: "/" }

const topLevelPages: Record<string, string> = {
  "/torrents": "种子",
  "/list": "种子",
  "/search": "搜索",
  "/upload": "上传",
  "/announcements": "公告",
  "/wiki": "Wiki",
  "/notifications": "消息",
  "/messages": "消息",
  "/social": "动态圈",
  "/workgroups": "工作组",
  "/medals": "勋章",
  "/review": "种子审核",
  "/restrictions": "封禁记录与申诉",
  "/account": "账户设置",
  "/account/invitations": "邀请",
}

const accountSettingsPages: Record<string, string> = {
  "/account/email": "邮箱验证",
  "/account/security": "安全设置",
  "/account/api-key": "API Key",
  "/account/permissions": "我的权限",
}

// Mirrors the collapsible sidebar sections so the trail names the same parent
// a member sees in the navigation.
const groupedPages: Record<string, [group: string, label: string]> = {
  "/account/traffic": ["账户与成长", "流量统计"],
  "/account/download-restriction": ["账户与成长", "下载限制"],
  "/account/ratio": ["账户与成长", "分享率考核"],
  "/account/assessment": ["账户与成长", "新人考核"],
  "/account/economy": ["账户与成长", "等级与魔力值"],
  "/account/hnr": ["账户与成长", "H&R"],
  "/account/rss": ["种子与订阅", "RSS"],
  "/account/seedbox": ["种子与订阅", "盒子申报"],
  "/account/bookmarks": ["种子与订阅", "我的收藏"],
  "/account/purchases": ["种子与订阅", "已购种子"],
  "/account/promotions": ["种子与订阅", "促销记录"],
  "/account/submissions": ["种子与订阅", "我的上传"],
}

const nestedPages: Array<{
  pattern: RegExp
  parent: BreadcrumbItem
  label: string
}> = [
  {
    pattern: /^\/torrents?\/[^/]+$/,
    parent: { label: "种子", to: "/torrents" },
    label: "种子详情",
  },
  {
    pattern: /^\/announcements\/[^/]+$/,
    parent: { label: "公告", to: "/announcements" },
    label: "公告详情",
  },
  {
    pattern: /^\/social\/notifications$/,
    parent: { label: "动态圈", to: "/social" },
    label: "互动通知",
  },
  {
    pattern: /^\/social\/post\/[^/]+$/,
    parent: { label: "动态圈", to: "/social" },
    label: "动态详情",
  },
  {
    pattern: /^\/social\/user\/[^/]+$/,
    parent: { label: "动态圈", to: "/social" },
    label: "成员动态",
  },
  {
    pattern: /^\/wiki\/[^/]+\/edit$/,
    parent: { label: "Wiki", to: "/wiki" },
    label: "编辑页面",
  },
  {
    pattern: /^\/wiki\/[^/]+$/,
    parent: { label: "Wiki", to: "/wiki" },
    label: "页面详情",
  },
  {
    pattern: /^\/review\/queue$/,
    parent: { label: "种子审核", to: "/review" },
    label: "审核队列",
  },
  {
    pattern: /^\/review\/torrent\/[^/]+$/,
    parent: { label: "种子审核", to: "/review" },
    label: "审核详情",
  },
]

/** Resolves the member-facing header trail from the current pathname. */
export function resolveAppBreadcrumbs(pathname: string): BreadcrumbItem[] {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, "") : pathname
  if (path === "/") return [{ label: home.label }]

  const topLevel = topLevelPages[path]
  if (topLevel) return [home, { label: topLevel }]

  const accountSetting = accountSettingsPages[path]
  if (accountSetting) {
    return [
      home,
      { label: "账户设置", to: "/account" },
      { label: accountSetting },
    ]
  }

  const grouped = groupedPages[path]
  if (grouped) return [home, { label: grouped[0] }, { label: grouped[1] }]

  if (/^\/user\/[^/]+$/.test(path)) return [home, { label: "成员资料" }]

  const nested = nestedPages.find((page) => page.pattern.test(path))
  if (nested) return [home, nested.parent, { label: nested.label }]

  return [home]
}
