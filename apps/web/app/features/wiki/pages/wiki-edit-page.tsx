import * as React from "react"
import { Link, useNavigate, useParams } from "react-router"
import {
  ArrowLeftIcon,
  CircleAlertIcon,
  PencilIcon,
  SaveIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { Textarea } from "~/components/ui/textarea"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  isWikiSlug,
  useUpdateAssignedWikiPage,
  useWikiPage,
} from "~/features/wiki/api/wiki.queries"
import { WikiMarkdownEditor } from "~/features/wiki/components/wiki-markdown-editor"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"

export function WikiEditPage() {
  const { slug = "" } = useParams()
  const navigate = useNavigate()
  const session = useWebSession()
  const page = useWikiPage(slug, isWikiSlug(slug))
  const update = useUpdateAssignedWikiPage()
  const [form, setForm] = React.useState({
    title: "",
    summary: "",
    body: "",
    reason: "",
  })
  const [loadedVersion, setLoadedVersion] = React.useState<number>()
  const [validationError, setValidationError] = React.useState("")

  React.useEffect(() => {
    if (!page.data || loadedVersion === page.data.version) return
    setForm({
      title: page.data.title,
      summary: page.data.summary,
      body: page.data.body,
      reason: "",
    })
    setLoadedVersion(page.data.version)
  }, [loadedVersion, page.data])

  if (page.isPending || session.isPending) {
    return <WikiEditSkeleton />
  }

  if (page.isError || !page.data) {
    return (
      <WikiEditProblem
        title="无法读取 Wiki"
        description="页面可能不存在或当前账号没有阅读权限。"
      />
    )
  }

  if (!session.data || !page.data.can_edit) {
    return (
      <WikiEditProblem
        title="没有编辑权限"
        description="只有页面创建者、被指派协作者或具有 Wiki 管理权限的成员可以修改正文。"
      />
    )
  }

  const wiki = page.data

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    const title = form.title.trim()
    const body = form.body.trim()
    if (!title || !body) {
      setValidationError("标题和正文不能为空。")
      return
    }
    setValidationError("")
    try {
      const saved = await update.mutateAsync({
        csrfToken: session.data!.csrf_token,
        pageId: wiki.id,
        slug: wiki.slug,
        body: {
          title,
          summary: form.summary.trim(),
          body,
          expected_version: wiki.version,
          reason: form.reason.trim(),
        },
      })
      navigate(`/wiki/${encodeURIComponent(saved.slug)}`, { replace: true })
    } catch {
      // Mutation state renders a stable error below and keeps the draft intact.
    }
  }

  return (
    <PageLayout className="max-w-5xl gap-6">
      <Link
        to={`/wiki/${encodeURIComponent(wiki.slug)}`}
        className={buttonVariants({ variant: "ghost", size: "sm" })}
      >
        <ArrowLeftIcon data-icon="inline-start" />
        返回文档
      </Link>
      <PageHeader
        title="编辑 Wiki"
        description={`正在编辑“${wiki.title}” · 当前版本 ${wiki.revision_number}`}
        badge={<PencilIcon className="size-5 text-primary" />}
      />

      {validationError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>请检查内容</AlertTitle>
          <AlertDescription>{validationError}</AlertDescription>
        </Alert>
      ) : null}
      {update.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>保存失败</AlertTitle>
          <AlertDescription>
            内容没有丢失。可能已有其他人提交了新版本，请刷新页面确认后再保存。
          </AlertDescription>
        </Alert>
      ) : null}

      <form onSubmit={handleSubmit}>
        <Card>
          <CardHeader>
            <CardTitle>文档内容</CardTitle>
            <CardDescription>
              协作者只能修改标题、摘要和正文；路由、可见性、排序与协作者由管理员维护。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              <Field data-invalid={!form.title.trim()}>
                <FieldLabel htmlFor="wiki-title">标题</FieldLabel>
                <Input
                  id="wiki-title"
                  value={form.title}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      title: event.target.value,
                    }))
                  }
                  maxLength={160}
                  disabled={update.isPending}
                />
                {!form.title.trim() ? (
                  <FieldError>请输入标题。</FieldError>
                ) : null}
              </Field>
              <Field>
                <FieldLabel htmlFor="wiki-summary">摘要</FieldLabel>
                <Textarea
                  id="wiki-summary"
                  value={form.summary}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      summary: event.target.value,
                    }))
                  }
                  maxLength={500}
                  rows={3}
                  disabled={update.isPending}
                />
                <FieldDescription>
                  {form.summary.length}/500 字符
                </FieldDescription>
              </Field>
              <Field data-invalid={!form.body.trim()}>
                <FieldLabel htmlFor="wiki-body">正文</FieldLabel>
                <WikiMarkdownEditor
                  id="wiki-body"
                  value={form.body}
                  onValueChange={(body) =>
                    setForm((current) => ({ ...current, body }))
                  }
                  invalid={!form.body.trim()}
                  disabled={update.isPending}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="wiki-reason">变更说明（可选）</FieldLabel>
                <Input
                  id="wiki-reason"
                  value={form.reason}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      reason: event.target.value,
                    }))
                  }
                  maxLength={500}
                  placeholder="留空时由系统自动生成"
                  disabled={update.isPending}
                />
                <FieldDescription>
                  不再强制填写 5 或 10 个字符；留空会生成稳定的版本说明。
                </FieldDescription>
              </Field>
              <div className="flex flex-wrap justify-end gap-2">
                <Link
                  to={`/wiki/${encodeURIComponent(wiki.slug)}`}
                  className={buttonVariants({ variant: "outline" })}
                >
                  取消
                </Link>
                <Button type="submit" disabled={update.isPending}>
                  <SaveIcon data-icon="inline-start" />
                  {update.isPending ? "保存中…" : "保存新版本"}
                </Button>
              </div>
            </FieldGroup>
          </CardContent>
        </Card>
      </form>
    </PageLayout>
  )
}

function WikiEditProblem({
  title,
  description,
}: {
  title: string
  description: string
}) {
  return (
    <PageLayout className="max-w-3xl">
      <Alert variant="destructive">
        <CircleAlertIcon />
        <AlertTitle>{title}</AlertTitle>
        <AlertDescription>{description}</AlertDescription>
      </Alert>
      <Link to="/wiki" className={buttonVariants({ variant: "outline" })}>
        <ArrowLeftIcon data-icon="inline-start" />
        返回 Wiki
      </Link>
    </PageLayout>
  )
}

function WikiEditSkeleton() {
  return (
    <PageLayout className="max-w-5xl" aria-busy="true">
      <Skeleton className="h-9 w-28" />
      <Skeleton className="h-10 w-56" />
      <Skeleton className="h-[680px]" />
    </PageLayout>
  )
}
