import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "~/components/ui/tooltip"
import {
  torrentKeys,
  type TorrentFilePage,
  type TorrentPublicContent,
  type TorrentPublicDetail,
  type TorrentRelatedVersions,
  type TorrentSwarmOverview,
} from "~/features/torrent/api/torrent.queries"
import { TorrentDetailPage } from "~/features/torrent/pages/torrent-detail-page"

const torrentId = 42

describe("TorrentDetailPage", () => {
  it("renders the compact summary and independently cached file page", async () => {
    const user = userEvent.setup()
    const queryClient = detailTestClient()
    const detail: TorrentPublicDetail = {
      id: torrentId,
      category: { id: "movies", name: "电影" },
      title: "Final Release 2026",
      subtitle: "首版详情",
      content_name: "Final.Release.2026",
      uploader_display_name: "PeerGo 发布者",
      anonymous: false,
      promotion: "free",
      promotion_ends_at: "2026-08-13T03:53:39Z",
      sticky_until: null,
      facets: [
        {
          facet_id: "resolution",
          facet_name: "分辨率",
          option_key: "1080p",
          option_label: "1080p",
        },
      ],
      external_identifiers: [{ provider: "imdb", external_id: "tt1234567" }],
      info_hash_v1: "a".repeat(40),
      total_size_bytes: 1_073_741_824,
      payload_size_bytes: 1_073_741_000,
      file_count: 2,
      padding_file_count: 1,
      piece_length_bytes: 262_144,
      piece_count: 4096,
      screenshot_count: 2,
      state: "published",
      submitted_at: "2026-08-09T10:00:00Z",
      published_at: "2026-08-09T11:00:00Z",
    }
    const files: TorrentFilePage = {
      torrent_id: torrentId,
      total: 2,
      limit: 50,
      offset: 0,
      items: [
        {
          file_index: 0,
          display_path: "Final.Release.2026/movie.mkv",
          size_bytes: 1_073_741_000,
          is_padding: false,
        },
        {
          file_index: 1,
          display_path: "Final.Release.2026/.pad/724",
          size_bytes: 824,
          is_padding: true,
        },
      ],
    }
    queryClient.setQueryData(torrentKeys.detail(torrentId), detail)
    queryClient.setQueryData(torrentKeys.content(torrentId), {
      torrent_id: torrentId,
      description: "**完整发布说明**",
      description_format: "markdown",
      media_info: "General\nFormat: Matroska",
    } satisfies TorrentPublicContent)
    queryClient.setQueryData(torrentKeys.related(torrentId), {
      items: [
        {
          id: 43,
          name: "Final Release 2026 2160p",
          subtitle: "同一资源的 4K 版本",
          category: { id: "movies", name: "电影" },
          size_bytes: 2_147_483_648,
          promotion: "free",
          sticky_until: null,
          uploaded_at: "2026-08-08T11:00:00Z",
          seeders: 8,
          leechers: 1,
          completed: 19,
          swarm_observed_at: "2026-08-09T11:59:00Z",
          swarm_stale: false,
        },
      ],
    } satisfies TorrentRelatedVersions)
    queryClient.setQueryData(torrentKeys.swarm(torrentId), {
      torrent_id: torrentId,
      seeders: 18,
      leechers: 5,
      completed: 61,
      observed_at: "2026-08-09T11:59:00Z",
      stale: false,
      confidence: "fresh",
    } satisfies TorrentSwarmOverview)
    queryClient.setQueryData(torrentKeys.files(torrentId, 50, 0), files)

    renderDetailPage(queryClient, `/torrents/${torrentId}`)

    expect(
      screen.getByRole("heading", { name: "Final Release 2026" })
    ).toBeVisible()
    const cover = screen.getByRole("img", {
      name: "Final Release 2026封面",
    })
    expect(cover).toBeVisible()
    expect(cover).toHaveClass("h-auto", "max-h-56", "w-full", "object-contain")
    expect(cover.parentElement).not.toHaveClass("h-56")
    expect(screen.getByText("PeerGo 发布者")).toBeVisible()
    expect(screen.getByText("PeerGo 发布者").closest("dl")).toHaveClass(
      "justify-start"
    )
    expect(screen.getAllByText("免费").length).toBeGreaterThan(0)
    expect(screen.getByText("2026-08-13")).toBeVisible()
    expect(screen.getByText("2026-08-13").parentElement).toHaveClass(
      "text-xs",
      "font-semibold",
      "opacity-70"
    )
    expect(screen.getByText("1080p")).toBeVisible()
    expect(screen.getByRole("link", { name: /IMDb/ })).toHaveAttribute(
      "href",
      "https://www.imdb.com/title/tt1234567/"
    )
    expect(screen.getByText("完整发布说明")).toBeVisible()
    expect(screen.getByRole("button", { name: "分享到动态圈" })).toBeVisible()
    expect(screen.getByText("18 个做种")).toBeVisible()
    expect(screen.getByText("5 个下载者")).toBeVisible()
    const peerListHeader = screen
      .getByText("用户列表")
      .closest("[data-slot=card-header]")
    expect(peerListHeader).toHaveClass("p-6", "pb-2")
    expect(peerListHeader?.closest("[data-slot=card]")).toHaveClass(
      "gap-0",
      "py-0"
    )
    expect(
      screen.queryByText(/只显示 Tracker 聚合统计/)
    ).not.toBeInTheDocument()
    const mediaInfoHeader = screen
      .getByText("MediaInfo/BDInfo")
      .closest("[data-slot=card-header]")
    expect(mediaInfoHeader).not.toBeNull()
    expect(mediaInfoHeader).toHaveClass("p-6", "pb-2")
    expect(mediaInfoHeader).not.toHaveClass("pt-[18px]")
    const mediaInfoCard = screen
      .getByText("MediaInfo/BDInfo")
      .closest("[data-slot=card]")
    expect(mediaInfoCard).toHaveClass("gap-0", "py-0")
    expect(
      mediaInfoCard?.querySelector("[data-slot=card-content]")
    ).toHaveClass("px-6", "pb-6")
    expect(
      screen.getByText("用户列表").closest("[data-slot=card-title]")
    ).toHaveClass("font-semibold")
    const rawMediaInfoButton = screen.getByRole("button", {
      name: "查看原始信息",
    })
    expect(rawMediaInfoButton).toHaveClass("text-muted-foreground")
    await user.click(rawMediaInfoButton)
    const rawMediaInfo = screen.getByText(/General/, { selector: "pre" })
    expect(screen.getByRole("button", { name: "隐藏原始信息" })).toBeVisible()
    expect(
      screen
        .getByRole("button", { name: "隐藏原始信息" })
        .compareDocumentPosition(rawMediaInfo) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(rawMediaInfo).toHaveClass(
      "mt-4",
      "overflow-x-auto",
      "rounded",
      "bg-muted",
      "p-4",
      "text-xs"
    )
    expect(rawMediaInfo).not.toHaveClass("max-h-[32rem]", "border")
    expect(screen.getByText("截图 (2)")).toBeVisible()
    expect(screen.getByRole("img", { name: "截图 1" })).toBeVisible()
    expect(screen.getByRole("img", { name: "截图 2" })).toBeVisible()
    expect(screen.getByRole("img", { name: "截图 1" })).toHaveAttribute(
      "src",
      `http://localhost:3000/api/v1/torrents/${torrentId}/screenshots/0`
    )
    const firstScreenshot = screen.getByRole("button", {
      name: "查看截图 1",
    })
    expect(firstScreenshot).toHaveClass("w-32", "sm:w-auto")
    await user.click(firstScreenshot)
    expect(screen.getByRole("img", { name: "截图 1 大图" })).toBeVisible()
    expect(screen.getByText("1 / 2")).toBeVisible()
    await user.click(screen.getByRole("button", { name: "下一张截图" }))
    expect(screen.getByRole("img", { name: "截图 2 大图" })).toBeVisible()
    expect(screen.getByText("2 / 2")).toBeVisible()
    await user.click(screen.getByRole("button", { name: "上一张截图" }))
    expect(screen.getByRole("img", { name: "截图 1 大图" })).toBeVisible()
    await user.click(screen.getByRole("button", { name: "关闭" }))
    expect(
      screen.getByRole("link", { name: /Final Release 2026 2160p/ })
    ).toHaveAttribute("href", "/torrents/43")
    expect(screen.getByText("1 GB")).toBeVisible()
    expect(screen.getByText("18")).toBeVisible()
    expect(screen.getByText("61")).toBeVisible()
    expect(screen.getByText(/2 个文件/)).toBeVisible()
    expect(
      screen.getByText(/2 个文件/).closest("[data-slot='card-title']")
    ).toHaveClass("text-base", "font-semibold")
    expect(screen.getByText("Final.Release.2026/movie.mkv")).toBeVisible()
    expect(screen.getByText("填充")).toBeVisible()
    const downloadButton = screen.getByRole("button", {
      name: "下载“Final Release 2026”的种子文件",
    })
    expect(downloadButton).toBeEnabled()
    const desktopDownloadActions = downloadButton.closest(
      "[data-slot=torrent-download-actions]"
    )
    expect(desktopDownloadActions).toHaveClass("h-10", "w-full")
    expect(desktopDownloadActions?.parentElement).toHaveClass(
      "w-[156px]",
      "max-md:w-full"
    )
    const copyDownloadAddress = screen.getByRole("button", {
      name: "复制“Final Release 2026”的下载地址",
    })
    expect(copyDownloadAddress).toBeVisible()
    expect(copyDownloadAddress).toHaveClass(
      "h-full",
      "rounded-l-none",
      "border-l-0"
    )
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    })
    await user.click(copyDownloadAddress)
    expect(writeText).toHaveBeenCalledWith(
      `http://localhost:3000/api/v1/torrents/${torrentId}/download`
    )
    expect(await screen.findByText("已复制下载地址")).toBeVisible()

    expect(screen.getByText("a".repeat(40))).toBeVisible()
    expect(
      screen.queryByRole("button", { name: "展开种子参数" })
    ).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: /用户列表/ }))

    expect(screen.getByText(/登录后可查看当前做种者和下载者/)).toBeVisible()
  })

  it("does not query a legacy demo slug as a real aggregate", () => {
    renderDetailPage(detailTestClient(), "/torrents/tiny-orchestra")

    expect(screen.getByText("种子不存在")).toHaveClass(
      "text-xl",
      "font-medium",
      "text-destructive"
    )
    expect(screen.getByRole("button", { name: "返回首页" })).toHaveAttribute(
      "href",
      "/"
    )
  })

  it("preserves the Rousi cover column with an explicit migrated-image fallback", () => {
    const queryClient = detailTestClient()
    queryClient.setQueryData(torrentKeys.detail(torrentId), {
      id: torrentId,
      category: { id: "movies", name: "电影" },
      title: "Migrated Release",
      subtitle: "仅迁移用户、种子数据与原始种子文件",
      content_name: "Migrated.Release",
      uploader_display_name: "迁移用户",
      anonymous: true,
      promotion: "none",
      promotion_ends_at: null,
      sticky_until: null,
      facets: [],
      external_identifiers: [],
      info_hash_v1: "b".repeat(40),
      total_size_bytes: 1_073_741_824,
      payload_size_bytes: 1_073_741_824,
      file_count: 1,
      padding_file_count: 0,
      piece_length_bytes: 262_144,
      piece_count: 4096,
      screenshot_count: 0,
      state: "published",
      submitted_at: "2026-08-09T10:00:00Z",
      published_at: "2026-08-09T11:00:00Z",
    } satisfies TorrentPublicDetail)
    queryClient.setQueryData(torrentKeys.swarm(torrentId), {
      torrent_id: torrentId,
      seeders: 0,
      leechers: 0,
      completed: 0,
      observed_at: "2026-08-09T11:59:00Z",
      stale: false,
      confidence: "fresh",
    } satisfies TorrentSwarmOverview)

    renderDetailPage(queryClient, `/torrents/${torrentId}`)

    expect(screen.getByText("匿名")).toBeVisible()
    expect(screen.queryByText("迁移用户")).not.toBeInTheDocument()
    expect(
      screen.getByRole("img", { name: "Migrated Release暂无封面" })
    ).toBeVisible()
    expect(screen.getByText("暂无封面").parentElement).toHaveClass(
      "bg-gradient-to-br",
      "from-neutral-100",
      "to-neutral-300"
    )
    const fallbackCover = screen.getByRole("img", {
      name: "Migrated Release暂无封面",
    })
    expect(fallbackCover).toHaveClass("[&_svg]:size-7")
    expect(fallbackCover).not.toHaveClass("min-h-56")
    expect(fallbackCover.parentElement).toHaveClass(
      "h-36",
      "md:h-56",
      "self-start"
    )
    expect(
      screen.queryByRole("img", { name: "Migrated Release封面" })
    ).not.toBeInTheDocument()
  })

  it("preserves the Rousi description card when migrated text is empty", () => {
    const queryClient = detailTestClient()
    queryClient.setQueryData(torrentKeys.detail(torrentId), {
      id: torrentId,
      category: { id: "movies", name: "电影" },
      title: "Release Without Description",
      subtitle: "",
      content_name: "Release.Without.Description",
      uploader_display_name: "迁移用户",
      anonymous: false,
      promotion: "none",
      promotion_ends_at: null,
      sticky_until: null,
      facets: [],
      external_identifiers: [],
      info_hash_v1: "c".repeat(40),
      total_size_bytes: 1_073_741_824,
      payload_size_bytes: 1_073_741_824,
      file_count: 1,
      padding_file_count: 0,
      piece_length_bytes: 262_144,
      piece_count: 4096,
      screenshot_count: 0,
      state: "published",
      submitted_at: "2026-08-09T10:00:00Z",
      published_at: "2026-08-09T11:00:00Z",
    } satisfies TorrentPublicDetail)
    queryClient.setQueryData(torrentKeys.content(torrentId), {
      torrent_id: torrentId,
      description: "",
      description_format: "plain_text",
      media_info: "",
    } satisfies TorrentPublicContent)

    renderDetailPage(queryClient, `/torrents/${torrentId}`)

    expect(screen.getByText("描述")).toBeVisible()
    expect(screen.getByText("暂无描述")).toBeVisible()
  })

  it("shows three related versions before expanding the remaining items", () => {
    const queryClient = detailTestClient()
    queryClient.setQueryData(torrentKeys.detail(torrentId), {
      id: torrentId,
      category: { id: "movies", name: "电影" },
      title: "Release With Versions",
      subtitle: "",
      content_name: "Release.With.Versions",
      uploader_display_name: "迁移用户",
      anonymous: false,
      promotion: "none",
      promotion_ends_at: null,
      sticky_until: null,
      facets: [],
      external_identifiers: [],
      info_hash_v1: "d".repeat(40),
      total_size_bytes: 1_073_741_824,
      payload_size_bytes: 1_073_741_824,
      file_count: 1,
      padding_file_count: 0,
      piece_length_bytes: 262_144,
      piece_count: 4096,
      screenshot_count: 0,
      state: "published",
      submitted_at: "2026-08-09T10:00:00Z",
      published_at: "2026-08-09T11:00:00Z",
    } satisfies TorrentPublicDetail)
    queryClient.setQueryData(torrentKeys.related(torrentId), {
      items: Array.from({ length: 5 }, (_, index) => ({
        id: 43 + index,
        name:
          index === 0
            ? "Related Version 1 With A Very Long Release Name For Layout Verification"
            : `Related Version ${index + 1}`,
        subtitle: "",
        category: { id: "movies", name: "电影" },
        size_bytes: 2_147_483_648,
        promotion: "none" as const,
        sticky_until: null,
        uploaded_at: "2026-08-08T11:00:00Z",
        seeders: 8,
        leechers: 1,
        completed: 19,
        swarm_observed_at: "2026-08-09T11:59:00Z",
        swarm_stale: false,
      })),
    } satisfies TorrentRelatedVersions)

    renderDetailPage(queryClient, `/torrents/${torrentId}`)

    expect(
      screen.getByText(
        "Related Version 1 With A Very Long Release Name For Layout Verification"
      )
    ).toHaveClass("block", "truncate")
    expect(screen.getByText("Related Version 3")).toBeVisible()
    expect(screen.queryByText("Related Version 4")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "展开更多 (2) ▼" }))
    expect(screen.getByText("Related Version 4")).toBeVisible()
    expect(screen.getByText("Related Version 5")).toBeVisible()
    expect(screen.getByRole("button", { name: "收起其它版本 ▲" })).toBeVisible()
  })
})

function detailTestClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderDetailPage(queryClient: QueryClient, path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <Routes>
            <Route
              path="/torrents/:torrentId"
              element={<TorrentDetailPage />}
            />
          </Routes>
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>
  )
}
