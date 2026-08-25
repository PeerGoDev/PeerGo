import * as React from "react"
import { Link } from "react-router"
import {
  ArrowRightIcon,
  BookOpenIcon,
  CircleAlertIcon,
  RefreshCwIcon,
  SearchIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Field, FieldLabel } from "~/components/ui/field"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "~/components/ui/input-group"
import { Skeleton } from "~/components/ui/skeleton"
import { useWikiPageList } from "~/features/wiki/api/wiki.queries"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

export function WikiListPage() {
  const [draftQuery, setDraftQuery] = React.useState("")
  const [query, setQuery] = React.useState("")
  const pages = useWikiPageList(query)

  return (
    <PageLayout className="gap-6">
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            <span className="flex size-11 items-center justify-center rounded-xl bg-primary/10 text-primary">
              <BookOpenIcon className="size-6" />
            </span>
            Wiki 文档
          </span>
        }
        description="站点规则、发种规范与使用指南。登录成员可阅读成员文档，被指派的协作者可以维护正文。"
      />

      <form
        onSubmit={(event) => {
          event.preventDefault()
          setQuery(draftQuery.trim())
        }}
      >
        <Field>
          <FieldLabel htmlFor="wiki-search" className="sr-only">
            搜索 Wiki
          </FieldLabel>
          <InputGroup className="max-w-3xl">
            <InputGroupAddon>
              <SearchIcon />
            </InputGroupAddon>
            <InputGroupInput
              id="wiki-search"
              value={draftQuery}
              onChange={(event) => setDraftQuery(event.target.value)}
              placeholder="搜索标题、摘要或正文"
              maxLength={100}
            />
            <InputGroupAddon align="inline-end">
              <InputGroupButton type="submit">搜索</InputGroupButton>
            </InputGroupAddon>
          </InputGroup>
        </Field>
      </form>

      {pages.isPending ? <WikiListSkeleton /> : null}

      {pages.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>Wiki 暂时无法读取</AlertTitle>
          <AlertDescription>
            其他站点功能不受影响，请稍后重试。
          </AlertDescription>
          <AlertAction>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => void pages.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      ) : null}

      {pages.data ? (
        pages.data.items.length > 0 ? (
          <section aria-label="Wiki 文档列表" className="flex flex-col gap-4">
            <ol className="grid gap-4 md:grid-cols-2">
              {pages.data.items.map((page) => (
                <li key={page.id}>
                  <Link
                    to={`/wiki/${encodeURIComponent(page.slug)}`}
                    className="group block h-full rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <Card className="h-full transition-colors group-hover:border-primary/70">
                      <CardHeader>
                        <div className="flex items-start justify-between gap-3">
                          <CardTitle className="text-xl break-words">
                            {page.title}
                          </CardTitle>
                          <ArrowRightIcon className="size-5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-1 group-hover:text-primary" />
                        </div>
                        <CardDescription className="line-clamp-3 leading-6">
                          {page.summary || "站点 Wiki 文档"}
                        </CardDescription>
                      </CardHeader>
                      <CardContent className="mt-auto flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                        <Badge variant="outline">
                          {page.visibility === "members"
                            ? "成员文档"
                            : "公开文档"}
                        </Badge>
                        {page.can_edit ? <Badge>可协作编辑</Badge> : null}
                        <span className="ml-auto">
                          更新于 {formatCompactDateTime(page.updated_at)}
                        </span>
                      </CardContent>
                    </Card>
                  </Link>
                </li>
              ))}
            </ol>
            <p className="text-center text-sm text-muted-foreground">
              共 {pages.data.total} 篇文档
              {query ? `，搜索“${query}”` : ""}
            </p>
          </section>
        ) : (
          <Empty className="border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <BookOpenIcon />
              </EmptyMedia>
              <EmptyTitle>
                {query ? "没有匹配的文档" : "暂无 Wiki 文档"}
              </EmptyTitle>
              <EmptyDescription>
                {query
                  ? "换一个关键词，或清除搜索条件。"
                  : "管理员可以在后台创建第一篇文档。"}
              </EmptyDescription>
            </EmptyHeader>
            {query ? (
              <EmptyContent>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setDraftQuery("")
                    setQuery("")
                  }}
                >
                  清除搜索
                </Button>
              </EmptyContent>
            ) : null}
          </Empty>
        )
      ) : null}
    </PageLayout>
  )
}

function WikiListSkeleton() {
  return (
    <div className="grid gap-4 md:grid-cols-2" aria-busy="true">
      {Array.from({ length: 4 }, (_, index) => (
        <Card key={index}>
          <CardHeader>
            <Skeleton className="h-6 w-2/3" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-4/5" />
          </CardHeader>
          <CardContent className="flex gap-2">
            <Skeleton className="h-5 w-20" />
            <Skeleton className="h-5 w-28" />
          </CardContent>
        </Card>
      ))}
    </div>
  )
}
