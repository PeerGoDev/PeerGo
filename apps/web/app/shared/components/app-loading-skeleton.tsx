import { Skeleton } from "~/components/ui/skeleton"

export function AppLoadingSkeleton() {
  return (
    <main
      role="status"
      aria-label="加载中"
      className="grid min-h-svh grid-cols-1 lg:grid-cols-[calc(var(--shell-sidebar-width)+2*var(--shell-gap))_minmax(0,1fr)]"
    >
      <span className="sr-only">页面加载中</span>
      <aside className="hidden p-(--shell-gap) lg:block">
        <div className="flex h-full flex-col gap-5 rounded-lg bg-sidebar p-4 shadow-soft">
          <Skeleton className="h-7 w-28" />
          <div className="flex flex-col gap-3 pt-3">
            {Array.from({ length: 8 }, (_, index) => (
              <Skeleton
                key={index}
                className="h-10 w-full"
                aria-hidden="true"
              />
            ))}
          </div>
        </div>
      </aside>
      <div className="min-w-0">
        <header className="px-4 pt-(--shell-gap) lg:px-6">
          <div className="mx-auto flex h-(--shell-header-height) w-full max-w-[1200px] items-center justify-end rounded-lg bg-glass px-4 shadow-soft backdrop-blur-[7px]">
            <Skeleton className="h-8 w-36" aria-hidden="true" />
          </div>
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
          <Skeleton className="h-8 w-48" aria-hidden="true" />
          <Skeleton className="h-4 w-72 max-w-full" aria-hidden="true" />
        </div>
        <Skeleton className="h-9 w-24" aria-hidden="true" />
      </div>
      <Skeleton className="h-28 w-full rounded-lg" aria-hidden="true" />
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton
            key={index}
            className="h-40 w-full rounded-lg"
            aria-hidden="true"
          />
        ))}
      </div>
    </>
  )

  if (embedded) {
    return (
      <section className="mx-auto flex w-full max-w-[1248px] flex-col gap-4 p-4 lg:p-6">
        {content}
      </section>
    )
  }

  return (
    <main
      role="status"
      aria-label="页面加载中"
      className="mx-auto flex w-full max-w-[1248px] flex-col gap-4 p-4 lg:p-6"
    >
      <span className="sr-only">页面加载中</span>
      {content}
    </main>
  )
}
