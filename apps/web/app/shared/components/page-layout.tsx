import type { ComponentProps, ReactNode } from "react"

import { cn } from "~/lib/utils"

export function PageLayout({ className, ...props }: ComponentProps<"main">) {
  return (
    <main
      className={cn(
        "mx-auto flex w-full max-w-[1248px] flex-1 flex-col gap-5 p-4 lg:p-6",
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
      className={cn(
        "flex min-h-[50px] flex-wrap items-center gap-x-3.5 gap-y-1 rounded-lg border-b border-muted bg-glass px-5 py-2.5 shadow-soft backdrop-blur-[7px]",
        className
      )}
      {...props}
    >
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="font-heading text-[15px] font-semibold">{title}</h1>
        {badge}
      </div>
      {description ? (
        <p className="text-xs text-muted-foreground sm:border-l sm:border-border sm:pl-3.5">
          {description}
        </p>
      ) : null}
    </header>
  )
}
