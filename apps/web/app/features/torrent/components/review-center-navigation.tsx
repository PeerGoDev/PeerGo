import { Link, useLocation } from "react-router"
import { ClipboardListIcon, UploadIcon } from "lucide-react"

import { cn } from "~/lib/utils"

const reviewerItems = [
  {
    label: "审核队列",
    to: "/review/queue",
    icon: ClipboardListIcon,
  },
  {
    label: "我的上传",
    to: "/account/submissions",
    icon: UploadIcon,
  },
] as const

const memberItems = [
  {
    label: "我的上传",
    to: "/account/submissions",
    icon: UploadIcon,
  },
] as const

export function ReviewCenterNavigation({
  canReview = true,
}: {
  canReview?: boolean
}) {
  const location = useLocation()
  const items = canReview ? reviewerItems : memberItems

  return (
    <nav
      aria-label="审核中心"
      className={cn(
        "grid h-9 w-full max-w-lg gap-0.5 rounded-lg border bg-muted p-[3px] text-muted-foreground",
        canReview ? "grid-cols-2" : "grid-cols-1"
      )}
    >
      {items.map((item) => {
        const active = location.pathname === item.to
        const className = cn(
          "flex items-center justify-center gap-2 rounded-md border border-transparent px-3 text-sm font-medium whitespace-nowrap transition-colors outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/30",
          active && "border-border bg-segment text-foreground shadow-xs"
        )
        return (
          <Link
            key={item.label}
            to={item.to}
            aria-current={active ? "page" : undefined}
            className={className}
          >
            <item.icon className="size-4" />
            {item.label}
          </Link>
        )
      })}
    </nav>
  )
}
