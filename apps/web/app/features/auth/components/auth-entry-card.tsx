import * as React from "react"

import { Card, CardContent, CardHeader } from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { cn } from "~/lib/utils"

/**
 * Shared public-account entry surface.
 *
 * Authentication pages intentionally use one fixed-width, vertically centred
 * card so login, registration and recovery retain the same visual rhythm.
 */
export function AuthEntryCard({
  className,
  viewport = "shell",
  ...props
}: React.ComponentProps<typeof Card> & {
  viewport?: "shell" | "full"
}) {
  return (
    <main
      className={cn(
        "flex items-center justify-center p-4 sm:p-6",
        viewport === "full"
          ? "mt-4 min-h-svh lg:mt-6"
          : "mt-4 min-h-[calc(100svh-7.5rem)] lg:mt-6"
      )}
    >
      <Card
        className={cn("w-full max-w-md rounded-lg border ring-0", className)}
        {...props}
      />
    </main>
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
