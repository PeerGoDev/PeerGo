import type { ComponentType, ReactNode, SVGProps } from "react"

import { cn } from "~/lib/utils"

/**
 * First-level staff page heading: a flat title row with optional meta,
 * description and actions, leaving each page free to choose its own table or
 * form body.
 */
export function StaffPageHeader({
  title,
  description,
  meta,
  actions,
  icon: Icon,
  variant = "compact",
  className,
  descriptionClassName,
}: {
  title: string
  description?: string
  meta?: ReactNode
  actions?: ReactNode
  icon?: ComponentType<SVGProps<SVGSVGElement>>
  variant?: "compact" | "hero"
  className?: string
  descriptionClassName?: string
}) {
  const hero = variant === "hero"

  return (
    <header
      className={cn(
        "flex min-w-0 flex-col gap-3 pt-1 sm:flex-row sm:items-center sm:justify-between",
        className
      )}
    >
      <div className="flex min-w-0 items-center gap-3">
        {Icon ? (
          <span className="flex size-10 shrink-0 items-center justify-center rounded-lg border border-primary/20 bg-primary/10 text-link [&_svg]:size-5">
            <Icon />
          </span>
        ) : null}
        <div className="flex min-w-0 flex-col gap-1">
          <div
            className={cn(
              "flex min-w-0 flex-wrap gap-x-3 gap-y-1",
              hero ? "items-center" : "items-baseline"
            )}
          >
            <h1
              className={cn(
                "font-heading font-semibold tracking-title",
                hero ? "text-xl" : "text-lg"
              )}
            >
              {title}
            </h1>
            {meta ? (
              <span className="text-sm text-muted-foreground">{meta}</span>
            ) : null}
          </div>
          {description ? (
            <p
              className={cn(
                "text-xs text-muted-foreground",
                descriptionClassName
              )}
            >
              {description}
            </p>
          ) : null}
        </div>
      </div>
      {actions ? (
        <div className="flex w-full flex-wrap items-center gap-2 sm:w-auto sm:justify-end">
          {actions}
        </div>
      ) : null}
    </header>
  )
}
