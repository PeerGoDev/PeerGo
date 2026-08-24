import { z } from "zod"

import type { SiteDisplaySettings } from "~/features/staff/api/site-display-settings.queries"

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
  defaultTorrentView: z.enum(["list", "poster"]),
  showLatestAnnouncement: z.boolean(),
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
  return changes
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
