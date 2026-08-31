import type { ComponentProps, ReactNode } from "react"

import { cn } from "~/lib/utils"

export function PageLayout({ className, ...props }: ComponentProps<"main">) {
  return (
    <main
      className={cn(
        "mx-auto flex w-full max-w-[1248px] min-w-0 flex-1 flex-col gap-4 p-4 sm:gap-5 lg:p-6",
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
    <header className={cn("flex flex-col gap-1", className)} {...props}>
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="font-heading text-2xl font-bold sm:text-3xl">{title}</h1>
        {badge}
      </div>
      {description ? (
        <p className="text-sm text-muted-foreground">{description}</p>
      ) : null}
    </header>
  )
}
