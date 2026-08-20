import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

import { Button } from "~/components/ui/button"
import {
  Pagination,
  PaginationContent,
  PaginationItem,
} from "~/components/ui/pagination"
import { cn } from "~/lib/utils"

export function OffsetPagination({
  total,
  limit,
  offset,
  onOffsetChange,
  ariaLabel,
  summaryLabel,
  summaryUnit = "个",
  buttonVariant = "outline",
  className,
}: {
  total: number
  limit: number
  offset: number
  onOffsetChange: (offset: number) => void
  ariaLabel: string
  summaryLabel?: string
  summaryUnit?: string
  buttonVariant?: "ghost" | "outline"
  className?: string
}) {
  const pageCount = Math.ceil(total / limit)
  if (pageCount <= 1) {
    return summaryLabel ? (
      <p
        className={cn(
          "px-4 py-3 text-center text-xs text-muted-foreground",
          className
        )}
      >
        共 {total.toLocaleString("zh-CN")} {summaryUnit}
        {summaryLabel}
      </p>
    ) : null
  }

  const currentPage = Math.floor(offset / limit) + 1
  return (
    <Pagination className={className} aria-label={ariaLabel}>
      <PaginationContent>
        <PaginationItem>
          <Button
            type="button"
            variant={buttonVariant}
            size="sm"
            disabled={offset === 0}
            onClick={() => onOffsetChange(Math.max(0, offset - limit))}
          >
            <ChevronLeftIcon data-icon="inline-start" />
            上一页
          </Button>
        </PaginationItem>
        <PaginationItem>
          <span className="px-3 text-xs text-muted-foreground tabular-nums">
            第 {currentPage} / {pageCount} 页
          </span>
        </PaginationItem>
        <PaginationItem>
          <Button
            type="button"
            variant={buttonVariant}
            size="sm"
            disabled={offset + limit >= total}
            onClick={() => onOffsetChange(offset + limit)}
          >
            下一页
            <ChevronRightIcon data-icon="inline-end" />
          </Button>
        </PaginationItem>
      </PaginationContent>
    </Pagination>
  )
}
