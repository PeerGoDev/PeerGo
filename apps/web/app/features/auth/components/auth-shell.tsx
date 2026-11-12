import type { ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"

import { siteInfoQueryOptions } from "~/features/site/api/site.queries"
import { ThemeToggleButton } from "~/features/shell/components/theme-toggle-button"

const registrationModeLabels = {
  open: "开放注册",
  invite: "邀请注册",
  closed: "暂停注册",
} as const

/**
 * Chrome-less shell for the public auth routes (login, register, password
 * recovery and email confirmation). Desktop splits the viewport into the form
 * panel and a brand panel that stays dark in both themes; phones keep only the
 * form panel so the first field is visible without scrolling.
 */
export function AuthShell({ children }: { children: ReactNode }) {
  return (
    <div className="relative flex min-h-svh bg-background">
      <div className="relative flex min-h-svh w-full min-w-0 flex-col bg-card lg:w-[40rem] lg:shrink-0 lg:border-r">
        <ThemeToggleButton className="absolute top-3 right-3 z-10 sm:top-5 sm:right-5" />
        {children}
      </div>
      <AuthBrandPanel />
    </div>
  )
}

function AuthBrandPanel() {
  const siteInfo = useQuery(siteInfoQueryOptions)
  const site = siteInfo.data
  const siteName = site?.name ?? "PeerGo"

  return (
    <aside
      aria-label={`${siteName} 简介`}
      className="dark relative hidden flex-1 items-center overflow-hidden bg-background text-foreground lg:flex"
    >
      <span
        aria-hidden="true"
        className="pointer-events-none absolute -top-36 -right-28 size-[32.5rem] rounded-full border border-link/12"
      />
      <span
        aria-hidden="true"
        className="pointer-events-none absolute -bottom-52 -left-20 size-[32.5rem] rounded-full border border-link/8"
      />
      <span
        aria-hidden="true"
        className="pointer-events-none absolute top-52 right-60 size-1.5 rounded-full bg-link/50"
      />
      <span
        aria-hidden="true"
        className="pointer-events-none absolute top-80 right-36 size-1 rounded-full bg-link/30"
      />
      <span
        aria-hidden="true"
        className="pointer-events-none absolute bottom-64 left-44 size-1 rounded-full bg-foreground/25"
      />

      <div className="relative flex max-w-[28.75rem] flex-col gap-5 px-12 xl:px-[4.5rem]">
        <span className="text-xs font-medium tracking-[0.08em] text-link uppercase">
          {siteName}
        </span>
        <h2 className="text-[2rem] leading-[1.4] font-semibold tracking-title">
          与同好一起做种，
          <br />
          让每一份资源长存。
        </h2>
        <p className="text-sm leading-[1.9] text-muted-foreground">
          {site?.description?.trim() ||
            "上传、保种、分享，社区会记得每一份贡献。"}
        </p>
        {site ? (
          <dl className="mt-2 flex gap-7 border-t pt-5">
            <div className="flex flex-col-reverse gap-1">
              <dt className="text-2xs text-faint">在线</dt>
              <dd className="font-mono text-lg font-medium tabular-nums">
                {site.online_users.toLocaleString("zh-CN")}
              </dd>
            </div>
            <div className="flex flex-col-reverse gap-1">
              <dt className="text-2xs text-faint">注册方式</dt>
              <dd className="text-lg font-medium">
                {registrationModeLabels[site.registration_mode]}
              </dd>
            </div>
          </dl>
        ) : null}
      </div>
    </aside>
  )
}
