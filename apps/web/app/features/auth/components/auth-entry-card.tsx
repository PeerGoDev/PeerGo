import * as React from "react"

import { Card } from "~/components/ui/card"
import { Spinner } from "~/components/ui/spinner"
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
    <main className="mt-4 flex min-h-[calc(100svh-7.5rem)] items-center justify-center lg:mt-6">
      <div role="status" aria-label={label}>
        <Spinner className="size-8" />
      </div>
    </main>
  )
}
