import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "~/components/ui/tooltip"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import {
  type TorrentPurchaseStatus,
  torrentPurchaseHistoryKeys,
} from "~/features/torrent/api/torrent-purchases.queries"
import { TorrentDownloadButton } from "~/features/torrent/components/torrent-download-button"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("TorrentDownloadButton", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("uses a fresh request identifier after Core confirms an idempotency conflict", async () => {
    const user = userEvent.setup()
    const queryClient = purchaseQueryClient([purchaseRequiredStatus(42)])
    const requestIds: string[] = []
    vi.stubGlobal(
      "fetch",
      purchaseFetchMock(requestIds, {
        purchaseStatus: 409,
        problemCode: "torrent_purchase_idempotency_conflict",
      })
    )

    renderDownloadButton(queryClient, {
      torrentId: 42,
      torrentName: "测试种子",
    })

    await openPurchaseDialog(user, "测试种子")
    await user.click(screen.getByRole("button", { name: "确认购买并下载" }))
    await waitFor(() => expect(requestIds).toHaveLength(1))
    expect(await screen.findByRole("alert")).toHaveTextContent("请求标识冲突")

    await user.click(screen.getByRole("button", { name: "确认购买并下载" }))
    await waitFor(() => expect(requestIds).toHaveLength(2))

    expect(requestIds[0]).toMatch(/^[0-9a-f-]{36}$/)
    expect(requestIds[1]).toMatch(/^[0-9a-f-]{36}$/)
    expect(requestIds[1]).not.toBe(requestIds[0])
  })

  it("does not carry an uncertain purchase identifier to another torrent", async () => {
    const user = userEvent.setup()
    const queryClient = purchaseQueryClient([
      purchaseRequiredStatus(42),
      purchaseRequiredStatus(43),
    ])
    const requestIds: string[] = []
    vi.stubGlobal(
      "fetch",
      purchaseFetchMock(requestIds, {
        purchaseStatus: 503,
        problemCode: "service_unavailable",
      })
    )

    const view = renderDownloadButton(queryClient, {
      torrentId: 42,
      torrentName: "第一条种子",
    })
    await openPurchaseDialog(user, "第一条种子")
    await user.click(screen.getByRole("button", { name: "确认购买并下载" }))
    await waitFor(() => expect(requestIds).toHaveLength(1))

    view.rerender(downloadButtonTree(queryClient, 43, "第二条种子"))
    await waitFor(() =>
      expect(
        screen.queryByRole("alertdialog", { name: "购买种子下载权限" })
      ).not.toBeInTheDocument()
    )
    await openPurchaseDialog(user, "第二条种子")
    await user.click(screen.getByRole("button", { name: "确认购买并下载" }))
    await waitFor(() => expect(requestIds).toHaveLength(2))

    expect(requestIds[1]).not.toBe(requestIds[0])
  })
})

function renderDownloadButton(
  queryClient: QueryClient,
  input: { torrentId: number; torrentName: string }
) {
  return render(
    downloadButtonTree(queryClient, input.torrentId, input.torrentName)
  )
}

function downloadButtonTree(
  queryClient: QueryClient,
  torrentId: number,
  torrentName: string
) {
  return (
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <TorrentDownloadButton
            torrentId={torrentId}
            torrentName={torrentName}
            purchaseAware
            showLabel
          />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>
  )
}

async function openPurchaseDialog(
  user: ReturnType<typeof userEvent.setup>,
  torrentName: string
) {
  await user.click(
    screen.getByRole("button", {
      name: `下载“${torrentName}”的种子文件`,
    })
  )
  expect(
    await screen.findByRole("alertdialog", { name: "购买种子下载权限" })
  ).toBeVisible()
}

function purchaseQueryClient(statuses: TorrentPurchaseStatus[]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: userId,
      username: "buyer",
      display_name: "购买测试用户",
      email_verified: true,
    },
    expires_at: "2026-09-22T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  for (const status of statuses) {
    queryClient.setQueryData(
      torrentPurchaseHistoryKeys.status(userId, status.torrent_id),
      status
    )
  }
  return queryClient
}

function purchaseRequiredStatus(torrentId: number): TorrentPurchaseStatus {
  return {
    torrent_id: torrentId,
    title: `种子 ${torrentId}`,
    price: "100",
    tax: "10",
    seller_income: "90",
    magic_balance: "1000",
    state: "purchase_required",
    policy_revision: "purchase-test-v1",
    legacy_import: false,
  }
}

function purchaseFetchMock(
  requestIds: string[],
  response: { purchaseStatus: number; problemCode: string }
) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : new Request(input, init)
    if (request.method === "POST" && request.url.endsWith("/purchase")) {
      requestIds.push(request.headers.get("Idempotency-Key") ?? "")
      return jsonResponse(
        {
          type: "about:blank",
          title:
            response.problemCode === "torrent_purchase_idempotency_conflict"
              ? "请求标识冲突"
              : "服务暂时不可用",
          status: response.purchaseStatus,
          code: response.problemCode,
          detail:
            response.problemCode === "torrent_purchase_idempotency_conflict"
              ? "请求标识冲突"
              : "服务暂时不可用",
          request_id: "0198f20a-6da8-7e51-9c64-333333333333",
        },
        response.purchaseStatus
      )
    }
    if (request.method === "GET" && request.url.endsWith("/purchase")) {
      const torrentId = Number(request.url.match(/torrents\/(\d+)/)?.[1])
      return jsonResponse(purchaseRequiredStatus(torrentId))
    }
    throw new Error(`unexpected request: ${request.method} ${request.url}`)
  })
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/problem+json" },
  })
}
