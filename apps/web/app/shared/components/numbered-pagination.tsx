import * as React from "react"
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

import { Button } from "~/components/ui/button"
import { Input } from "~/components/ui/input"
import { cn } from "~/lib/utils"

type PaginationWindowItem = number | "gap"

/**
 * Pages shown between the previous/next controls: the first and last page,
 * the current page with its neighbours, and an ellipsis for skipped runs.
 * A gap of exactly one page is rendered as that page instead of "…".
 */
export function paginationWindow(
  currentPage: number,
  totalPages: number
): PaginationWindowItem[] {
  if (totalPages < 1) return []
  const current = Math.min(Math.max(1, currentPage), totalPages)
  const pages = new Set<number>([1, totalPages])
  const start = Math.max(1, Math.min(current - 1, totalPages - 2))
  const end = Math.min(totalPages, Math.max(current + 1, 3))
  for (let page = start; page <= end; page += 1) pages.add(page)

  const items: PaginationWindowItem[] = []
  let previous: number | undefined
  for (const page of [...pages].sort((left, right) => left - right)) {
    if (previous !== undefined && page - previous === 2) items.push(page - 1)
    else if (previous !== undefined && page - previous > 2) items.push("gap")
    items.push(page)
    previous = page
  }
  return items
}

export function NumberedPagination({
  currentPage,
  totalPages,
  onPageChange,
  ariaLabel,
  summary,
  pageSize,
  pageSizeOptions,
  onPageSizeChange,
  showJump = false,
  compact = false,
  buttonVariant = "outline",
  className,
}: {
  currentPage: number
  totalPages: number
  onPageChange: (page: number) => void
  ariaLabel: string
  summary?: React.ReactNode
  pageSize?: number
  pageSizeOptions?: readonly number[]
  onPageSizeChange?: (pageSize: number) => void
  showJump?: boolean
  /** Replaces the numbered window with "第 X / Y 页" at every width. */
  compact?: boolean
  buttonVariant?: "ghost" | "outline"
  className?: string
}) {
  const [jumpPage, setJumpPage] = React.useState("")
  const pages = paginationWindow(currentPage, totalPages)

  function jumpToRequestedPage() {
    const requestedPage = Number.parseInt(jumpPage, 10)
    if (
      !Number.isSafeInteger(requestedPage) ||
      requestedPage < 1 ||
      requestedPage > totalPages
    ) {
      return
    }
    onPageChange(requestedPage)
    setJumpPage("")
  }

  return (
    <nav
      aria-label={ariaLabel}
      className={cn(
        "flex flex-wrap items-center justify-between gap-x-4 gap-y-2.5",
        className
      )}
    >
      {summary ? (
        <p className="text-xs text-faint">{summary}</p>
      ) : (
        <span aria-hidden="true" />
      )}
      <div
        className={cn(
          "flex flex-wrap items-center justify-end gap-1.5",
          !compact && "max-sm:w-full max-sm:justify-between"
        )}
      >
        <Button
          type="button"
          variant={buttonVariant}
          size="xs"
          className="h-8 gap-0.5 px-2.5 sm:h-7"
          disabled={currentPage <= 1}
          onClick={() => onPageChange(currentPage - 1)}
        >
          <ChevronLeftIcon data-icon="inline-start" className="sm:hidden" />
          上一页
        </Button>
        <ol
          className={cn(
            "flex items-center gap-1.5",
            compact ? "hidden" : "max-sm:hidden"
          )}
        >
          {pages.map((page, index) =>
            page === "gap" ? (
              <li
                key={`gap-${index}`}
                aria-hidden="true"
                className="px-0.5 text-xs text-faint"
              >
                …
              </li>
            ) : (
              <li key={page}>
                <Button
                  type="button"
                  variant={page === currentPage ? "default" : buttonVariant}
                  size="xs"
                  className={cn(
                    "h-7 min-w-7 px-1.5 font-mono tabular-nums",
                    page !== currentPage && "text-muted-foreground"
                  )}
                  aria-current={page === currentPage ? "page" : undefined}
                  aria-label={`第 ${page} 页`}
                  onClick={() => {
                    if (page !== currentPage) onPageChange(page)
                  }}
                >
                  {page}
                </Button>
              </li>
            )
          )}
        </ol>
        <span
          className={cn(
            "px-1 text-xs text-muted-foreground tabular-nums",
            !compact && "sm:hidden"
          )}
        >
          第 {currentPage} / {totalPages} 页
        </span>
        <Button
          type="button"
          variant={buttonVariant}
          size="xs"
          className="h-8 gap-0.5 px-2.5 sm:h-7"
          disabled={currentPage >= totalPages}
          onClick={() => onPageChange(currentPage + 1)}
        >
          下一页
          <ChevronRightIcon data-icon="inline-end" className="sm:hidden" />
        </Button>
        {pageSize && pageSizeOptions && onPageSizeChange ? (
          <label className="flex items-center gap-1.5 text-xs text-faint sm:ml-2">
            每页
            <select
              value={pageSize}
              onChange={(event) =>
                onPageSizeChange(Number.parseInt(event.target.value, 10))
              }
              aria-label="每页条数"
              className="h-8 rounded-md border border-input bg-card px-1.5 font-mono text-base text-foreground outline-none focus-visible:border-primary focus-visible:ring-3 focus-visible:ring-primary/20 sm:h-7 sm:text-xs"
            >
              {pageSizeOptions.map((option) => (
                <option key={option} value={option}>
                  {option}
                </option>
              ))}
            </select>
            条
          </label>
        ) : null}
        {showJump ? (
          <span className="flex items-center gap-1.5 text-xs text-faint">
            跳至
            <Input
              type="number"
              min={1}
              max={totalPages}
              value={jumpPage}
              onChange={(event) => setJumpPage(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") jumpToRequestedPage()
              }}
              placeholder="页码"
              aria-label="跳转页码"
              className="h-8 w-16 px-2 text-center font-mono sm:h-7 sm:w-14 md:text-xs"
            />
            页
            <Button
              type="button"
              variant={buttonVariant}
              size="xs"
              className="h-8 px-2.5 sm:h-7"
              onClick={jumpToRequestedPage}
            >
              跳转
            </Button>
          </span>
        ) : null}
      </div>
    </nav>
  )
}
