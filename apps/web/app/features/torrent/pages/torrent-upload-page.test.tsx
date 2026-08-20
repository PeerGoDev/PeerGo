import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  sessionKeys,
  type WebSession,
} from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import { TorrentUploadPage } from "~/features/torrent/pages/torrent-upload-page"
import { MockUploadXMLHttpRequest } from "~/test/upload-xhr"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"
const session: WebSession = {
  user: {
    id: userId,
    username: "demo",
    display_name: "PeerGo 演示用户",
    email_verified: true,
  },
  expires_at: "2026-08-10T00:00:00Z",
  csrf_token: "c".repeat(43),
}

describe("TorrentUploadPage", () => {
  beforeEach(() => {
    MockUploadXMLHttpRequest.reset()
    vi.stubGlobal("XMLHttpRequest", MockUploadXMLHttpRequest)
    vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue(
      "0198f20a-6da8-7e51-9c64-777777777777"
    )
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: vi.fn(() => "blob:peergo-screenshot"),
    })
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: vi.fn(),
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    Reflect.deleteProperty(URL, "createObjectURL")
    Reflect.deleteProperty(URL, "revokeObjectURL")
  })

  it("validates the first visible field locally", async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByRole("button", { name: "上传种子" }))

    expect(screen.getByText("请选择一个 .torrent 文件")).toBeVisible()
    await waitFor(() => {
      expect(screen.getByLabelText("种子文件")).toHaveFocus()
    })
    expect(MockUploadXMLHttpRequest.instances).toHaveLength(0)
  })

  it("presents the PtYes-compatible upload sequence with real screenshot input", () => {
    renderPage()

    expect(screen.getByText("Announce URL:").parentElement).toHaveClass(
      "min-h-[160px]",
      "sm:min-h-[60px]"
    )
    expect(screen.getByText(/页面不会显示私有 passkey/)).toBeInTheDocument()
    expect(screen.getByText("点击或拖拽上传 .torrent 文件")).toBeVisible()
    expect(
      screen.getByRole("heading", { level: 2, name: "基本信息" })
    ).toBeVisible()
    expect(screen.getByLabelText("截图")).toBeInTheDocument()
    expect(screen.getByText("点击或拖拽上传截图")).toBeVisible()
    expect(screen.getByRole("checkbox", { name: "匿名上传" })).toBeVisible()
    expect(screen.getByLabelText("描述 *")).toBeVisible()
    expect(screen.getByLabelText("MediaInfo/BDInfo *")).toBeVisible()
    expect(screen.getByLabelText("副标题 *")).toBeVisible()
    expect(screen.getByText("类型")).toBeVisible()
    expect(screen.getByRole("button", { name: "剧情" })).toBeVisible()
    expect(screen.getByRole("combobox", { name: "地区 *" })).toBeVisible()
    expect(screen.getByRole("combobox", { name: "来源 *" })).toBeVisible()
    expect(screen.queryByText("发布类型")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "解析 IMDb" })).toBeDisabled()
    expect(screen.getAllByRole("button", { name: "预览" }).at(-1)).toHaveClass(
      "w-30"
    )
    expect(screen.getByRole("button", { name: "上传种子" })).toHaveClass("w-30")
  })

  it("uploads with progress and renders a pending-review receipt", async () => {
    const user = userEvent.setup()
    renderPage()
    const torrentFile = new File(["d4:infodee"], "example.torrent", {
      type: "application/x-bittorrent",
    })
    const screenshot = new File(["image"], "cover.png", {
      type: "image/png",
    })

    await user.upload(screen.getByLabelText("种子文件"), torrentFile)
    await user.upload(screen.getByLabelText("截图"), screenshot)
    expect(screen.getByRole("combobox", { name: "分类 *" })).toHaveTextContent(
      "电影"
    )
    await user.click(screen.getByRole("button", { name: "剧情" }))
    await user.click(screen.getByRole("combobox", { name: "地区 *" }))
    await user.click(await screen.findByRole("option", { name: "中国大陆" }))
    await user.click(screen.getByRole("combobox", { name: "来源 *" }))
    await user.click(await screen.findByRole("option", { name: "WEB-DL" }))
    changeField("标题 *", "Example Release")
    changeField("副标题 *", "First edition")
    changeField("IMDb", "tt1234567")
    changeField("描述 *", "Release description")
    changeField("MediaInfo/BDInfo *", "General")
    await user.click(screen.getByRole("checkbox", { name: "匿名上传" }))
    await user.click(screen.getByRole("button", { name: "上传种子" }))

    const request = MockUploadXMLHttpRequest.instances[0]
    expect(request).toBeDefined()
    const body = request.body as FormData
    expect(body.get("description")).toBe("Release description")
    expect(body.get("media_info")).toBe("General")
    expect(body.get("anonymous")).toBe("true")
    expect(body.get("imdb_id")).toBe("tt1234567")
    expect(body.get("facet_selections[0][facet_id]")).toBe("genre")
    expect(body.get("facet_selections[0][option_keys][0]")).toBe("drama")
    expect(body.get("facet_selections[1][facet_id]")).toBe("region")
    expect(body.get("facet_selections[1][option_keys][0]")).toBe(
      "mainland-china"
    )
    expect(body.get("facet_selections[2][facet_id]")).toBe("release-type")
    expect(body.get("facet_selections[2][option_keys][0]")).toBe("web-dl")
    expect(body.getAll("screenshots")).toEqual([screenshot])
    act(() => {
      request.reportProgress(1, 2)
    })
    expect(screen.getByText("50%")).toBeVisible()
    act(() => {
      request.completeUpload()
    })
    expect(screen.getByText("正在检查种子文件")).toBeVisible()

    act(() => {
      request.respond(201, {
        id: 42,
        info_hash_v1: "a".repeat(40),
        state: "pending_review",
        content_name: "Example Release",
        total_size_bytes: 4096,
        file_count: 2,
        submitted_at: "2026-08-09T12:00:00Z",
      })
    })

    expect(
      await screen.findByRole("heading", { name: "已进入审核队列" })
    ).toBeVisible()
    expect(screen.getByText("4 KB")).toBeVisible()
    expect(screen.getByText("2 个")).toBeVisible()
    expect(
      screen.getByText("种子已保存并等待审核，目前还没有公开发布。")
    ).toBeVisible()
  })

  it("previews the current metadata without sending an upload", async () => {
    const user = userEvent.setup()
    renderPage()

    changeField("标题 *", "Preview Release")
    changeField("描述 *", "**Preview body**")
    const previewButtons = screen.getAllByRole("button", { name: "预览" })
    await user.click(previewButtons.at(-1)!)

    const dialog = screen.getByRole("dialog")
    expect(dialog).toBeVisible()
    expect(screen.getByRole("heading", { name: "种子预览" })).toBeVisible()
    expect(within(dialog).getByText("Preview Release")).toBeVisible()
    expect(within(dialog).getByText("Preview body")).toBeVisible()
    expect(MockUploadXMLHttpRequest.instances).toHaveLength(0)
  })

  it("normalizes an external identifier with the visible parse action", async () => {
    const user = userEvent.setup()
    renderPage()

    changeField("IMDb", "https://www.imdb.com/title/tt7654321/")
    await user.click(screen.getByRole("button", { name: "解析 IMDb" }))

    expect(screen.getByLabelText("IMDb")).toHaveValue("tt7654321")
  })

  it("does not expose the form without torrent.submit", () => {
    renderPage({ canSubmit: false })

    expect(screen.getByText("当前账户不能提交种子")).toBeVisible()
    expect(
      screen.queryByRole("button", { name: "上传种子" })
    ).not.toBeInTheDocument()
  })
})

function changeField(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } })
}

function renderPage({ canSubmit = true }: { canSubmit?: boolean } = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), session)
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "development-v1",
    items: canSubmit
      ? [
          {
            action: "torrent.submit",
            description: "提交本人种子",
            scope: { type: "site", id: "peergo" },
            expires_at: "2026-08-10T00:00:00Z",
          },
        ]
      : [],
  })
  queryClient.setQueryData(torrentKeys.categories(), [
    { id: "movies", name: "电影", torrent_count: 0 },
    { id: "tv", name: "电视剧", torrent_count: 0 },
  ])
  queryClient.setQueryData(torrentKeys.categoryFacets("movies"), [
    {
      id: "genre",
      name: "类型",
      selection_mode: "multi_option",
      required: true,
      options: [
        { key: "drama", label: "剧情" },
        { key: "action", label: "动作" },
      ],
    },
    {
      id: "region",
      name: "地区",
      selection_mode: "single_option",
      required: true,
      options: [
        { key: "mainland-china", label: "中国大陆" },
        { key: "united-states", label: "美国" },
      ],
    },
    {
      id: "source-medium",
      name: "来源",
      selection_mode: "single_option",
      required: false,
      requirement_group: "source",
      options: [
        { key: "blu-ray", label: "Blu-ray" },
        { key: "other", label: "其它" },
      ],
    },
    {
      id: "release-type",
      name: "发布类型",
      selection_mode: "single_option",
      required: false,
      requirement_group: "source",
      options: [
        { key: "web-dl", label: "WEB-DL" },
        { key: "hdtv", label: "HDTV" },
        { key: "other", label: "其它" },
      ],
    },
  ])

  return render(
    <MemoryRouter initialEntries={["/upload"]}>
      <QueryClientProvider client={queryClient}>
        <TorrentUploadPage />
      </QueryClientProvider>
    </MemoryRouter>
  )
}
