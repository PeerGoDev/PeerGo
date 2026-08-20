import type { ComponentProps } from "react"

import { cn } from "~/lib/utils"

/**
 * Shared content frame for authenticated staff pages.
 *
 * PtYes lets administration tables use the full space remaining beside the
 * 200px sidebar. Keeping that rule here avoids page-specific width caps while
 * preserving the same 16px/24px responsive gutter used by the public shell.
 */
export function StaffPageFrame({
  className,
  ...props
}: ComponentProps<"main">) {
  return (
    <main
      className={cn("flex w-full flex-1 flex-col gap-6 p-4 lg:p-6", className)}
      {...props}
    />
  )
}
