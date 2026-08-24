import { z } from "zod"

import type {
  CustomNavigationItem,
  SiteDisplaySettings,
} from "~/features/staff/api/site-display-settings.queries"

const customNavigationItemSchema = z.object({
  label: z
    .string()
    .trim()
    .refine((value) => runeLength(value) >= 1, "请输入菜单名称")
    .refine((value) => runeLength(value) <= 32, "菜单名称不能超过 32 个字符"),
  url: z
    .string()
    .trim()
    .refine((value) => runeLength(value) >= 1, "请输入链接地址")
    .refine(
      (value) => runeLength(value) <= 2048,
      "链接地址不能超过 2048 个字符"
    )
    .refine(
      isCustomNavigationURL,
      "请输入站内路径（如 /wiki）或不含账号凭据的 HTTPS 地址"
    ),
  open_in_new_tab: z.boolean(),
  enabled: z.boolean(),
})

const customNavigationItemsSchema = z
  .array(customNavigationItemSchema)
  .max(12, "自定义菜单最多 12 项")
  .superRefine((items, context) => {
    const labels = new Set<string>()
    const urls = new Set<string>()
    items.forEach((item, index) => {
      const label = item.label.toLocaleLowerCase()
      if (labels.has(label)) {
        context.addIssue({
          code: "custom",
          path: [index, "label"],
          message: "菜单名称不能重复",
        })
      }
      if (urls.has(item.url)) {
        context.addIssue({
          code: "custom",
          path: [index, "url"],
          message: "链接地址不能重复",
        })
      }
      labels.add(label)
      urls.add(item.url)
    })
  })

export const siteDisplaySettingsFormSchema = z.object({
  name: z
    .string()
    .trim()
    .refine((value) => runeLength(value) >= 1, "请输入站点名称")
    .refine((value) => runeLength(value) <= 80, "站点名称不能超过 80 个字符"),
  description: z
    .string()
    .trim()
    .refine((value) => runeLength(value) <= 500, "站点说明不能超过 500 个字符"),
  torrentFilenamePrefix: z
    .string()
    .trim()
    .refine(
      (value) => runeLength(value) <= 40,
      "种子文件名前缀不能超过 40 个字符"
    )
    .refine(
      (value) => !/[\\/:*?"<>|\u0000-\u001f\u007f]/u.test(value),
      "种子文件名前缀不能包含路径或控制字符"
    )
    .refine(
      (value) => value === "" || value.replaceAll(".", "").length > 0,
      "种子文件名前缀不能只包含句点"
    ),
  defaultTorrentView: z.enum(["list", "poster"]),
  showLatestAnnouncement: z.boolean(),
  customNavigationItems: customNavigationItemsSchema,
  reason: z
    .string()
    .trim()
    .refine((value) => runeLength(value) <= 500, "变更理由不能超过 500 个字符"),
})

export type SiteDisplaySettingsFormValues = z.output<
  typeof siteDisplaySettingsFormSchema
>
export type SiteDisplaySettingsFormField = keyof z.input<
  typeof siteDisplaySettingsFormSchema
>

export type SiteDisplaySettingsBusinessValues = Omit<
  SiteDisplaySettingsFormValues,
  "reason"
>

export type SiteDisplaySettingsDiff = {
  field: string
  before: string
  after: string
}

export function hasSiteDisplaySettingsChanges(
  settings: SiteDisplaySettings,
  values: SiteDisplaySettingsBusinessValues
) {
  return siteDisplaySettingsDiff(settings, values).length > 0
}

export function siteDisplaySettingsDiff(
  settings: SiteDisplaySettings,
  values: SiteDisplaySettingsBusinessValues
): SiteDisplaySettingsDiff[] {
  const changes: SiteDisplaySettingsDiff[] = []
  if (settings.name !== values.name) {
    changes.push({
      field: "站点名称",
      before: settings.name,
      after: values.name,
    })
  }
  if (settings.description !== values.description) {
    changes.push({
      field: "站点说明",
      before: displayText(settings.description),
      after: displayText(values.description),
    })
  }
  if (settings.torrent_filename_prefix !== values.torrentFilenamePrefix) {
    changes.push({
      field: "种子文件名前缀",
      before: displayText(settings.torrent_filename_prefix),
      after: displayText(values.torrentFilenamePrefix),
    })
  }
  if (settings.default_torrent_view !== values.defaultTorrentView) {
    changes.push({
      field: "默认种子视图",
      before: torrentViewLabel(settings.default_torrent_view),
      after: torrentViewLabel(values.defaultTorrentView),
    })
  }
  if (settings.show_latest_announcement !== values.showLatestAnnouncement) {
    changes.push({
      field: "首页最新公告",
      before: settings.show_latest_announcement ? "显示" : "隐藏",
      after: values.showLatestAnnouncement ? "显示" : "隐藏",
    })
  }
  if (
    !customNavigationItemsEqual(
      settings.custom_navigation_items,
      values.customNavigationItems
    )
  ) {
    changes.push({
      field: "自定义左侧菜单",
      before: customNavigationItemsLabel(settings.custom_navigation_items),
      after: customNavigationItemsLabel(values.customNavigationItems),
    })
  }
  return changes
}

function customNavigationItemsEqual(
  first: CustomNavigationItem[],
  second: CustomNavigationItem[]
) {
  return (
    first.length === second.length &&
    first.every(
      (item, index) =>
        item.label === second[index]?.label &&
        item.url === second[index]?.url &&
        item.open_in_new_tab === second[index]?.open_in_new_tab &&
        item.enabled === second[index]?.enabled
    )
  )
}

function customNavigationItemsLabel(items: CustomNavigationItem[]) {
  if (items.length === 0) return "（无）"
  const enabled = items.filter((item) => item.enabled)
  if (enabled.length === 0) return `${items.length} 项（均停用）`
  return `${items.length} 项：${enabled.map((item) => item.label).join("、")}`
}

export function isCustomNavigationURL(value: string) {
  if (
    value.includes("\\") ||
    /[\u0000-\u001f\u007f]/u.test(value) ||
    value.startsWith("//")
  ) {
    return false
  }
  if (value.startsWith("/")) return true
  try {
    const parsed = new URL(value)
    return (
      parsed.protocol === "https:" &&
      Boolean(parsed.hostname) &&
      parsed.username === "" &&
      parsed.password === ""
    )
  } catch {
    return false
  }
}

function torrentViewLabel(view: "list" | "poster") {
  return view === "list" ? "列表" : "海报"
}

function displayText(value: string) {
  return value || "（空）"
}

function runeLength(value: string) {
  return Array.from(value).length
}
