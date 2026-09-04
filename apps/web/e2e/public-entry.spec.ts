import { expect, test } from "@playwright/test"

import { mockPublicApi } from "./support/mock-api"

test("guest entry pages render without browser errors", async ({ page }) => {
  const browserErrors: Error[] = []
  page.on("pageerror", (error) => browserErrors.push(error))
  await mockPublicApi(page)

  const entries = [
    { path: "/", heading: "PeerGo" },
    { path: "/login", heading: "登录" },
    { path: "/register", heading: "注册" },
    { path: "/forgot-password", heading: "找回密码" },
    { path: "/restrictions", heading: "封禁记录与申诉" },
  ]

  for (const entry of entries) {
    await page.goto(entry.path)
    await expect(
      page.getByRole("heading", { name: entry.heading, exact: true })
    ).toBeVisible()
    await expect(page.locator("body")).not.toContainText("页面暂时不可用")
    expect(await hasHorizontalPageOverflow(page)).toBe(false)
  }

  expect(browserErrors).toEqual([])
})

test("login validates locally and creates a session", async ({ page }) => {
  await mockPublicApi(page)
  await page.goto("/login")

  await page.getByRole("button", { name: "登录", exact: true }).click()
  await expect(page.getByText("请输入用户名或邮箱")).toBeVisible()
  await expect(page.getByText("请输入密码")).toBeVisible()
  await expect(page.getByLabel("用户名 / 邮箱")).toBeFocused()

  await page.getByLabel("用户名 / 邮箱").fill("demo")
  await page.getByLabel("密码", { exact: true }).fill("correct-password")
  const sessionRequest = page.waitForRequest(
    (request) =>
      request.url().endsWith("/api/v1/session") && request.method() === "POST"
  )
  await page.getByRole("button", { name: "登录", exact: true }).click()

  const request = await sessionRequest
  expect(request.postDataJSON()).toMatchObject({
    identifier: "demo",
    password: "correct-password",
    remember_me: false,
  })
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole("heading", { name: "首页" })).toBeVisible()
  await expect(page.getByText("暂无种子")).toBeVisible()
})

test("second-factor challenge remains usable", async ({ page }) => {
  await mockPublicApi(page, { secondFactorRequired: true })
  await page.goto("/login")

  await page.getByLabel("用户名 / 邮箱").fill("demo")
  await page.getByLabel("密码", { exact: true }).fill("correct-password")
  await page.getByRole("button", { name: "登录", exact: true }).click()

  await expect(page.getByText("还需要第二因素")).toBeVisible()
  await expect(page.getByLabel("两步验证码（可选）")).toBeFocused()
})

test("authenticated navigation stays inside the mobile viewport", async ({
  page,
}) => {
  await mockPublicApi(page, { includeTorrent: true })
  await page.goto("/login")
  await page.getByLabel("用户名 / 邮箱").fill("demo")
  await page.getByLabel("密码", { exact: true }).fill("correct-password")
  await page.getByRole("button", { name: "登录", exact: true }).click()

  await expect(page.getByRole("heading", { name: "首页" })).toBeVisible()
  await expect(
    page.locator("a:visible").filter({ hasText: "Published Release" })
  ).toBeVisible()
  expect(await hasHorizontalPageOverflow(page)).toBe(false)

  if ((page.viewportSize()?.width ?? 0) < 1024) {
    await page.getByRole("button", { name: "切换侧栏" }).click()
    const mobileSidebar = page.locator(
      '[data-sidebar="sidebar"][data-mobile="true"]'
    )
    await expect(mobileSidebar).toBeVisible()
    await mobileSidebar.getByRole("link", { name: "种子", exact: true }).click()
    await expect(mobileSidebar).toBeHidden()
  } else {
    await page.getByRole("link", { name: "种子", exact: true }).click()
  }

  await expect(page).toHaveURL(/\/torrents$/)
  await expect(page.getByLabel("搜索种子标题")).toBeVisible()
  await expect(
    page.locator("a:visible").filter({ hasText: "Published Release" })
  ).toBeVisible()
  expect(await hasHorizontalPageOverflow(page)).toBe(false)
})

test("Core outage produces a bounded user-facing state", async ({ page }) => {
  const browserErrors: Error[] = []
  page.on("pageerror", (error) => browserErrors.push(error))
  await mockPublicApi(page, { serviceUnavailable: true })

  await page.goto("/login")
  await expect(page.getByText("暂时无法读取登录策略")).toBeVisible()
  await expect(page.getByText("请稍后刷新页面再试。")).toBeVisible()
  await expect(page.locator("body")).not.toContainText("页面暂时不可用")
  expect(browserErrors).toEqual([])
})

async function hasHorizontalPageOverflow(
  page: import("@playwright/test").Page
) {
  return page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth + 1
  )
}
