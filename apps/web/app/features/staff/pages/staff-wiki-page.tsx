import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  ArchiveIcon,
  BookOpenTextIcon,
  CheckCircle2Icon,
  CircleAlertIcon,
  ExternalLinkIcon,
  HistoryIcon,
  PencilIcon,
  PlusIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  SearchIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog"
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "~/components/ui/input-group"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Skeleton } from "~/components/ui/skeleton"
import { Switch } from "~/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { Textarea } from "~/components/ui/textarea"
import {
  managedWikiPageListQueryOptions,
  managedWikiPageQueryOptions,
  managedWikiRevisionsQueryOptions,
  type WikiPage,
  useCreateManagedWikiPage,
  useRestoreManagedWikiRevision,
  useUpdateManagedWikiPage,
} from "~/features/staff/api/wiki-administration.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffPageHeader } from "~/features/staff/components/staff-page-header"
import { hasCapability } from "~/features/staff/model/capability"
import { WikiMarkdownEditor } from "~/features/wiki/components/wiki-markdown-editor"
import type { components } from "~/generated/api"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

type CapabilityList = components["schemas"]["CapabilityList"]
type EditorState = { mode: "create" } | { mode: "edit"; pageId: string }

export function StaffWikiPage() {
  return (
    <StaffAccessGate
      requiredAction="wiki.page.manage.read"
      pageHeader={{
        title: "Wiki 管理",
        icon: BookOpenTextIcon,
        variant: "hero",
        frameClassName: "gap-0",
        contentClassName: "px-4 py-8",
      }}
    >
      {({ session, capabilities }) => (
        <WikiAdministration
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function WikiAdministration({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const [draftQuery, setDraftQuery] = React.useState("")
  const [query, setQuery] = React.useState("")
  const [editor, setEditor] = React.useState<EditorState>()
  const [success, setSuccess] = React.useState("")
  const pages = useQuery(managedWikiPageListQueryOptions(query))
  const canCreate = hasCapability(capabilities, "wiki.page.create")
  const canUpdate = hasCapability(capabilities, "wiki.page.update")
  const canRestore = hasCapability(capabilities, "wiki.page.restore")

  return (
    <StaffPageFrame>
      <StaffPageHeader
        title="Wiki 管理"
        description="维护站点知识库、协作者与最近 50 个真实修订；阅读次数、搜索词和自动保存不会写入数据库。"
        icon={BookOpenTextIcon}
        variant="hero"
        actions={
          canCreate ? (
            <Button
              onClick={() => {
                setSuccess("")
                setEditor({ mode: "create" })
              }}
            >
              <PlusIcon data-icon="inline-start" />
              创建文档
            </Button>
          ) : null
        }
      />

      <form
        onSubmit={(event) => {
          event.preventDefault()
          setQuery(draftQuery.trim())
        }}
      >
        <InputGroup className="max-w-2xl">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput
            value={draftQuery}
            onChange={(event) => setDraftQuery(event.target.value)}
            placeholder="搜索标题、路由或正文"
            maxLength={100}
            aria-label="搜索 Wiki"
          />
          <InputGroupAddon align="inline-end">
            <InputGroupButton type="submit">搜索</InputGroupButton>
          </InputGroupAddon>
        </InputGroup>
      </form>

      {success ? (
        <Alert>
          <CheckCircle2Icon />
          <AlertTitle>Wiki 已更新</AlertTitle>
          <AlertDescription>{success}</AlertDescription>
        </Alert>
      ) : null}

      {pages.isPending ? <WikiTableSkeleton /> : null}
      {pages.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>无法读取 Wiki 管理列表</AlertTitle>
          <AlertDescription>
            请确认 Core 服务和数据库迁移状态。
          </AlertDescription>
          <AlertAction>
            <Button
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
          <Card>
            <CardHeader>
              <CardTitle>文档列表</CardTitle>
              <CardDescription>
                共 {pages.data.total} 篇（包括已停用文档）
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>文档</TableHead>
                    <TableHead>范围</TableHead>
                    <TableHead>版本</TableHead>
                    <TableHead>更新时间</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {pages.data.items.map((page) => (
                    <TableRow key={page.id}>
                      <TableCell className="max-w-md whitespace-normal">
                        <div className="flex flex-col gap-1">
                          <span className="font-medium">{page.title}</span>
                          <code className="text-xs text-muted-foreground">
                            /{page.slug}
                          </code>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          <Badge variant="outline">
                            {page.visibility === "members" ? "成员" : "公开"}
                          </Badge>
                          {page.archived ? (
                            <Badge variant="destructive">已停用</Badge>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell>#{page.revision_number}</TableCell>
                      <TableCell>
                        {formatCompactDateTime(page.updated_at)}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          {!page.archived ? (
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-sm"
                              aria-label={`打开 ${page.title}`}
                              onClick={() =>
                                window.open(
                                  `/wiki/${page.slug}`,
                                  "_blank",
                                  "noopener"
                                )
                              }
                            >
                              <ExternalLinkIcon />
                            </Button>
                          ) : null}
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            onClick={() =>
                              setEditor({ mode: "edit", pageId: page.id })
                            }
                          >
                            <PencilIcon data-icon="inline-start" />
                            {canUpdate ? "编辑" : "查看"}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        ) : (
          <Empty className="border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <BookOpenTextIcon />
              </EmptyMedia>
              <EmptyTitle>没有匹配的 Wiki</EmptyTitle>
              <EmptyDescription>
                清除搜索条件或创建一篇新文档。
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )
      ) : null}

      {editor ? (
        <WikiEditorDialog
          key={editor.mode === "create" ? "create" : editor.pageId}
          state={editor}
          csrfToken={csrfToken}
          canUpdate={canUpdate}
          canRestore={canRestore}
          onClose={() => setEditor(undefined)}
          onSaved={(message) => {
            setSuccess(message)
            setEditor(undefined)
          }}
        />
      ) : null}
    </StaffPageFrame>
  )
}

type WikiForm = {
  slug: string
  title: string
  summary: string
  body: string
  visibility: "public" | "members"
  sortOrder: string
  editorIds: string
  reason: string
  archived: boolean
}

const emptyForm: WikiForm = {
  slug: "",
  title: "",
  summary: "",
  body: "",
  visibility: "members",
  sortOrder: "0",
  editorIds: "",
  reason: "",
  archived: false,
}

function WikiEditorDialog({
  state,
  csrfToken,
  canUpdate,
  canRestore,
  onClose,
  onSaved,
}: {
  state: EditorState
  csrfToken: string
  canUpdate: boolean
  canRestore: boolean
  onClose: () => void
  onSaved: (message: string) => void
}) {
  const pageId = state.mode === "edit" ? state.pageId : ""
  const detail = useQuery(
    managedWikiPageQueryOptions(pageId, state.mode === "edit")
  )
  const revisions = useQuery(
    managedWikiRevisionsQueryOptions(pageId, state.mode === "edit")
  )
  const create = useCreateManagedWikiPage()
  const update = useUpdateManagedWikiPage()
  const restore = useRestoreManagedWikiRevision()
  const [form, setForm] = React.useState<WikiForm>(emptyForm)
  const [loadedVersion, setLoadedVersion] = React.useState<number>()
  const [error, setError] = React.useState("")
  const [restoreRevision, setRestoreRevision] = React.useState<number>()

  React.useEffect(() => {
    if (!detail.data || loadedVersion === detail.data.version) return
    setForm(formFromPage(detail.data))
    setLoadedVersion(detail.data.version)
  }, [detail.data, loadedVersion])

  const busy = create.isPending || update.isPending || restore.isPending
  const readOnly = state.mode === "edit" && !canUpdate

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    const parsed = parseWikiForm(form)
    if (typeof parsed === "string") {
      setError(parsed)
      return
    }
    setError("")
    try {
      if (state.mode === "create") {
        const saved = await create.mutateAsync({ csrfToken, body: parsed })
        onSaved(`已创建“${saved.title}”。`)
      } else if (detail.data) {
        const saved = await update.mutateAsync({
          csrfToken,
          pageId,
          body: {
            ...parsed,
            expected_version: detail.data.version,
            archived: form.archived,
          },
        })
        onSaved(`已保存“${saved.title}”的新版本。`)
      }
    } catch {
      setError(
        "保存失败；内容已保留。请检查路由是否重复、协作者 ID 是否存在，或刷新后处理版本冲突。"
      )
    }
  }

  async function confirmRestore() {
    if (!detail.data || restoreRevision === undefined) return
    try {
      const saved = await restore.mutateAsync({
        csrfToken,
        pageId,
        revisionNumber: restoreRevision,
        expectedVersion: detail.data.version,
        reason: "",
      })
      setRestoreRevision(undefined)
      onSaved(`已将“${saved.title}”的第 ${restoreRevision} 版恢复为新版本。`)
    } catch {
      setError("恢复失败；请刷新版本历史后重试。")
      setRestoreRevision(undefined)
    }
  }

  return (
    <>
      <Dialog open onOpenChange={(open) => !open && onClose()}>
        <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-5xl">
          <DialogHeader>
            <DialogTitle>
              {state.mode === "create" ? "创建 Wiki" : "Wiki 编辑与版本"}
            </DialogTitle>
            <DialogDescription>
              只保存当前投影和有界修订；协作者使用数字用户 ID，最多 20 人。
            </DialogDescription>
          </DialogHeader>

          {state.mode === "edit" && detail.isPending ? (
            <EditorSkeleton />
          ) : null}
          {detail.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>无法读取文档</AlertTitle>
              <AlertDescription>请关闭后重试。</AlertDescription>
            </Alert>
          ) : null}
          {error ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>操作未完成</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          {(state.mode === "create" || detail.data) && (
            <form id="wiki-managed-form" onSubmit={handleSubmit}>
              <FieldGroup>
                <div className="grid gap-5 md:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="managed-wiki-title">标题</FieldLabel>
                    <Input
                      id="managed-wiki-title"
                      value={form.title}
                      onChange={(event) =>
                        setFormValue(setForm, "title", event.target.value)
                      }
                      maxLength={160}
                      disabled={busy || readOnly}
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="managed-wiki-slug">
                      路由 Slug
                    </FieldLabel>
                    <Input
                      id="managed-wiki-slug"
                      value={form.slug}
                      onChange={(event) =>
                        setFormValue(
                          setForm,
                          "slug",
                          event.target.value.toLowerCase()
                        )
                      }
                      maxLength={96}
                      placeholder="user-guide"
                      disabled={busy || readOnly}
                    />
                    <FieldDescription>
                      小写字母、数字与连字符，修改后旧链接不再生效。
                    </FieldDescription>
                  </Field>
                </div>
                <Field>
                  <FieldLabel htmlFor="managed-wiki-summary">摘要</FieldLabel>
                  <Textarea
                    id="managed-wiki-summary"
                    value={form.summary}
                    onChange={(event) =>
                      setFormValue(setForm, "summary", event.target.value)
                    }
                    maxLength={500}
                    rows={2}
                    disabled={busy || readOnly}
                  />
                </Field>
                <div className="grid gap-5 md:grid-cols-3">
                  <Field>
                    <FieldLabel htmlFor="managed-wiki-visibility">
                      可见范围
                    </FieldLabel>
                    <Select
                      value={form.visibility}
                      onValueChange={(value) =>
                        value &&
                        setFormValue(
                          setForm,
                          "visibility",
                          value as "public" | "members"
                        )
                      }
                      disabled={busy || readOnly}
                    >
                      <SelectTrigger
                        id="managed-wiki-visibility"
                        className="w-full"
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectLabel>阅读范围</SelectLabel>
                          <SelectItem value="members">登录成员</SelectItem>
                          <SelectItem value="public">公开访问</SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="managed-wiki-order">
                      排序权重
                    </FieldLabel>
                    <Input
                      id="managed-wiki-order"
                      type="number"
                      min={-100000}
                      max={100000}
                      value={form.sortOrder}
                      onChange={(event) =>
                        setFormValue(setForm, "sortOrder", event.target.value)
                      }
                      disabled={busy || readOnly}
                    />
                    <FieldDescription>数值越大越靠前。</FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="managed-wiki-editors">
                      协作者数字 ID
                    </FieldLabel>
                    <Input
                      id="managed-wiki-editors"
                      value={form.editorIds}
                      onChange={(event) =>
                        setFormValue(setForm, "editorIds", event.target.value)
                      }
                      placeholder="163, 2179"
                      disabled={busy || readOnly}
                    />
                    <FieldDescription>逗号分隔，最多 20 人。</FieldDescription>
                  </Field>
                </div>
                {state.mode === "edit" ? (
                  <Field orientation="horizontal">
                    <FieldLabel htmlFor="managed-wiki-archived">
                      <span className="flex flex-col gap-1">
                        <span>停用这篇文档</span>
                        <span className="text-xs font-normal text-muted-foreground">
                          停用后前台不可见，历史和正文仍可恢复。
                        </span>
                      </span>
                    </FieldLabel>
                    <Switch
                      id="managed-wiki-archived"
                      checked={form.archived}
                      onCheckedChange={(checked) =>
                        setFormValue(setForm, "archived", checked)
                      }
                      disabled={busy || readOnly}
                    />
                  </Field>
                ) : null}
                <Field data-invalid={!form.body.trim()}>
                  <FieldLabel htmlFor="managed-wiki-body">
                    Markdown 正文
                  </FieldLabel>
                  <WikiMarkdownEditor
                    id="managed-wiki-body"
                    value={form.body}
                    onValueChange={(body) =>
                      setFormValue(setForm, "body", body)
                    }
                    invalid={!form.body.trim()}
                    disabled={busy || readOnly}
                    minHeight={520}
                  />
                  {!form.body.trim() ? (
                    <FieldError>正文不能为空。</FieldError>
                  ) : null}
                </Field>
                <Field>
                  <FieldLabel htmlFor="managed-wiki-reason">
                    变更说明（可选）
                  </FieldLabel>
                  <Input
                    id="managed-wiki-reason"
                    value={form.reason}
                    onChange={(event) =>
                      setFormValue(setForm, "reason", event.target.value)
                    }
                    maxLength={500}
                    placeholder="留空时系统自动生成"
                    disabled={busy || readOnly}
                  />
                </Field>
              </FieldGroup>
            </form>
          )}

          {state.mode === "edit" && detail.data ? (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base">
                  <HistoryIcon className="size-4" />
                  修订历史
                </CardTitle>
                <CardDescription>
                  仅保留最近 50 个有效保存，恢复会创建一个新版本。
                </CardDescription>
              </CardHeader>
              <CardContent>
                {revisions.isPending ? <Skeleton className="h-28" /> : null}
                {revisions.data?.items.length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    暂无修订记录。
                  </p>
                ) : null}
                <div className="flex flex-col gap-2">
                  {revisions.data?.items.map((revision) => (
                    <div
                      key={revision.revision_number}
                      className="flex flex-wrap items-center gap-3 rounded-lg border px-3 py-2 text-sm"
                    >
                      <Badge variant="outline">
                        #{revision.revision_number}
                      </Badge>
                      <div className="min-w-0 flex-1">
                        <p className="font-medium break-words">
                          {revision.reason}
                        </p>
                        <p className="text-xs text-muted-foreground">
                          {revision.editor.display_name} ·{" "}
                          {formatCompactDateTime(revision.created_at)} ·{" "}
                          {revision.origin}
                        </p>
                      </div>
                      {canRestore &&
                      revision.revision_number !==
                        detail.data.revision_number ? (
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          onClick={() =>
                            setRestoreRevision(revision.revision_number)
                          }
                          disabled={busy}
                        >
                          <RotateCcwIcon data-icon="inline-start" />
                          恢复
                        </Button>
                      ) : null}
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          ) : null}

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={onClose}
              disabled={busy}
            >
              关闭
            </Button>
            {!readOnly ? (
              <Button type="submit" form="wiki-managed-form" disabled={busy}>
                {form.archived ? (
                  <ArchiveIcon data-icon="inline-start" />
                ) : (
                  <PencilIcon data-icon="inline-start" />
                )}
                {busy
                  ? "处理中…"
                  : state.mode === "create"
                    ? "创建文档"
                    : "保存新版本"}
              </Button>
            ) : null}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={restoreRevision !== undefined}
        onOpenChange={(open) => !open && setRestoreRevision(undefined)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>恢复第 {restoreRevision} 版？</AlertDialogTitle>
            <AlertDialogDescription>
              当前内容不会被删除；历史内容会复制成一个新的当前版本。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirmRestore()}>
              确认恢复
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function formFromPage(page: WikiPage): WikiForm {
  return {
    slug: page.slug,
    title: page.title,
    summary: page.summary,
    body: page.body,
    visibility: page.visibility,
    sortOrder: String(page.sort_order),
    editorIds: page.editors.map((editor) => editor.numeric_id).join(", "),
    reason: "",
    archived: page.archived,
  }
}

function parseWikiForm(form: WikiForm) {
  const slug = form.slug.trim()
  const title = form.title.trim()
  const body = form.body.trim()
  const sortOrder = Number(form.sortOrder)
  const editorIds = form.editorIds.trim()
    ? form.editorIds
        .split(/[,，\s]+/u)
        .filter(Boolean)
        .map(Number)
    : []
  const uniqueEditorIds = [...new Set(editorIds)]
  if (!/^[a-z0-9][a-z0-9-]{0,95}$/u.test(slug)) {
    return "路由只能包含小写字母、数字与连字符，最长 96 个字符。"
  }
  if (!title || !body) return "标题和正文不能为空。"
  if (
    !Number.isInteger(sortOrder) ||
    sortOrder < -100000 ||
    sortOrder > 100000
  ) {
    return "排序权重必须是 -100000 到 100000 之间的整数。"
  }
  if (
    uniqueEditorIds.length > 20 ||
    uniqueEditorIds.some((id) => !Number.isSafeInteger(id) || id < 1)
  ) {
    return "协作者必须是 1 到 20 个有效数字用户 ID。"
  }
  return {
    slug,
    title,
    summary: form.summary.trim(),
    body,
    visibility: form.visibility,
    sort_order: sortOrder,
    editor_numeric_ids: uniqueEditorIds,
    reason: form.reason.trim(),
  }
}

function setFormValue<K extends keyof WikiForm>(
  setter: React.Dispatch<React.SetStateAction<WikiForm>>,
  key: K,
  value: WikiForm[K]
) {
  setter((current) => ({ ...current, [key]: value }))
}

function WikiTableSkeleton() {
  return (
    <Card aria-busy="true">
      <CardHeader>
        <Skeleton className="h-6 w-32" />
        <Skeleton className="h-4 w-52" />
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-12 w-full" />
        ))}
      </CardContent>
    </Card>
  )
}

function EditorSkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-busy="true">
      <Skeleton className="h-10 w-full" />
      <Skeleton className="h-20 w-full" />
      <Skeleton className="h-[520px] w-full" />
    </div>
  )
}
