import type { ComponentType, ReactNode, SVGProps } from "react"

import { cn } from "~/lib/utils"

/**
 * Keeps first-level staff page headings aligned with the compact PtYes admin
 * layout while leaving each page free to choose its own table or form body.
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
        "flex min-h-[50px] flex-col gap-3 rounded-lg border-b border-muted bg-glass px-5 py-2.5 shadow-soft backdrop-blur-[7px] sm:flex-row sm:justify-between",
        hero ? "sm:items-center" : "sm:items-center",
        className
      )}
    >
      <div className="flex min-w-0 flex-col gap-1">
        <div
          className={cn(
            "flex min-w-0 flex-wrap gap-x-3 gap-y-1",
            hero ? "items-center" : "items-baseline"
          )}
        >
          {Icon ? (
            <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-info/10 text-info [&_svg]:size-6">
              <Icon />
            </span>
          ) : null}
          <h1
            className={cn(
              "font-heading",
              hero ? "text-xl font-bold" : "text-[15px] font-semibold"
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
      {actions ? (
        <div className="flex w-full flex-wrap items-center gap-2 sm:w-auto sm:justify-end">
          {actions}
        </div>
      ) : null}
    </header>
  )
}
