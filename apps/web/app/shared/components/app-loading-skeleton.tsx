import { Skeleton } from "~/components/ui/skeleton"

export function AppLoadingSkeleton() {
  return (
    <main
      role="status"
      aria-label="加载中"
      className="grid min-h-svh grid-cols-1 lg:grid-cols-[12.5rem_minmax(0,1fr)]"
    >
      <span className="sr-only">页面加载中</span>
      <aside className="hidden border-r p-4 lg:flex lg:flex-col lg:gap-5">
        <Skeleton className="h-7 w-28 self-end" />
        <div className="flex flex-col gap-3 pt-3">
          {Array.from({ length: 8 }, (_, index) => (
            <Skeleton key={index} className="h-10 w-full" aria-hidden="true" />
          ))}
        </div>
      </aside>
      <div className="min-w-0">
        <header className="flex h-[60px] items-center justify-end border-b px-4 lg:px-6">
          <Skeleton className="h-8 w-36" aria-hidden="true" />
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
