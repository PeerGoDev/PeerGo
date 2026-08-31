import { lazy, Suspense, useEffect, type ReactNode } from "react"
import {
  Links,
  Meta,
  Outlet,
  Scripts,
  ScrollRestoration,
  isRouteErrorResponse,
  useLocation,
  useNavigation,
} from "react-router"

import type { Route } from "./+types/root"
import "~/shared/polyfills/crypto-random-uuid"
import { AppProviders } from "~/shared/providers/app-providers"
import { SiteDocumentTitle } from "~/features/site/components/site-document-title"
import {
  AppLoadingSkeleton,
  RouteLoadingSkeleton,
} from "~/shared/components/app-loading-skeleton"
import "./app.css"

const LazyAppShell = lazy(() =>
  import("~/features/shell/components/app-shell").then(({ AppShell }) => ({
    default: AppShell,
  }))
)
const LazyStaffShell = lazy(() =>
  import("~/features/staff/components/staff-shell").then(({ StaffShell }) => ({
    default: StaffShell,
  }))
)
const pendingNavigationStorageKey = "peergo.pending-navigation.v1"

export const meta: Route.MetaFunction = () => [
  { title: "PeerGo" },
  {
    name: "description",
    content: "私有分享社区",
  },
]

export function Layout({ children }: { children: ReactNode }) {
  return (
    <html lang="zh-CN">
      <head>
        <meta charSet="utf-8" />
        <meta
          name="viewport"
          content="width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content"
        />
        <Meta />
        <Links />
      </head>
      <body>
        {children}
        <ScrollRestoration />
        <Scripts />
      </body>
    </html>
  )
}

export default function App() {
  const location = useLocation()
  const shell = location.pathname.startsWith("/staff")
    ? LazyStaffShell
    : LazyAppShell
  const Shell = shell
  useStaleAssetRecovery()

  return (
    <AppProviders>
      <SiteDocumentTitle />
      <Suspense fallback={<AppLoadingSkeleton />}>
        <Shell>
          <RoutedContent />
        </Shell>
      </Suspense>
    </AppProviders>
  )
}

function RoutedContent() {
  const location = useLocation()
  const navigation = useNavigation()
  const nextLocation = navigation.location
  const changingRoute =
    navigation.state === "loading" &&
    nextLocation !== undefined &&
    (nextLocation.pathname !== location.pathname ||
      nextLocation.search !== location.search)

  return changingRoute ? <RouteLoadingSkeleton /> : <Outlet />
}

function useStaleAssetRecovery() {
  const location = useLocation()

  useEffect(() => {
    try {
      globalThis.sessionStorage?.removeItem(pendingNavigationStorageKey)
    } catch {
      // Navigation recovery is best effort when storage is unavailable.
    }
  }, [location.hash, location.pathname, location.search])

  useEffect(() => {
    function rememberNavigation(event: MouseEvent) {
      if (
        event.defaultPrevented ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey ||
        !(event.target instanceof Element)
      ) {
        return
      }
      const anchor = event.target.closest<HTMLAnchorElement>("a[href]")
      if (
        !anchor ||
        anchor.hasAttribute("download") ||
        (anchor.target && anchor.target !== "_self")
      ) {
        return
      }
      const target = new URL(anchor.href, globalThis.location.href)
      if (target.origin !== globalThis.location.origin) return
      try {
        globalThis.sessionStorage?.setItem(
          pendingNavigationStorageKey,
          `${target.pathname}${target.search}${target.hash}`
        )
      } catch {
        // A normal reload still recovers the current page without storage.
      }
    }

    function recoverFromStaleAsset(event: Event) {
      event.preventDefault()
      let target = ""
      try {
        target =
          globalThis.sessionStorage?.getItem(pendingNavigationStorageKey) ?? ""
        globalThis.sessionStorage?.removeItem(pendingNavigationStorageKey)
      } catch {
        // Fall back to reloading the current URL below.
      }
      if (target) globalThis.location.assign(target)
      else globalThis.location.reload()
    }

    document.addEventListener("click", rememberNavigation, true)
    globalThis.addEventListener("vite:preloadError", recoverFromStaleAsset)
    return () => {
      document.removeEventListener("click", rememberNavigation, true)
      globalThis.removeEventListener("vite:preloadError", recoverFromStaleAsset)
    }
  }, [])
}

export function HydrateFallback() {
  return <AppLoadingSkeleton />
}

export function ErrorBoundary({ error }: Route.ErrorBoundaryProps) {
  let message = "页面暂时不可用"
  let details = "发生了未预期的错误，请稍后重试。"
  let stack: string | undefined

  if (isRouteErrorResponse(error)) {
    message = error.status === 404 ? "404" : "请求失败"
    details =
      error.status === 404 ? "没有找到这个页面。" : error.statusText || details
  } else if (import.meta.env.DEV && error && error instanceof Error) {
    details = error.message
    stack = error.stack
  }

  return (
    <main className="mx-auto flex min-h-svh max-w-2xl flex-col justify-center gap-3 p-6">
      <h1 className="font-heading text-2xl font-semibold">{message}</h1>
      <p className="text-muted-foreground">{details}</p>
      {stack && (
        <pre className="w-full overflow-x-auto p-4">
          <code>{stack}</code>
        </pre>
      )}
    </main>
  )
}
