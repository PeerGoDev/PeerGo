import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  Clock3Icon,
  EyeIcon,
  FilePenLineIcon,
  HistoryIcon,
  MegaphoneIcon,
  PlusIcon,
  SaveIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
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
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "~/components/ui/sheet"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type AnnouncementRevisionPage,
  type ManagedAnnouncement,
  announcementRevisionQueryOptions,
  managedAnnouncementQueryOptions,
  useCreateManagedAnnouncement,
  useUpdateManagedAnnouncementDraft,
} from "~/features/staff/api/announcement-administration.queries"
import { AnnouncementStatusBadge } from "~/features/staff/components/announcement-table"
import {
  type AnnouncementDraftFormField,
  announcementDraftFormSchema,
  hasAnnouncementContentChanges,
} from "~/features/staff/model/announcement-form"
import { ApiProblemError } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

type FormErrors = Partial<Record<AnnouncementDraftFormField | "form", string>>

export function AnnouncementEditorSheet({
  announcementId,
  csrfToken,
  canUpdate,
  onOpenChange,
  onSaved,
}: {
  announcementId?: string
  csrfToken: string
  canUpdate: boolean
  onOpenChange: (open: boolean) => void
  onSaved: (
    announcement: ManagedAnnouncement,
    mode: "created" | "updated"
  ) => void
}) {
  const detail = useQuery(
    managedAnnouncementQueryOptions(
      announcementId ?? "",
      Boolean(announcementId)
    )
  )

  if (announcementId && detail.isPending) {
    return (
      <Sheet open onOpenChange={onOpenChange}>
        <SheetContent className="w-full sm:max-w-2xl">
          <SheetHeader className="border-b pr-12">
            <SheetTitle>读取公告</SheetTitle>
            <SheetDescription>
              正在载入当前编辑修订与发布状态。
            </SheetDescription>
          </SheetHeader>
          <div className="flex flex-col gap-4 px-4">
            <Skeleton className="h-10 w-56" />
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-72 w-full" />
          </div>
        </SheetContent>
      </Sheet>
    )
  }

  if (announcementId && (detail.isError || !detail.data)) {
    return (
      <Sheet open onOpenChange={onOpenChange}>
        <SheetContent className="w-full sm:max-w-lg">
          <SheetHeader className="border-b pr-12">
            <SheetTitle>公告暂时无法读取</SheetTitle>
            <SheetDescription>
              编辑面板不会使用列表摘要代替完整正文。
            </SheetDescription>
          </SheetHeader>
          <div className="px-4">
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>公告详情读取失败</AlertTitle>
              <AlertDescription>请关闭面板、刷新列表后重试。</AlertDescription>
            </Alert>
          </div>
          <SheetFooter className="border-t">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              关闭
            </Button>
            <Button onClick={() => void detail.refetch()}>重试</Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    )
  }

  return (
    <AnnouncementEditorLoaded
      key={detail.data ? `${detail.data.id}:${detail.data.version}` : "create"}
      announcement={detail.data}
      csrfToken={csrfToken}
      canUpdate={canUpdate}
      onOpenChange={onOpenChange}
      onSaved={onSaved}
    />
  )
}

function AnnouncementEditorLoaded({
  announcement,
  csrfToken,
  canUpdate,
  onOpenChange,
  onSaved,
}: {
  announcement?: ManagedAnnouncement
  csrfToken: string
  canUpdate: boolean
  onOpenChange: (open: boolean) => void
  onSaved: (
    announcement: ManagedAnnouncement,
    mode: "created" | "updated"
  ) => void
}) {
  const createMutation = useCreateManagedAnnouncement()
  const updateMutation = useUpdateManagedAnnouncementDraft()
  const revisions = useQuery(
    announcementRevisionQueryOptions(
      announcement?.id ?? "",
      Boolean(announcement)
    )
  )
  const [view, setView] = React.useState<"edit" | "preview">("edit")
  const [id, setId] = React.useState(announcement?.id ?? "")
  const [title, setTitle] = React.useState(announcement?.title ?? "")
  const [summary, setSummary] = React.useState(announcement?.summary ?? "")
  const [body, setBody] = React.useState(announcement?.body ?? "")
  const [bodyFormat, setBodyFormat] = React.useState<
    ManagedAnnouncement["body_format"]
  >(announcement?.body_format ?? "plain_text")
  const [reason, setReason] = React.useState("")
  const [errors, setErrors] = React.useState<FormErrors>({})
  const mutation = announcement ? updateMutation : createMutation
  const isPending = mutation.isPending
  const editingLocked = Boolean(
    announcement?.status === "scheduled" || (announcement && !canUpdate)
  )

  function handleSheetOpenChange(nextOpen: boolean) {
    if (!nextOpen && isPending) {
      return
    }
    onOpenChange(nextOpen)
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (editingLocked) {
      return
    }
    mutation.reset()
    const formElement = event.currentTarget
    const result = announcementDraftFormSchema.safeParse({
      id,
      title,
      summary,
      body,
      bodyFormat,
      reason,
    })
    if (!result.success) {
      const nextErrors: FormErrors = {}
      for (const issue of result.error.issues) {
        const field = issue.path[0]
        if (
          typeof field === "string" &&
          !nextErrors[field as AnnouncementDraftFormField]
        ) {
          nextErrors[field as AnnouncementDraftFormField] = issue.message
        }
      }
      setErrors(nextErrors)
      setView("edit")
      requestAnimationFrame(() => {
        formElement.querySelector<HTMLElement>("[aria-invalid='true']")?.focus()
      })
      return
    }
    if (
      announcement &&
      !hasAnnouncementContentChanges(announcement, result.data)
    ) {
      setErrors({ form: "标题、摘要、正文和格式均未变化，无需创建空修订。" })
      setView("edit")
      return
    }
    setErrors({})
    try {
      if (announcement) {
        const updated = await updateMutation.mutateAsync({
          csrfToken,
          announcementId: announcement.id,
          body: {
            title: result.data.title,
            summary: result.data.summary,
            body: result.data.body,
            body_format: result.data.bodyFormat,
            expected_version: announcement.version,
            reason: result.data.reason,
          },
        })
        onSaved(updated, "updated")
      } else {
        const created = await createMutation.mutateAsync({
          csrfToken,
          body: {
            id: result.data.id,
            title: result.data.title,
            summary: result.data.summary,
            body: result.data.body,
            body_format: result.data.bodyFormat,
            reason: result.data.reason,
          },
        })
        onSaved(created, "created")
      }
    } catch {
      setView("edit")
      // The API problem remains visible without discarding the entered draft.
    }
  }

  return (
    <Sheet open onOpenChange={handleSheetOpenChange}>
      <SheetContent className="w-full sm:max-w-2xl">
        <SheetHeader className="border-b pr-12">
          <div className="flex flex-wrap items-center gap-2">
            <SheetTitle>{announcement ? "公告工作台" : "创建公告"}</SheetTitle>
            {announcement ? (
              <>
                <AnnouncementStatusBadge status={announcement.status} />
                <Badge variant="outline">
                  状态第 {announcement.version} 版
                </Badge>
                <Badge variant="outline">
                  正文第 {announcement.revision_number} 稿
                </Badge>
              </>
            ) : null}
          </div>
          <SheetDescription>
            {announcement
              ? "正文保存为不可变修订；草稿不会覆盖当前公开版本。"
              : "先建立草稿与稳定公开路由键，保存后再单独执行发布。"}
          </SheetDescription>
        </SheetHeader>

        <div className="border-b px-4 pb-3">
          <ToggleGroup
            value={[view]}
            onValueChange={(values) => {
              const nextView = values[0]
              if (nextView === "edit" || nextView === "preview") {
                setView(nextView)
              }
            }}
            variant="outline"
            spacing={0}
            aria-label="公告编辑视图"
          >
            <ToggleGroupItem value="edit">
              <FilePenLineIcon data-icon="inline-start" />
              编辑
            </ToggleGroupItem>
            <ToggleGroupItem value="preview">
              <EyeIcon data-icon="inline-start" />
              上线预览
            </ToggleGroupItem>
          </ToggleGroup>
        </div>

        <form
          id="announcement-editor-form"
          className="flex min-h-0 flex-1 flex-col"
          onSubmit={handleSubmit}
          noValidate
        >
          <div className="flex flex-1 flex-col gap-5 overflow-y-auto px-4 pb-6">
            {mutation.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>{draftErrorTitle(mutation.error)}</AlertTitle>
                <AlertDescription>
                  {draftErrorDescription(mutation.error)}
                </AlertDescription>
              </Alert>
            ) : null}

            {errors.form ? (
              <Alert>
                <CircleAlertIcon />
                <AlertTitle>没有可保存的正文变更</AlertTitle>
                <AlertDescription>{errors.form}</AlertDescription>
              </Alert>
            ) : null}

            {announcement?.status === "scheduled" ? (
              <Alert>
                <Clock3Icon />
                <AlertTitle>预约修订已冻结</AlertTitle>
                <AlertDescription>
                  请先在列表中取消排期，再基于退回的草稿追加新修订；现有公开版本不受影响。
                </AlertDescription>
              </Alert>
            ) : null}

            {view === "preview" ? (
              <AnnouncementPreview
                title={title}
                summary={summary}
                body={body}
                bodyFormat={bodyFormat}
              />
            ) : (
              <FieldGroup>
                <Field data-invalid={Boolean(errors.id)}>
                  <FieldLabel htmlFor="announcement-id">公开路由键</FieldLabel>
                  <Input
                    id="announcement-id"
                    value={id}
                    onChange={(event) => setId(event.target.value)}
                    placeholder="例如 maintenance-2026-08"
                    maxLength={120}
                    pattern="[A-Za-z0-9][A-Za-z0-9._-]{0,119}"
                    disabled={
                      Boolean(announcement) || isPending || editingLocked
                    }
                    aria-invalid={Boolean(errors.id)}
                    autoFocus={!announcement}
                  />
                  <FieldDescription>
                    创建后不可修改，并用于公开链接及评论目标绑定。
                  </FieldDescription>
                  <FieldError
                    errors={errors.id ? [{ message: errors.id }] : []}
                  />
                </Field>

                <Field data-invalid={Boolean(errors.title)}>
                  <FieldLabel htmlFor="announcement-title">标题</FieldLabel>
                  <Input
                    id="announcement-title"
                    value={title}
                    onChange={(event) => setTitle(event.target.value)}
                    maxLength={160}
                    disabled={isPending || editingLocked}
                    aria-invalid={Boolean(errors.title)}
                    autoFocus={Boolean(announcement) && !editingLocked}
                  />
                  <FieldError
                    errors={errors.title ? [{ message: errors.title }] : []}
                  />
                </Field>

                <Field data-invalid={Boolean(errors.summary)}>
                  <FieldLabel htmlFor="announcement-summary">摘要</FieldLabel>
                  <Textarea
                    id="announcement-summary"
                    value={summary}
                    onChange={(event) => setSummary(event.target.value)}
                    rows={3}
                    maxLength={500}
                    disabled={isPending || editingLocked}
                    aria-invalid={Boolean(errors.summary)}
                  />
                  <FieldDescription>
                    用于首页公告卡片，正文页会继续显示完整内容。
                  </FieldDescription>
                  <FieldError
                    errors={errors.summary ? [{ message: errors.summary }] : []}
                  />
                </Field>

                <Field>
                  <FieldLabel>正文格式</FieldLabel>
                  <ToggleGroup
                    value={[bodyFormat]}
                    onValueChange={(values) => {
                      const format = values[0]
                      if (
                        format === "plain_text" ||
                        format === "legacy_bbcode"
                      ) {
                        setBodyFormat(format)
                      }
                    }}
                    variant="outline"
                    spacing={0}
                    disabled={isPending || editingLocked}
                    aria-label="公告正文格式"
                  >
                    <ToggleGroupItem value="plain_text">纯文本</ToggleGroupItem>
                    <ToggleGroupItem value="legacy_bbcode">
                      旧版 BBCode 文本
                    </ToggleGroupItem>
                  </ToggleGroup>
                  <FieldDescription>
                    旧版格式只用于迁移兼容；展示时会安全转义，不执行标记或嵌入。
                  </FieldDescription>
                </Field>

                <Field data-invalid={Boolean(errors.body)}>
                  <FieldLabel htmlFor="announcement-body">正文</FieldLabel>
                  <Textarea
                    id="announcement-body"
                    value={body}
                    onChange={(event) => setBody(event.target.value)}
                    rows={14}
                    maxLength={20_000}
                    disabled={isPending || editingLocked}
                    aria-invalid={Boolean(errors.body)}
                    className="min-h-72 font-mono text-sm leading-6"
                  />
                  <FieldDescription>
                    最多 20000 字；请在“上线预览”中核对换行与长文本。
                  </FieldDescription>
                  <FieldError
                    errors={errors.body ? [{ message: errors.body }] : []}
                  />
                </Field>

                <Field data-invalid={Boolean(errors.reason)}>
                  <FieldLabel htmlFor="announcement-draft-reason">
                    草稿变更理由
                  </FieldLabel>
                  <Textarea
                    id="announcement-draft-reason"
                    value={reason}
                    onChange={(event) => setReason(event.target.value)}
                    rows={4}
                    minLength={10}
                    maxLength={500}
                    placeholder="记录资料来源、修订范围与复核要求（10–500 字）…"
                    disabled={isPending || editingLocked}
                    aria-invalid={Boolean(errors.reason)}
                  />
                  <FieldDescription>
                    保存只追加草稿修订，不会改变当前公开内容。
                  </FieldDescription>
                  <FieldError
                    errors={errors.reason ? [{ message: errors.reason }] : []}
                  />
                </Field>
              </FieldGroup>
            )}

            {announcement ? (
              <AnnouncementRevisionHistory
                pending={revisions.isPending}
                failed={revisions.isError}
                items={revisions.data?.items}
              />
            ) : null}
          </div>

          <SheetFooter className="border-t bg-muted/30 sm:flex-row sm:justify-end">
            <Button
              type="button"
              variant="outline"
              disabled={isPending}
              onClick={() => onOpenChange(false)}
            >
              关闭
            </Button>
            {view === "preview" ? (
              <Button type="button" onClick={() => setView("edit")}>
                <FilePenLineIcon data-icon="inline-start" />
                返回编辑
              </Button>
            ) : !editingLocked ? (
              <Button type="submit" disabled={isPending}>
                {isPending ? (
                  <Spinner />
                ) : announcement ? (
                  <SaveIcon data-icon="inline-start" />
                ) : (
                  <PlusIcon data-icon="inline-start" />
                )}
                {isPending
                  ? "正在保存…"
                  : announcement
                    ? "保存新草稿修订"
                    : "创建公告草稿"}
              </Button>
            ) : null}
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  )
}

function AnnouncementPreview({
  title,
  summary,
  body,
  bodyFormat,
}: {
  title: string
  summary: string
  body: string
  bodyFormat: ManagedAnnouncement["body_format"]
}) {
  return (
    <div className="flex flex-col gap-4">
      <Alert>
        <EyeIcon />
        <AlertTitle>安全上线预览</AlertTitle>
        <AlertDescription>
          这里使用和用户端一致的转义纯文本策略，不执行 HTML、BBCode 或外部嵌入。
        </AlertDescription>
      </Alert>
      <Card>
        <CardHeader className="border-b">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary">
              <MegaphoneIcon data-icon="inline-start" />
              站点公告
            </Badge>
            {bodyFormat === "legacy_bbcode" ? (
              <Badge variant="outline">旧版格式</Badge>
            ) : null}
          </div>
          <CardTitle className="text-2xl break-words">
            {title.trim() || "未填写标题"}
          </CardTitle>
          <CardDescription className="leading-relaxed break-words">
            {summary.trim() || "未填写摘要"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="text-sm leading-7 break-words whitespace-pre-wrap">
            {body.trim() || "未填写正文"}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function AnnouncementRevisionHistory({
  pending,
  failed,
  items,
}: {
  pending: boolean
  failed: boolean
  items: AnnouncementRevisionPage["items"] | undefined
}) {
  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="flex items-center gap-2 text-base">
          <HistoryIcon className="size-4" />
          版本记录
        </CardTitle>
        <CardDescription>
          修订只追加、不覆盖；发布状态变化不会伪造新的正文版本。
        </CardDescription>
      </CardHeader>
      <CardContent>
        {pending ? (
          <div className="flex flex-col gap-2" aria-busy="true">
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
          </div>
        ) : failed ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>版本记录暂时无法读取</AlertTitle>
          </Alert>
        ) : items && items.length > 0 ? (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader className="bg-muted/45">
                <TableRow>
                  <TableHead>修订</TableHead>
                  <TableHead>标题</TableHead>
                  <TableHead>标记</TableHead>
                  <TableHead>创建时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((revision) => (
                  <TableRow key={revision.revision_number}>
                    <TableCell className="text-xs">
                      第 {revision.revision_number} 稿
                    </TableCell>
                    <TableCell className="max-w-52 whitespace-normal">
                      <p className="line-clamp-2 font-medium">
                        {revision.title}
                      </p>
                      <p className="mt-0.5 text-[11px] text-muted-foreground">
                        {revision.editor_display_name ??
                          originLabel(revision.origin)}
                      </p>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {revision.is_draft ? (
                          <Badge variant="secondary">草稿</Badge>
                        ) : null}
                        {revision.is_scheduled ? (
                          <Badge variant="outline">排期</Badge>
                        ) : null}
                        {revision.is_published ? (
                          <Badge variant="outline">公开</Badge>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatDateTime(revision.created_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        ) : (
          <Empty className="min-h-36 border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <HistoryIcon />
              </EmptyMedia>
              <EmptyTitle>暂无修订记录</EmptyTitle>
              <EmptyDescription>请刷新后重试。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
      </CardContent>
    </Card>
  )
}

function originLabel(origin: "migration" | "development_seed" | "staff") {
  switch (origin) {
    case "migration":
      return "历史迁移"
    case "development_seed":
      return "系统初始化"
    case "staff":
      return "后台人员"
  }
}

function draftErrorTitle(error: Error) {
  if (error instanceof ApiProblemError) {
    switch (error.code) {
      case "announcement_version_conflict":
        return "公告已被其他操作更新"
      case "announcement_exists":
        return "公开路由键已经存在"
      case "announcement_publication_conflict":
        return "预约修订暂时不能编辑"
      case "announcement_no_changes":
        return "正文没有变化"
      case "announcement_change_denied":
        return "当前职责不能保存公告"
      case "invalid_announcement":
        return "公告内容未通过服务端校验"
    }
  }
  return "公告草稿保存失败"
}

function draftErrorDescription(error: Error) {
  if (
    error instanceof ApiProblemError &&
    (error.code === "announcement_version_conflict" ||
      error.code === "announcement_publication_conflict")
  ) {
    return "请关闭面板，刷新后继续；已有修订没有被覆盖。"
  }
  return "本次保存没有生效；已输入的草稿仍保留在当前面板中。"
}
