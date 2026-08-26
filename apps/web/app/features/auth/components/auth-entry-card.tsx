import * as React from "react"
import { useQuery } from "@tanstack/react-query"

import { Badge } from "~/components/ui/badge"
import { Card, CardContent, CardHeader } from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { siteInfoQueryOptions } from "~/features/site/api/site.queries"
import { cn } from "~/lib/utils"

/**
 * Shared public-account entry surface.
 *
 * Auth routes render inside the chrome-less AuthShell, so the default
 * viewport centers one fixed-width stacked card (two tinted layers behind a
 * soft-shadow card) against the full viewport. `viewport="shell"` keeps the
 * previous in-shell centering for pages that still live under the AppShell
 * header, such as /restrictions.
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
      <main className="mt-4 flex min-h-[calc(100svh-var(--shell-header-height)-var(--shell-gap)-3.75rem)] items-center justify-center p-4 sm:p-6 lg:mt-6">
        <Card className={cn("w-full max-w-md", className)} {...props}>
          {children}
        </Card>
      </main>
    )
  }

  return (
    <main className="flex min-h-svh items-center justify-center p-4 sm:p-6">
      <div className="relative w-full max-w-[448px]">
        <span
          aria-hidden="true"
          className="pointer-events-none absolute inset-x-[26px] -top-6 bottom-6 rounded-3xl bg-auth-layer-2 opacity-70"
        />
        <span
          aria-hidden="true"
          className="pointer-events-none absolute inset-x-[13px] -top-[13px] bottom-[13px] rounded-3xl bg-auth-layer-1"
        />
        <Card
          className={cn(
            "relative w-full gap-4 rounded-3xl border-0 shadow-soft ring-0 [--card-spacing:--spacing(6)]",
            className
          )}
          {...props}
        >
          <AuthEntryBrand />
          {children}
        </Card>
      </div>
    </main>
  )
}

/** Brand row at the top of the stacked auth card (site name + beta pill). */
function AuthEntryBrand() {
  // Read-only cache subscription: pages that already fetch site info (login,
  // register, forgot-password) show the configured name; the rest fall back
  // to the default without issuing an extra request.
  const siteInfo = useQuery({ ...siteInfoQueryOptions, enabled: false })
  const siteName = siteInfo.data?.name ?? "PeerGo"

  return (
    <div data-slot="auth-brand" className="flex items-baseline gap-1.5 px-6">
      <span className="font-heading text-[19px] leading-none font-bold tracking-tight">
        {siteName}
      </span>
      <Badge
        variant="outline"
        className="h-4 border-transparent bg-sidebar-accent px-1 text-[9px] tracking-wider text-sidebar-accent-foreground uppercase"
      >
        beta
      </Badge>
    </div>
  )
}

/** Keeps session/config resolution visually consistent across public auth pages. */
export function AuthEntryLoading({ label }: { label: string }) {
  return (
    <AuthEntryCard role="status" aria-label={label} aria-busy="true">
      <span className="sr-only">{label}</span>
      <CardHeader className="flex flex-col gap-3 px-6">
        <Skeleton className="h-8 w-28" aria-hidden="true" />
        <Skeleton className="h-4 w-64 max-w-full" aria-hidden="true" />
      </CardHeader>
      <CardContent className="flex flex-col gap-4 px-6 pb-6">
        <Skeleton className="h-10 w-full" aria-hidden="true" />
        <Skeleton className="h-10 w-full" aria-hidden="true" />
        <Skeleton className="h-10 w-full" aria-hidden="true" />
      </CardContent>
    </AuthEntryCard>
  )
}
