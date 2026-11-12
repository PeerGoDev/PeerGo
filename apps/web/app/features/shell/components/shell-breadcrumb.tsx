import { ChevronRightIcon } from "lucide-react"
import { Link } from "react-router"

import type { BreadcrumbItem } from "~/features/shell/model/breadcrumbs"
import { cn } from "~/lib/utils"

export function ShellBreadcrumb({
  items,
  className,
}: {
  items: BreadcrumbItem[]
  className?: string
}) {
  return (
    <nav
      aria-label="当前位置"
      className={cn(
        "flex h-6 min-w-0 items-center rounded-sm border px-2 text-xs font-medium text-muted-foreground",
        className
      )}
    >
      <ol className="flex min-w-0 items-center gap-1.5">
        {items.map((item, index) => {
          const current = index === items.length - 1
          return (
            <li
              key={`${index}:${item.label}`}
              className={cn(
                "flex items-center gap-1.5",
                current ? "min-w-0" : "shrink-0"
              )}
            >
              {index > 0 ? (
                <ChevronRightIcon
                  aria-hidden="true"
                  className="size-3 shrink-0 text-faint"
                />
              ) : null}
              {item.to && !current ? (
                <Link
                  to={item.to}
                  className="rounded-sm transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/40"
                >
                  {item.label}
                </Link>
              ) : (
                <span
                  aria-current={current ? "page" : undefined}
                  className={cn(current && "truncate")}
                >
                  {item.label}
                </span>
              )}
            </li>
          )
        })}
      </ol>
    </nav>
  )
}
