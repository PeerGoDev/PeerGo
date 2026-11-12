import { Skeleton } from "~/components/ui/skeleton"

export function AppLoadingSkeleton() {
  return (
    <main
      role="status"
      aria-label="加载中"
      className="grid min-h-svh grid-cols-1 lg:grid-cols-[var(--shell-sidebar-width)_minmax(0,1fr)]"
    >
      <span className="sr-only">页面加载中</span>
      <aside className="hidden flex-col gap-2 border-r border-sidebar-border bg-sidebar px-3 lg:flex">
        <div className="flex h-16 items-center px-2">
          <Skeleton className="h-5 w-24" />
        </div>
        <Skeleton className="mx-2 mt-2 h-3 w-16" aria-hidden="true" />
        {Array.from({ length: 9 }, (_, index) => (
          <Skeleton
            key={index}
            className="h-9 w-full rounded-md"
            aria-hidden="true"
          />
        ))}
      </aside>
      <div className="min-w-0">
        <header className="flex h-(--shell-header-height) items-center justify-end gap-3 border-b px-4">
          <Skeleton className="h-7 w-24 rounded-full" aria-hidden="true" />
          <Skeleton className="size-8 rounded-full" aria-hidden="true" />
        </header>
        <RouteLoadingSkeleton embedded />
      </div>
    </main>
  )
}

export function RouteLoadingSkeleton({
  embedded = false,
}: {
  embedded?: boolean
}) {
  const content = (
    <>
      <div className="flex items-center justify-between gap-4">
        <div className="flex flex-col gap-2">
          <Skeleton className="h-6 w-40" aria-hidden="true" />
          <Skeleton className="h-3.5 w-64 max-w-full" aria-hidden="true" />
        </div>
        <Skeleton className="h-9 w-24 rounded-lg" aria-hidden="true" />
      </div>
      <Skeleton className="h-28 w-full rounded-xl" aria-hidden="true" />
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton
            key={index}
            className="h-40 w-full rounded-xl"
            aria-hidden="true"
          />
        ))}
      </div>
    </>
  )

  if (embedded) {
    return (
      <section className="mx-auto flex w-full max-w-[1120px] flex-col gap-4 px-4 pt-4 pb-8 sm:px-6 lg:px-8">
        {content}
      </section>
    )
  }

  return (
    <main
      role="status"
      aria-label="页面加载中"
      className="mx-auto flex w-full max-w-[1120px] flex-col gap-4 px-4 pt-4 pb-8 sm:px-6 lg:px-8"
    >
      <span className="sr-only">页面加载中</span>
      {content}
    </main>
  )
}
