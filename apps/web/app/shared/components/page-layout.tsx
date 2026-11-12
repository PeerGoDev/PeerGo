import type { ComponentProps, ReactNode } from "react"

import { cn } from "~/lib/utils"

export function PageLayout({ className, ...props }: ComponentProps<"main">) {
  return (
    <main
      className={cn(
        "mx-auto flex w-full max-w-[1120px] min-w-0 flex-1 flex-col gap-4 px-4 pt-4 pb-8 sm:px-6 lg:px-8",
        className
      )}
      {...props}
    />
  )
}

type PageHeaderProps = Omit<ComponentProps<"header">, "title"> & {
  title: ReactNode
  description?: ReactNode
  badge?: ReactNode
}

export function PageHeader({
  title,
  description,
  badge,
  className,
  ...props
}: PageHeaderProps) {
  return (
    <header
      className={cn("flex min-w-0 flex-col gap-1 pt-1", className)}
      {...props}
    >
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <h1 className="font-heading text-lg leading-tight font-semibold tracking-title">
          {title}
        </h1>
        {badge}
      </div>
      {description ? (
        <p className="text-xs text-muted-foreground">{description}</p>
      ) : null}
    </header>
  )
}
