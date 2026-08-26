import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import { moviePilotCredentialKeys } from "~/features/auth/api/moviepilot-credential.queries"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import { MoviePilotCredentialPage } from "~/features/auth/pages/moviepilot-credential-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"
const apiKey = `pgk_${"A".repeat(43)}`

describe("MoviePilotCredentialPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("keeps API Key in the old PtYes passkey position and shows the raw key once", async () => {
    const user = userEvent.setup()
    const queryClient = moviePilotQueryClient()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse(
          {
            credential: activeCredential(),
            api_key: apiKey,
          },
          201
        )
      )
      .mockResolvedValueOnce(jsonResponse(activeCredential()))
    vi.stubGlobal("fetch", fetchMock)

    render(
      <MemoryRouter initialEntries={["/account/api-key"]}>
        <QueryClientProvider client={queryClient}>
          <MoviePilotCredentialPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    const securityLink = screen.getByRole("link", { name: "安全设置" })
    const apiKeyLink = screen.getByRole("link", { name: "API Key" })
    expect(apiKeyLink).toHaveAttribute("href", "/account/api-key")
    expect(apiKeyLink).toHaveAttribute("aria-current", "page")
    expect(
      apiKeyLink.compareDocumentPosition(securityLink) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(screen.getByText(/仅保存 SHA-256 哈希和状态/)).toBeVisible()

    await user.click(screen.getByRole("button", { name: "创建 API Key" }))

    expect(await screen.findByText("立即保存新 API Key")).toBeVisible()
    expect(screen.getByLabelText("新 MoviePilot API Key")).toHaveValue(apiKey)
    expect(fetchMock).toHaveBeenCalledTimes(2)

    await user.click(screen.getByRole("button", { name: "我已保存，隐藏密钥" }))
    expect(
      screen.queryByLabelText("新 MoviePilot API Key")
    ).not.toBeInTheDocument()
    expect(screen.getByDisplayValue(/^pgk_A{8}•+$/)).toBeVisible()
  })
})

function moviePilotQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: userId,
      username: "demo",
      display_name: "PeerGo 演示用户",
      email_verified: true,
    },
    expires_at: "2026-09-05T12:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(moviePilotCredentialKeys.status(userId), {
    active: false,
    scopes: [
      "profile:read",
      "torrent:read",
      "torrent:download",
      "attendance:read",
      "attendance:claim",
    ],
  })
  return queryClient
}

function activeCredential() {
  return {
    active: true,
    key_prefix: "pgk_AAAAAAAA",
    version: 1,
    scopes: [
      "profile:read",
      "torrent:read",
      "torrent:download",
      "attendance:read",
      "attendance:claim",
    ],
    created_at: "2026-08-26T10:00:00Z",
  }
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}
