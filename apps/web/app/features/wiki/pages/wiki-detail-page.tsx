import { Link, useParams } from "react-router"
import {
  ArrowLeftIcon,
  BookOpenIcon,
  CircleAlertIcon,
  Clock3Icon,
  PencilIcon,
  RefreshCwIcon,
  UsersIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Separator } from "~/components/ui/separator"
import { Skeleton } from "~/components/ui/skeleton"
import {
  isWikiSlug,
  useWikiPage,
  useWikiPageList,
} from "~/features/wiki/api/wiki.queries"
import {
  extractWikiHeadings,
  WikiMarkdown,
} from "~/features/wiki/components/wiki-markdown"
import { cn } from "~/lib/utils"
import { PageLayout } from "~/shared/components/page-layout"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

export function WikiDetailPage() {
  const { slug = "" } = useParams()
  const page = useWikiPage(slug, isWikiSlug(slug))
  const navigation = useWikiPageList("", 100, 0)

  if (page.isPending) return <WikiDetailSkeleton />

  if (page.isError || !page.data) {
    return (
      <PageLayout>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>无法打开这篇 Wiki</AlertTitle>
          <AlertDescription>
            页面可能不存在，或当前账号没有成员文档的阅读权限。
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void page.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
        <Link to="/wiki" className={buttonVariants({ variant: "outline" })}>
          <ArrowLeftIcon data-icon="inline-start" />
          返回 Wiki
        </Link>
      </PageLayout>
    )
  }

  const wiki = page.data
  const headings = extractWikiHeadings(wiki.body)

  return (
    <PageLayout className="max-w-[1440px] gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Link
          to="/wiki"
          className={buttonVariants({ variant: "ghost", size: "sm" })}
        >
          <ArrowLeftIcon data-icon="inline-start" />
          Wiki 文档
        </Link>
        {wiki.can_edit ? (
          <Link
            to={`/wiki/${encodeURIComponent(wiki.slug)}/edit`}
            className={buttonVariants()}
          >
            <PencilIcon data-icon="inline-start" />
            编辑文档
          </Link>
        ) : null}
      </div>

      <header className="flex flex-col gap-3 rounded-xl border bg-card p-4 shadow-card sm:p-6">
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="primary">
            {wiki.visibility === "members" ? "成员文档" : "公开文档"}
          </Badge>
          {wiki.migrated ? <Badge variant="secondary">旧版已迁移</Badge> : null}
          <Badge variant="secondary">版本 {wiki.revision_number}</Badge>
        </div>
        <div className="flex flex-col gap-1.5">
          <h1 className="font-heading text-xl font-semibold tracking-title break-words md:text-2xl">
            {wiki.title}
          </h1>
          {wiki.summary ? (
            <p className="max-w-4xl text-md leading-6 text-muted-foreground">
              {wiki.summary}
            </p>
          ) : null}
        </div>
        <div className="flex flex-wrap gap-x-5 gap-y-1.5 text-xs text-faint">
          <span className="flex items-center gap-1.5">
            <Clock3Icon className="size-3.5" />
            {wiki.updater.display_name} 更新于{" "}
            {formatCompactDateTime(wiki.updated_at)}
          </span>
          <span className="flex items-center gap-1.5">
            <UsersIcon className="size-3.5" />
            创建者 {wiki.creator.display_name}
            {wiki.editors.length > 0
              ? ` · 协作者 ${wiki.editors.map((editor) => editor.display_name).join("、")}`
              : ""}
          </span>
        </div>
      </header>

      <div className="grid items-start gap-4 lg:grid-cols-[220px_minmax(0,1fr)_220px] lg:gap-6">
        <Card
          size="sm"
          className="hidden lg:sticky lg:top-[calc(var(--shell-header-height)+1rem)] lg:flex"
        >
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <BookOpenIcon />
              Wiki 导航
            </CardTitle>
            <CardDescription>
              {navigation.data?.total ?? 0} 篇可见文档
            </CardDescription>
          </CardHeader>
          <CardContent className="px-2">
            <nav aria-label="Wiki 页面" className="flex flex-col gap-0.5">
              {navigation.data?.items.map((item) => (
                <Link
                  key={item.id}
                  to={`/wiki/${encodeURIComponent(item.slug)}`}
                  aria-current={item.slug === wiki.slug ? "page" : undefined}
                  className={cn(
                    "rounded-md px-2.5 py-1.5 text-sm leading-5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
                    item.slug === wiki.slug &&
                      "bg-accent font-medium text-foreground"
                  )}
                >
                  {item.title}
                </Link>
              ))}
            </nav>
          </CardContent>
        </Card>

        <Card className="min-w-0">
          <CardContent className="md:px-8">
            <WikiMarkdown body={wiki.body} />
          </CardContent>
        </Card>

        {headings.length > 0 ? (
          <Card
            size="sm"
            className="hidden lg:sticky lg:top-[calc(var(--shell-header-height)+1rem)] lg:flex"
          >
            <CardHeader>
              <CardTitle>本页目录</CardTitle>
            </CardHeader>
            <CardContent className="px-2">
              <nav
                aria-label="本页目录"
                className="flex max-h-[70vh] flex-col gap-0.5 overflow-y-auto"
              >
                {headings.map((heading) => (
                  <a
                    key={heading.id}
                    href={`#${encodeURIComponent(heading.id)}`}
                    className={cn(
                      "rounded-md py-1 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
                      heading.level === 1 && "px-2.5 font-medium",
                      heading.level === 2 && "pr-2.5 pl-5",
                      heading.level === 3 && "pr-2.5 pl-8 text-xs"
                    )}
                  >
                    {heading.text}
                  </a>
                ))}
              </nav>
            </CardContent>
          </Card>
        ) : null}
      </div>

      <Separator />
      <p className="text-center text-xs text-faint">
        创建于 {formatCompactDateTime(wiki.created_at)} · 当前保留最近 50
        个有效修订
      </p>
    </PageLayout>
  )
}

function WikiDetailSkeleton() {
  return (
    <PageLayout className="max-w-[1440px]" aria-busy="true">
      <Skeleton className="h-9 w-28" />
      <Card>
        <CardHeader>
          <Skeleton className="h-9 w-2/3" />
          <Skeleton className="h-5 w-full" />
        </CardHeader>
      </Card>
      <div className="grid gap-6 lg:grid-cols-[220px_minmax(0,1fr)_220px]">
        <Skeleton className="hidden h-72 lg:block" />
        <Skeleton className="h-[520px]" />
        <Skeleton className="hidden h-72 lg:block" />
      </div>
    </PageLayout>
  )
}
