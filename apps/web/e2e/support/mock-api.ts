import type { Page, Route } from "@playwright/test"

type MockApiOptions = {
  serviceUnavailable?: boolean
  secondFactorRequired?: boolean
  includeTorrent?: boolean
}

const siteInfo = {
  name: "PeerGo",
  description: "热爱可抵岁月漫长",
  registration_mode: "open",
  registration_username_min_characters: 3,
  registration_username_max_characters: 20,
  registration_email_domain_mode: "any",
  human_verification: {
    provider: "disabled",
    site_key: "",
    registration_enabled: false,
    login_enabled: false,
    password_recovery_enabled: false,
  },
  online_users: 8,
  default_torrent_view: "list",
  show_latest_announcement: false,
}

const session = {
  user: {
    id: "0198f20a-6da8-7e51-9c64-111111111111",
    numeric_id: 12_328,
    username: "demo",
    display_name: "PeerGo 演示用户",
    email_verified: true,
  },
  expires_at: "2026-09-20T12:00:00Z",
  csrf_token: "c".repeat(43),
}

const torrent = {
  id: 42,
  name: "Published Release",
  subtitle: "PeerGo mobile fixture",
  category: { id: "movies", name: "电影" },
  size_bytes: 1_073_741_824,
  seeders: 12,
  leechers: 3,
  completed: 40,
  promotion: "free",
  sticky_until: null,
  uploaded_at: "2026-08-05T10:00:00Z",
  swarm_observed_at: "2026-08-05T09:00:00Z",
  swarm_stale: false,
}

export async function mockPublicApi(page: Page, options: MockApiOptions = {}) {
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request()
    const url = new URL(request.url())

    if (options.serviceUnavailable) {
      await problem(route, 502, "service_unavailable", "服务暂时不可用")
      return
    }

    if (url.pathname === "/api/v1/site" && request.method() === "GET") {
      await json(route, 200, siteInfo)
      return
    }
    if (url.pathname === "/api/v1/session" && request.method() === "GET") {
      await route.fulfill({ status: 204 })
      return
    }
    if (url.pathname === "/api/v1/session" && request.method() === "POST") {
      if (options.secondFactorRequired) {
        await problem(route, 428, "second_factor_required", "需要两步验证码")
        return
      }
      await json(route, 201, session)
      return
    }
    if (
      url.pathname === "/api/v1/me/capabilities" &&
      request.method() === "GET"
    ) {
      await json(route, 200, { items: [] })
      return
    }
    if (url.pathname === "/api/v1/categories" && request.method() === "GET") {
      await json(route, 200, [
        { id: "movies", name: "电影", enabled: true, torrent_count: 1 },
      ])
      return
    }
    if (url.pathname === "/api/v1/torrents" && request.method() === "GET") {
      await json(route, 200, {
        items: options.includeTorrent ? [torrent] : [],
        total: options.includeTorrent ? 1 : 0,
        limit: 20,
        offset: 0,
      })
      return
    }

    await problem(route, 404, "e2e_fixture_missing", "E2E fixture 未覆盖请求")
  })
}

async function json(route: Route, status: number, body: unknown) {
  await route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  })
}

async function problem(
  route: Route,
  status: number,
  code: string,
  title: string
) {
  await route.fulfill({
    status,
    contentType: "application/problem+json; charset=utf-8",
    body: JSON.stringify({
      type: "about:blank",
      title,
      status,
      code,
      detail: title,
      request_id: "e2e-request-id",
    }),
  })
}
