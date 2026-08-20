import {
  Links,
  Meta,
  Outlet,
  Scripts,
  ScrollRestoration,
  isRouteErrorResponse,
  useLocation,
} from "react-router"

import type { Route } from "./+types/root"
import "~/shared/polyfills/crypto-random-uuid"
import { AppProviders } from "~/shared/providers/app-providers"
import { AppShell } from "~/features/shell/components/app-shell"
import { StaffShell } from "~/features/staff/components/staff-shell"
import { Spinner } from "~/components/ui/spinner"
import "./app.css"

export const meta: Route.MetaFunction = () => [
  { title: "PeerGo" },
  {
    name: "description",
    content: "私有分享社区",
  },
]

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
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
  const shell = location.pathname.startsWith("/staff") ? StaffShell : AppShell
  const Shell = shell

  return (
    <AppProviders>
      <Shell>
        <Outlet />
      </Shell>
    </AppProviders>
  )
}

export function HydrateFallback() {
  return (
    <main className="flex min-h-svh items-center justify-center">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Spinner className="size-5" />
        <span>页面加载中</span>
      </div>
    </main>
  )
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
