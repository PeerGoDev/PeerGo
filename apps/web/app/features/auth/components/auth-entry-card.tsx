import * as React from "react"
import { useQuery } from "@tanstack/react-query"

import { Card, CardContent, CardHeader } from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { siteInfoQueryOptions } from "~/features/site/api/site.queries"
import { cn } from "~/lib/utils"

/**
 * Shared public-account entry surface.
 *
 * Auth routes render inside the split AuthShell, so the default viewport
 * centers a 384px form column inside the form panel: the page's card slots
 * keep their structure but lose the card chrome. `viewport="shell"` keeps a
 * regular centered card for pages that still live under the AppShell header,
 * such as /restrictions.
 */
export function AuthEntryCard({
  className,
  viewport = "auth",
  children,
  ...props
}: React.ComponentProps<typeof Card> & {
  viewport?: "auth" | "shell"
}) {
  if (viewport === "shell") {
    return (
      <main className="flex min-h-[calc(100svh-var(--shell-header-height)-3.75rem)] items-center justify-center p-4 sm:p-6 lg:mt-4">
        <Card
          className={cn(
            "w-full max-w-md [&_p>a]:text-link [&_p>a]:underline-offset-4 [&_p>a]:hover:underline",
            className
          )}
          {...props}
        >
          {children}
        </Card>
      </main>
    )
  }

  return (
    <main className="flex flex-1 items-start justify-center px-5 pt-16 pb-10 sm:items-center sm:px-10 sm:py-20">
      <Card
        className={cn(
          "w-full max-w-sm gap-4 overflow-visible rounded-none border-0 bg-transparent py-0 shadow-none [--card-spacing:0px]",
          "*:data-[slot=card-content]:px-0 *:data-[slot=card-footer]:px-0 *:data-[slot=card-footer]:pb-0 *:data-[slot=card-header]:px-0",
          "[&_p>a]:text-link [&_p>a]:underline-offset-4 [&_p>a]:hover:underline",
          className
        )}
        {...props}
      >
        <AuthEntryBrand />
        {children}
      </Card>
    </main>
  )
}

/** Brand row above the auth form (site name + beta tag). */
function AuthEntryBrand() {
  // Read-only cache subscription: pages that already fetch site info (login,
  // register, forgot-password) show the configured name; the rest fall back
  // to the default without issuing an extra request.
  const siteInfo = useQuery({ ...siteInfoQueryOptions, enabled: false })
  const siteName = siteInfo.data?.name ?? "PeerGo"

  return (
    <div data-slot="auth-brand" className="mb-2 flex items-center gap-2">
      <span className="font-heading text-xl leading-none font-bold tracking-title">
        {siteName}
      </span>
      <span className="rounded-sm border border-primary/40 px-1 py-px text-[0.5625rem] leading-none font-semibold tracking-[0.14em] text-primary uppercase">
        beta
      </span>
    </div>
  )
}

/** Keeps session/config resolution visually consistent across public auth pages. */
export function AuthEntryLoading({ label }: { label: string }) {
  return (
    <AuthEntryCard role="status" aria-label={label} aria-busy="true">
      <span className="sr-only">{label}</span>
      <CardHeader className="flex flex-col gap-3">
        <Skeleton className="h-7 w-24" aria-hidden="true" />
        <Skeleton className="h-4 w-64 max-w-full" aria-hidden="true" />
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Skeleton className="h-10 w-full rounded-lg" aria-hidden="true" />
        <Skeleton className="h-10 w-full rounded-lg" aria-hidden="true" />
        <Skeleton className="h-10 w-full rounded-lg" aria-hidden="true" />
      </CardContent>
    </AuthEntryCard>
  )
}
