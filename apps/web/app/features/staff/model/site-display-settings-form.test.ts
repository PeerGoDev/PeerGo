import { describe, expect, it } from "vitest"

import {
  hasSiteDisplaySettingsChanges,
  siteDisplaySettingsDiff,
  siteDisplaySettingsFormSchema,
} from "~/features/staff/model/site-display-settings-form"
import type { SiteDisplaySettings } from "~/features/staff/api/site-display-settings.queries"

const settings: SiteDisplaySettings = {
  name: "PeerGo",
  description: "长期协作的私有分享社区。",
  torrent_filename_prefix: "[ROUSI]",
  default_torrent_view: "list",
  show_latest_announcement: true,
  custom_navigation_items: [],
  version: 3,
  effective_at: "2026-08-06T08:00:00Z",
  updated_at: "2026-08-06T08:00:00Z",
}

describe("siteDisplaySettingsFormSchema", () => {
  it("normalizes the typed settings section before review", () => {
    const result = siteDisplaySettingsFormSchema.parse({
      name: " PeerGo ",
      description: " 长期协作的私有分享社区。 ",
      torrentFilenamePrefix: " [ROUSI] ",
      defaultTorrentView: "list",
      showLatestAnnouncement: true,
      customNavigationItems: [],
      reason: " 核对站点公开展示文案并保持当前行为。 ",
    })

    expect(result).toEqual({
      name: "PeerGo",
      description: "长期协作的私有分享社区。",
      torrentFilenamePrefix: "[ROUSI]",
      defaultTorrentView: "list",
      showLatestAnnouncement: true,
      customNavigationItems: [],
      reason: "核对站点公开展示文案并保持当前行为。",
    })
    expect(hasSiteDisplaySettingsChanges(settings, result)).toBe(false)
  })

  it("returns field-level differences without treating the reason as state", () => {
    const changes = siteDisplaySettingsDiff(settings, {
      name: "PeerGo Club",
      description: settings.description,
      torrentFilenamePrefix: "[ROUSI-NEXT]",
      defaultTorrentView: "poster",
      showLatestAnnouncement: false,
      customNavigationItems: [],
    })

    expect(changes.map((change) => change.field)).toEqual([
      "站点名称",
      "种子文件名前缀",
      "默认种子视图",
      "首页最新公告",
    ])
  })

  it.each([
    ["empty name", { name: " " }],
    ["unknown view", { defaultTorrentView: "grid" }],
  ])("rejects %s before opening review", (_name, override) => {
    const result = siteDisplaySettingsFormSchema.safeParse({
      name: settings.name,
      description: settings.description,
      torrentFilenamePrefix: settings.torrent_filename_prefix,
      defaultTorrentView: settings.default_torrent_view,
      showLatestAnnouncement: settings.show_latest_announcement,
      customNavigationItems: settings.custom_navigation_items,
      reason: "核对站点公开展示文案并保持当前行为。",
      ...override,
    })

    expect(result.success).toBe(false)
  })

  it("accepts a blank audit reason", () => {
    expect(
      siteDisplaySettingsFormSchema.safeParse({
        name: settings.name,
        description: settings.description,
        torrentFilenamePrefix: settings.torrent_filename_prefix,
        defaultTorrentView: settings.default_torrent_view,
        showLatestAnnouncement: settings.show_latest_announcement,
        customNavigationItems: settings.custom_navigation_items,
        reason: "",
      }).success
    ).toBe(true)
  })

  it("rejects path-like filename prefixes", () => {
    expect(
      siteDisplaySettingsFormSchema.safeParse({
        name: settings.name,
        description: settings.description,
        torrentFilenamePrefix: "[ROU/SI]",
        defaultTorrentView: settings.default_torrent_view,
        showLatestAnnouncement: settings.show_latest_announcement,
        customNavigationItems: settings.custom_navigation_items,
        reason: "",
      }).success
    ).toBe(false)
  })

  it("accepts bounded internal and HTTPS custom navigation links", () => {
    const result = siteDisplaySettingsFormSchema.parse({
      name: settings.name,
      description: settings.description,
      torrentFilenamePrefix: settings.torrent_filename_prefix,
      defaultTorrentView: settings.default_torrent_view,
      showLatestAnnouncement: settings.show_latest_announcement,
      customNavigationItems: [
        {
          label: " Wiki ",
          url: " https://wiki.example.com ",
          open_in_new_tab: true,
          enabled: true,
        },
        {
          label: "站内帮助",
          url: "/help",
          open_in_new_tab: false,
          enabled: false,
        },
      ],
      reason: "",
    })

    expect(result.customNavigationItems).toEqual([
      {
        label: "Wiki",
        url: "https://wiki.example.com",
        open_in_new_tab: true,
        enabled: true,
      },
      {
        label: "站内帮助",
        url: "/help",
        open_in_new_tab: false,
        enabled: false,
      },
    ])
  })

  it.each([
    "http://wiki.example.com",
    "https://user:secret@wiki.example.com",
    "//wiki.example.com",
    "javascript:alert(1)",
  ])("rejects unsafe custom navigation URL %s", (url) => {
    expect(
      siteDisplaySettingsFormSchema.safeParse({
        name: settings.name,
        description: settings.description,
        torrentFilenamePrefix: settings.torrent_filename_prefix,
        defaultTorrentView: settings.default_torrent_view,
        showLatestAnnouncement: settings.show_latest_announcement,
        customNavigationItems: [
          {
            label: "Wiki",
            url,
            open_in_new_tab: true,
            enabled: true,
          },
        ],
        reason: "",
      }).success
    ).toBe(false)
  })
})
