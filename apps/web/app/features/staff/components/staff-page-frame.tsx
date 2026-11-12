import type { ComponentProps } from "react"

import { cn } from "~/lib/utils"

/**
 * Shared content frame for authenticated staff pages.
 *
 * Administration tables use the full space remaining beside the sidebar.
 * Keeping that rule here avoids page-specific width caps while preserving the
 * same responsive gutter as the public shell.
 */
export function StaffPageFrame({
  className,
  ...props
}: ComponentProps<"main">) {
  return (
    <main
      className={cn(
        "flex w-full min-w-0 flex-1 flex-col gap-4 px-4 pt-4 pb-8 sm:px-6 lg:gap-5",
        className
      )}
      {...props}
    />
  )
}
