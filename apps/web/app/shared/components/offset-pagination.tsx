import { cn } from "~/lib/utils"
import { NumberedPagination } from "~/shared/components/numbered-pagination"

export function OffsetPagination({
  total,
  limit,
  offset,
  onOffsetChange,
  ariaLabel,
  summaryLabel,
  summaryUnit = "个",
  buttonVariant = "outline",
  compact = false,
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
  compact?: boolean
  className?: string
}) {
  const pageCount = Math.ceil(total / limit)
  const summary = summaryLabel
    ? `共 ${total.toLocaleString("zh-CN")} ${summaryUnit}${summaryLabel}`
    : undefined
  if (pageCount <= 1) {
    return summary ? (
      <p
        className={cn(
          "px-4 py-3 text-center text-xs text-muted-foreground",
          className
        )}
      >
        {summary}
      </p>
    ) : null
  }

  return (
    <NumberedPagination
      currentPage={Math.floor(offset / limit) + 1}
      totalPages={pageCount}
      onPageChange={(page) => onOffsetChange(Math.max(0, (page - 1) * limit))}
      ariaLabel={ariaLabel}
      summary={summary}
      buttonVariant={buttonVariant}
      compact={compact}
      className={className}
    />
  )
}
