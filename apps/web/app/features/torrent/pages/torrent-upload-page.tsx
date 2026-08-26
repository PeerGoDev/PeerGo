import * as React from "react"
import { Link } from "react-router"
import {
  CheckIcon,
  CircleAlertIcon,
  CircleCheckIcon,
  EyeIcon,
  FileCheck2Icon,
  FileUpIcon,
  GripVerticalIcon,
  ImageIcon,
  LinkIcon,
  LogInIcon,
  MailCheckIcon,
  RefreshCwIcon,
  ShieldXIcon,
  StarIcon,
  UploadIcon,
  XIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import { Checkbox } from "~/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
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
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from "~/components/ui/progress"
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
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import { TorrentMarkdownEditor } from "~/features/torrent/components/torrent-markdown-editor"
import {
  TorrentDescriptionCard,
  TorrentMediaInfoCard,
} from "~/features/torrent/components/torrent-rich-content"
import {
  type TorrentSubmission,
  type TorrentUploadProgress,
  useSubmitTorrent,
} from "~/features/torrent/api/torrent-upload.mutations"
import {
  type TorrentCategory,
  type TorrentCategoryFacet,
  useCategoryFacets,
  useCategoryList,
} from "~/features/torrent/api/torrent.queries"
import {
  type TorrentUploadFormErrors,
  parseExternalIdentifier,
  torrentUploadFieldErrors,
  torrentUploadFormSchema,
} from "~/features/torrent/model/torrent-upload-form"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"
import { cn } from "~/lib/utils"

export function TorrentUploadPage() {
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)

  if (session.isPending) {
    return (
      <TorrentUploadLayout>
        <TorrentUploadSkeleton />
      </TorrentUploadLayout>
    )
  }
  if (session.isError) {
    return (
      <TorrentUploadLayout>
        <UploadAccessCard
          icon={CircleAlertIcon}
          title="暂时无法确认登录状态"
          description={requestErrorDescription(
            session.error,
            "暂时无法确认登录状态，请稍后刷新页面重试。"
          )}
        />
      </TorrentUploadLayout>
    )
  }
  if (!session.data) {
    return (
      <TorrentUploadLayout>
        <UploadAccessCard
          icon={LogInIcon}
          title="需要先登录"
          description="登录后才能提交种子。"
          action={
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          }
        />
      </TorrentUploadLayout>
    )
  }
  if (!session.data.user.email_verified) {
    return (
      <TorrentUploadLayout>
        <UploadAccessCard
          icon={MailCheckIcon}
          title="请先验证邮箱"
          description="完成邮箱验证后即可提交种子。"
          action={
            <Link to="/account/email" className={buttonVariants()}>
              <MailCheckIcon data-icon="inline-start" />
              验证邮箱
            </Link>
          }
        />
      </TorrentUploadLayout>
    )
  }
  if (capabilities.isPending) {
    return (
      <TorrentUploadLayout>
        <TorrentUploadSkeleton />
      </TorrentUploadLayout>
    )
  }
  if (capabilities.isError) {
    return (
      <TorrentUploadLayout>
        <UploadAccessCard
          icon={CircleAlertIcon}
          title="暂时无法确认上传资格"
          description={requestErrorDescription(
            capabilities.error,
            "暂时无法确认上传资格，请稍后重试。"
          )}
          action={
            <Button onClick={() => void capabilities.refetch()}>
              <RefreshCwIcon data-icon="inline-start" />
              重新核对
            </Button>
          }
        />
      </TorrentUploadLayout>
    )
  }
  if (
    !capabilities.data?.items.some(
      (capability) => capability.action === "torrent.submit"
    )
  ) {
    return (
      <TorrentUploadLayout>
        <UploadAccessCard
          icon={ShieldXIcon}
          title="当前账户不能提交种子"
          description="当前账户没有上传权限，请联系站点管理人员。"
        />
      </TorrentUploadLayout>
    )
  }

  return (
    <TorrentUploadLayout>
      <AuthorizedTorrentUpload csrfToken={session.data.csrf_token} />
    </TorrentUploadLayout>
  )
}

function AuthorizedTorrentUpload({ csrfToken }: { csrfToken: string }) {
  const categories = useCategoryList()

  if (categories.isPending) {
    return <TorrentUploadSkeleton />
  }
  if (categories.isError) {
    return (
      <UploadAccessCard
        icon={CircleAlertIcon}
        title="分类暂时无法读取"
        description="请稍后重试，分类恢复后即可继续填写。"
        action={
          <Button onClick={() => void categories.refetch()}>
            <RefreshCwIcon data-icon="inline-start" />
            重试
          </Button>
        }
      />
    )
  }
  if (!categories.data?.length) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>暂时不能提交</CardTitle>
          <CardDescription>当前没有可用的公开种子分类。</CardDescription>
        </CardHeader>
        <CardContent>
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <FileUpIcon />
              </EmptyMedia>
              <EmptyTitle>没有启用分类</EmptyTitle>
              <EmptyDescription>请稍后重新读取分类列表。</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button
                variant="outline"
                onClick={() => void categories.refetch()}
              >
                <RefreshCwIcon data-icon="inline-start" />
                重新读取
              </Button>
            </EmptyContent>
          </Empty>
        </CardContent>
      </Card>
    )
  }

  return (
    <TorrentUploadWorkspace
      categories={categories.data}
      csrfToken={csrfToken}
    />
  )
}

function TorrentUploadWorkspace({
  categories,
  csrfToken,
}: {
  categories: TorrentCategory[]
  csrfToken: string
}) {
  const submit = useSubmitTorrent()
  const [categoryId, setCategoryId] = React.useState<string | null>(
    categories[0]?.id ?? null
  )
  const categoryFacets = useCategoryFacets(
    categoryId ?? "",
    Boolean(categoryId)
  )
  const [facetSelections, setFacetSelections] = React.useState<
    Record<string, string[]>
  >({})
  const [facetErrors, setFacetErrors] = React.useState<Record<string, string>>(
    {}
  )
  const [torrentFile, setTorrentFile] = React.useState<File>()
  const [torrentDragActive, setTorrentDragActive] = React.useState(false)
  const [screenshots, setScreenshots] = React.useState<TorrentScreenshotItem[]>(
    []
  )
  const [screenshotError, setScreenshotError] = React.useState("")
  const [screenshotDragActive, setScreenshotDragActive] = React.useState(false)
  const [draggedScreenshotIndex, setDraggedScreenshotIndex] = React.useState<
    number | undefined
  >()
  const [screenshotDropIndex, setScreenshotDropIndex] = React.useState<
    number | undefined
  >()
  const [description, setDescription] = React.useState("")
  const [title, setTitle] = React.useState("")
  const [preview, setPreview] = React.useState<TorrentUploadPreview>()
  const [errors, setErrors] = React.useState<TorrentUploadFormErrors>({})
  const [progress, setProgress] = React.useState<TorrentUploadProgress>()
  const formRef = React.useRef<HTMLFormElement>(null)
  const idempotencyKey = React.useRef<string>(undefined)
  const screenshotsRef = React.useRef(screenshots)

  React.useEffect(() => {
    screenshotsRef.current = screenshots
  }, [screenshots])

  React.useEffect(
    () => () => {
      screenshotsRef.current.forEach((screenshot) => {
        URL.revokeObjectURL(screenshot.previewUrl)
      })
    },
    []
  )

  function resetAttempt() {
    idempotencyKey.current = undefined
    setProgress(undefined)
    if (submit.isError) {
      submit.reset()
    }
  }

  function selectTorrentFile(file: File | undefined) {
    setTorrentFile(file)
    setErrors((current) => ({ ...current, torrentFile: undefined }))
    resetAttempt()
  }

  function handleTorrentDrop(event: React.DragEvent<HTMLLabelElement>) {
    event.preventDefault()
    setTorrentDragActive(false)
    if (submit.isPending) return
    selectTorrentFile(event.dataTransfer.files.item(0) ?? undefined)
  }

  function addScreenshotFiles(files: FileList | File[]) {
    if (submit.isPending) return
    const incoming = Array.from(files)
    if (!incoming.length) return
    const remaining = 6 - screenshots.length
    if (remaining < 1) {
      setScreenshotError("最多只能上传 6 张截图")
      return
    }
    const accepted: TorrentScreenshotItem[] = []
    for (const file of incoming.slice(0, remaining)) {
      if (!torrentScreenshotTypes.has(file.type)) {
        setScreenshotError("截图仅支持 JPEG、PNG 或 WebP 格式")
        continue
      }
      if (file.size < 1 || file.size > 5 * 1024 * 1024) {
        setScreenshotError("单张原始截图不能超过 2 MiB")
        continue
      }
      if (
        screenshots.some(
          (item) =>
            item.file.name === file.name &&
            item.file.size === file.size &&
            item.file.lastModified === file.lastModified
        )
      ) {
        setScreenshotError("同一张截图不需要重复添加")
        continue
      }
      accepted.push({
        id: globalThis.crypto.randomUUID(),
        file,
        previewUrl: URL.createObjectURL(file),
      })
    }
    if (incoming.length > remaining) {
      setScreenshotError("最多只能上传 6 张截图")
    } else if (accepted.length) {
      setScreenshotError("")
    }
    if (accepted.length) {
      setScreenshots((current) => [...current, ...accepted])
      resetAttempt()
    }
  }

  function removeScreenshot(index: number) {
    setScreenshots((current) => {
      const removed = current[index]
      if (removed) URL.revokeObjectURL(removed.previewUrl)
      return current.filter((_, currentIndex) => currentIndex !== index)
    })
    setScreenshotError("")
    resetAttempt()
  }

  function moveScreenshot(from: number, to: number) {
    if (from === to) return
    setScreenshots((current) => {
      const next = [...current]
      const [moved] = next.splice(from, 1)
      if (!moved) return current
      next.splice(to, 0, moved)
      return next
    })
    resetAttempt()
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    submit.reset()
    const form = event.currentTarget
    const formData = new FormData(form)
    const result = torrentUploadFormSchema.safeParse({
      categoryId: categoryId ?? "",
      title: formData.get("title"),
      subtitle: formData.get("subtitle") ?? "",
      description,
      mediaInfo: formData.get("media-info") ?? "",
      anonymous: formData.has("anonymous"),
      imdbId: formData.get("imdb-id") ?? "",
      tmdbId: formData.get("tmdb-id") ?? "",
      doubanId: formData.get("douban-id") ?? "",
      torrentFile,
    })
    if (!result.success) {
      setErrors(torrentUploadFieldErrors(result.error))
      requestAnimationFrame(() => {
        form.querySelector<HTMLElement>("[aria-invalid='true']")?.focus()
      })
      return
    }

    const missingFacet = findMissingFacetRequirement(
      categoryFacets.data ?? [],
      facetSelections
    )
    if (missingFacet) {
      setFacetErrors({ [missingFacet.key]: `请选择${missingFacet.name}` })
      requestAnimationFrame(() => {
        form
          .querySelector<HTMLElement>(
            `[data-facet-id='${missingFacet.key}'] button`
          )
          ?.focus()
      })
      return
    }

    if (!screenshots.length) {
      setScreenshotError("请至少上传一张截图")
      requestAnimationFrame(() => {
        form.querySelector<HTMLElement>("#torrent-screenshots")?.focus()
      })
      return
    }

    setErrors({})
    setFacetErrors({})
    setScreenshotError("")
    setProgress({ phase: "uploading", percent: 0 })
    idempotencyKey.current ??= globalThis.crypto.randomUUID()
    try {
      await submit.mutateAsync({
        category_id: result.data.categoryId,
        title: result.data.title,
        subtitle: result.data.subtitle,
        description: result.data.description,
        media_info: result.data.mediaInfo,
        anonymous: result.data.anonymous,
        ...(result.data.imdbId ? { imdb_id: result.data.imdbId } : {}),
        ...(result.data.tmdbId ? { tmdb_id: result.data.tmdbId } : {}),
        ...(result.data.doubanId ? { douban_id: result.data.doubanId } : {}),
        facet_selections: buildFacetSelectionInputs(
          categoryFacets.data ?? [],
          facetSelections
        ),
        screenshots: screenshots.map((screenshot) => screenshot.file),
        torrent_file: result.data.torrentFile,
        csrfToken,
        idempotencyKey: idempotencyKey.current,
        onProgress: setProgress,
      })
    } catch (error) {
      if (
        error instanceof ApiProblemError &&
        (error.code === "torrent_upload_idempotency_conflict" ||
          error.code === "torrent_upload_expired")
      ) {
        idempotencyKey.current = undefined
      }
    }
  }

  if (submit.data) {
    return (
      <div className="flex flex-col gap-4">
        <TorrentSubmissionReceipt
          submission={submit.data}
          onReset={() => {
            submit.reset()
            setCategoryId(categories[0]?.id ?? null)
            setFacetSelections({})
            setFacetErrors({})
            setTorrentFile(undefined)
            screenshots.forEach((screenshot) =>
              URL.revokeObjectURL(screenshot.previewUrl)
            )
            setScreenshots([])
            setScreenshotError("")
            setDescription("")
            setTitle("")
            setErrors({})
            setProgress(undefined)
            idempotencyKey.current = undefined
          }}
        />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <Card className="gap-0 py-0">
        <CardContent className="flex min-h-[160px] min-w-0 items-center gap-2 p-4 sm:min-h-[60px]">
          <LinkIcon className="size-[18px] shrink-0 text-primary" />
          <span className="text-base font-normal text-muted-foreground">
            Announce URL:
          </span>
          <code className="min-w-0 flex-1 rounded-md bg-muted/55 px-2 py-1 text-xs leading-relaxed whitespace-normal sm:truncate sm:text-sm">
            专属 Tracker 地址在下载时安全注入，页面不会显示私有 passkey
          </code>
        </CardContent>
      </Card>
      <form
        ref={formRef}
        id="torrent-upload-form"
        className="flex min-w-0 flex-col gap-6"
        onSubmit={handleSubmit}
        onInput={resetAttempt}
        noValidate
      >
        {submit.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>{torrentUploadErrorTitle(submit.error)}</AlertTitle>
            <AlertDescription>
              {torrentUploadErrorDescription(submit.error)}
            </AlertDescription>
          </Alert>
        ) : null}

        <Card className="gap-0 py-0">
          <CardHeader className="p-6">
            <CardTitle className="text-2xl leading-none font-semibold tracking-tight">
              <h2>种子文件 *</h2>
            </CardTitle>
          </CardHeader>
          <CardContent className="p-6 pt-0">
            <FieldGroup>
              <Field data-invalid={Boolean(errors.torrentFile)}>
                <FieldLabel htmlFor="torrent-file" className="sr-only">
                  种子文件
                </FieldLabel>
                <label
                  htmlFor="torrent-file"
                  className={cn(
                    "flex min-h-[132px] cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed border-input bg-muted/20 p-8 text-center transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50 hover:bg-muted/40",
                    torrentDragActive && "border-primary bg-primary/5",
                    errors.torrentFile &&
                      "border-destructive/60 ring-3 ring-destructive/20",
                    submit.isPending && "pointer-events-none opacity-50"
                  )}
                  onDragEnter={(event) => {
                    event.preventDefault()
                    if (!submit.isPending) setTorrentDragActive(true)
                  }}
                  onDragOver={(event) => {
                    event.preventDefault()
                    if (!submit.isPending) setTorrentDragActive(true)
                  }}
                  onDragLeave={() => setTorrentDragActive(false)}
                  onDrop={handleTorrentDrop}
                >
                  <Input
                    id="torrent-file"
                    name="torrent-file"
                    className="sr-only"
                    type="file"
                    accept=".torrent,application/x-bittorrent"
                    aria-invalid={Boolean(errors.torrentFile)}
                    disabled={submit.isPending}
                    onClick={(event) => {
                      event.currentTarget.value = ""
                    }}
                    onChange={(event) => {
                      selectTorrentFile(event.currentTarget.files?.[0])
                    }}
                  />
                  {torrentFile ? (
                    <FileCheck2Icon className="size-7 text-primary" />
                  ) : (
                    <UploadIcon className="size-8 text-muted-foreground" />
                  )}
                  <span
                    className={cn(
                      "max-w-full text-base font-medium break-all",
                      torrentFile ? "text-primary" : "text-muted-foreground"
                    )}
                  >
                    {torrentFile
                      ? torrentFile.name
                      : "点击或拖拽上传 .torrent 文件"}
                  </span>
                  {torrentFile ? (
                    <span className="text-xs text-muted-foreground">
                      {formatBytes(torrentFile.size)} · 点击可重新选择
                    </span>
                  ) : null}
                </label>
                <FieldError
                  errors={
                    errors.torrentFile ? [{ message: errors.torrentFile }] : []
                  }
                />
              </Field>
            </FieldGroup>
          </CardContent>
        </Card>

        <Card className="gap-0 py-0">
          <CardHeader className="p-6">
            <CardTitle className="text-2xl leading-none font-semibold tracking-tight">
              <h2>基本信息</h2>
            </CardTitle>
          </CardHeader>
          <CardContent className="p-6 pt-0">
            <FieldGroup className="gap-4">
              <Field data-invalid={Boolean(errors.title)}>
                <FieldLabel htmlFor="torrent-title">
                  标题 <span className="text-destructive">*</span>
                </FieldLabel>
                <Input
                  id="torrent-title"
                  name="title"
                  value={title}
                  onChange={(event) => {
                    setTitle(event.currentTarget.value)
                    setErrors((current) => ({
                      ...current,
                      title: undefined,
                    }))
                  }}
                  maxLength={240}
                  placeholder="种子标题"
                  aria-invalid={Boolean(errors.title)}
                  disabled={submit.isPending}
                  className="h-10 rounded-md"
                />
                <FieldError
                  errors={errors.title ? [{ message: errors.title }] : []}
                />
              </Field>

              <Field data-invalid={Boolean(errors.subtitle)}>
                <FieldLabel htmlFor="torrent-subtitle">
                  副标题 <span className="text-destructive">*</span>
                </FieldLabel>
                <Input
                  id="torrent-subtitle"
                  name="subtitle"
                  maxLength={300}
                  placeholder="副标题"
                  aria-invalid={Boolean(errors.subtitle)}
                  disabled={submit.isPending}
                  className="h-10 rounded-md"
                />
                <FieldError
                  errors={errors.subtitle ? [{ message: errors.subtitle }] : []}
                />
              </Field>

              <Field data-invalid={Boolean(errors.categoryId)}>
                <FieldLabel htmlFor="torrent-category">
                  分类 <span className="text-destructive">*</span>
                </FieldLabel>
                <Select
                  items={categories.map((category) => ({
                    label: category.name,
                    value: category.id,
                  }))}
                  value={categoryId}
                  onValueChange={(value) => {
                    setCategoryId(value)
                    setFacetSelections({})
                    setFacetErrors({})
                    setErrors((current) => ({
                      ...current,
                      categoryId: undefined,
                    }))
                    resetAttempt()
                  }}
                  disabled={submit.isPending}
                >
                  <SelectTrigger
                    id="torrent-category"
                    className="h-10 w-full rounded-md"
                    aria-invalid={Boolean(errors.categoryId)}
                  >
                    <SelectValue placeholder="选择启用分类" />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectLabel>启用分类</SelectLabel>
                      {categories.map((category) => (
                        <SelectItem key={category.id} value={category.id}>
                          {category.name}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldError
                  errors={
                    errors.categoryId ? [{ message: errors.categoryId }] : []
                  }
                />
              </Field>

              <CategoryFacetFields
                facets={categoryFacets.data ?? []}
                selections={facetSelections}
                errors={facetErrors}
                isPending={categoryFacets.isPending}
                isError={categoryFacets.isError}
                disabled={submit.isPending}
                onRetry={() => void categoryFacets.refetch()}
                onValueChange={(facetId, optionKeys) => {
                  setFacetSelections((current) => ({
                    ...current,
                    [facetId]: optionKeys,
                  }))
                  setFacetErrors((current) => ({
                    ...current,
                    [facetId]: "",
                  }))
                  resetAttempt()
                }}
                onGroupValueChange={(group, facetIds, selection) => {
                  setFacetSelections((current) => {
                    const next = { ...current }
                    facetIds.forEach((facetId) => delete next[facetId])
                    if (selection) {
                      next[selection.facetId] = [selection.optionKey]
                    }
                    return next
                  })
                  setFacetErrors((current) => ({
                    ...current,
                    [`requirement:${group}`]: "",
                  }))
                  resetAttempt()
                }}
              >
                <ExternalIdentifierField
                  id="torrent-imdb-id"
                  name="imdb-id"
                  provider="imdb"
                  label="IMDb"
                  placeholder="填入 IMDb 链接"
                  error={errors.imdbId}
                  disabled={submit.isPending}
                />
                <ExternalIdentifierField
                  id="torrent-tmdb-id"
                  name="tmdb-id"
                  provider="tmdb"
                  label="TMDB"
                  placeholder="填入 TMDB 链接"
                  error={errors.tmdbId}
                  disabled={submit.isPending}
                />
                <ExternalIdentifierField
                  id="torrent-douban-id"
                  name="douban-id"
                  provider="douban"
                  label="豆瓣"
                  placeholder="填入豆瓣链接"
                  error={errors.doubanId}
                  disabled={submit.isPending}
                />
              </CategoryFacetFields>

              <Field data-invalid={Boolean(screenshotError)}>
                <FieldLabel className="flex flex-wrap items-center gap-2">
                  <ImageIcon className="size-4" />
                  截图 <span className="text-destructive">*</span>
                  <span className="font-normal text-muted-foreground">
                    （最多 6 张，拖拽调整顺序，第一张为封面 单张图片不能超过 2
                    MiB）
                  </span>
                </FieldLabel>
                {screenshots.length ? (
                  <div className="mb-1 grid grid-cols-3 gap-3 md:grid-cols-6">
                    {screenshots.map((screenshot, index) => (
                      <div
                        key={screenshot.id}
                        draggable={!submit.isPending}
                        className={cn(
                          "group relative aspect-video cursor-move overflow-hidden rounded-lg border-2 transition-all",
                          index === 0 ? "border-primary" : "border-transparent",
                          screenshotDropIndex === index &&
                            "scale-105 border-dashed border-primary",
                          draggedScreenshotIndex === index && "opacity-50"
                        )}
                        onDragStart={() => setDraggedScreenshotIndex(index)}
                        onDragEnd={() => {
                          setDraggedScreenshotIndex(undefined)
                          setScreenshotDropIndex(undefined)
                        }}
                        onDragOver={(event) => {
                          event.preventDefault()
                          setScreenshotDropIndex(index)
                        }}
                        onDragLeave={() => setScreenshotDropIndex(undefined)}
                        onDrop={(event) => {
                          event.preventDefault()
                          if (draggedScreenshotIndex !== undefined) {
                            moveScreenshot(draggedScreenshotIndex, index)
                          }
                          setDraggedScreenshotIndex(undefined)
                          setScreenshotDropIndex(undefined)
                        }}
                      >
                        <img
                          src={screenshot.previewUrl}
                          alt={`截图 ${index + 1}`}
                          className="pointer-events-none size-full object-cover"
                        />
                        <span className="absolute top-1 right-1 rounded bg-black/50 p-0.5 opacity-0 transition-opacity group-hover:opacity-100">
                          <GripVerticalIcon className="size-3.5 text-white" />
                        </span>
                        {index === 0 ? (
                          <span className="absolute top-1 left-1 flex items-center gap-1 rounded bg-primary px-1.5 py-0.5 text-xs text-primary-foreground">
                            <StarIcon className="size-2.5 fill-current" />
                            封面
                          </span>
                        ) : null}
                        <span className="absolute bottom-1 left-1 rounded bg-black/50 px-1.5 py-0.5 text-xs text-white">
                          {index + 1}
                        </span>
                        <span className="absolute inset-0 flex items-center justify-center bg-black/50 opacity-0 transition-opacity group-hover:opacity-100">
                          <Button
                            type="button"
                            size="icon-sm"
                            variant="destructive"
                            aria-label={`移除截图 ${index + 1}`}
                            disabled={submit.isPending}
                            onClick={() => removeScreenshot(index)}
                          >
                            <XIcon />
                          </Button>
                        </span>
                      </div>
                    ))}
                  </div>
                ) : null}
                {screenshots.length < 6 ? (
                  <label
                    htmlFor="torrent-screenshots"
                    data-screenshot-upload
                    className={cn(
                      "flex min-h-[132px] cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed border-input bg-muted/20 p-8 text-center text-muted-foreground transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50 hover:bg-muted/40",
                      screenshotDragActive && "border-primary bg-primary/5",
                      screenshotError &&
                        "border-destructive/60 ring-3 ring-destructive/20",
                      submit.isPending && "pointer-events-none opacity-50"
                    )}
                    onDragEnter={(event) => {
                      event.preventDefault()
                      if (!submit.isPending) setScreenshotDragActive(true)
                    }}
                    onDragOver={(event) => {
                      event.preventDefault()
                      if (!submit.isPending) setScreenshotDragActive(true)
                    }}
                    onDragLeave={() => setScreenshotDragActive(false)}
                    onDrop={(event) => {
                      event.preventDefault()
                      setScreenshotDragActive(false)
                      addScreenshotFiles(event.dataTransfer.files)
                    }}
                  >
                    <Input
                      id="torrent-screenshots"
                      className="sr-only"
                      type="file"
                      accept="image/jpeg,image/png,image/webp"
                      multiple
                      aria-label="截图"
                      aria-invalid={Boolean(screenshotError)}
                      disabled={submit.isPending}
                      onClick={(event) => {
                        event.currentTarget.value = ""
                      }}
                      onChange={(event) => {
                        if (event.currentTarget.files) {
                          addScreenshotFiles(event.currentTarget.files)
                        }
                      }}
                    />
                    <ImageIcon className="size-8" />
                    <span className="text-base">点击或拖拽上传截图</span>
                  </label>
                ) : null}
                <FieldError
                  errors={screenshotError ? [{ message: screenshotError }] : []}
                />
              </Field>

              <Field data-invalid={Boolean(errors.description)}>
                <FieldLabel htmlFor="torrent-description">
                  描述 <span className="text-destructive">*</span>
                </FieldLabel>
                <TorrentMarkdownEditor
                  id="torrent-description"
                  value={description}
                  onValueChange={(value) => {
                    setDescription(value)
                    setErrors((current) => ({
                      ...current,
                      description: undefined,
                    }))
                    resetAttempt()
                  }}
                  invalid={Boolean(errors.description)}
                  disabled={submit.isPending}
                />
                <FieldError
                  errors={
                    errors.description ? [{ message: errors.description }] : []
                  }
                />
              </Field>

              <Field data-invalid={Boolean(errors.mediaInfo)}>
                <FieldLabel htmlFor="torrent-media-info">
                  MediaInfo/BDInfo <span className="text-destructive">*</span>
                </FieldLabel>
                <Textarea
                  id="torrent-media-info"
                  name="media-info"
                  placeholder="粘贴 MediaInfo 或 BDInfo 信息"
                  rows={4}
                  aria-invalid={Boolean(errors.mediaInfo)}
                  disabled={submit.isPending}
                  className="min-h-[82px] resize-y rounded-md font-mono text-xs"
                />
                <FieldError
                  errors={
                    errors.mediaInfo ? [{ message: errors.mediaInfo }] : []
                  }
                />
              </Field>

              {submit.isPending && progress ? (
                <Progress value={progress.percent} aria-live="polite">
                  <ProgressLabel>
                    {progress.phase === "uploading"
                      ? "正在上传种子文件"
                      : "正在检查种子文件"}
                  </ProgressLabel>
                  <ProgressValue />
                </Progress>
              ) : null}
            </FieldGroup>
          </CardContent>
        </Card>

        <Card className="gap-0 py-0">
          <CardContent className="flex flex-col gap-4 p-4">
            <Field orientation="horizontal">
              <Checkbox
                id="torrent-anonymous"
                name="anonymous"
                disabled={submit.isPending}
              />
              <FieldLabel htmlFor="torrent-anonymous" className="font-normal">
                匿名上传
              </FieldLabel>
            </Field>
          </CardContent>
        </Card>

        <div className="flex justify-end gap-3">
          <Button
            type="button"
            variant="outline"
            size="lg"
            className="w-30"
            disabled={
              submit.isPending || (!title.trim() && !description.trim())
            }
            onClick={() => {
              const form = formRef.current
              if (!form) return
              const formData = new FormData(form)
              setPreview({
                title: String(formData.get("title") ?? "").trim(),
                subtitle: String(formData.get("subtitle") ?? "").trim(),
                category:
                  categories.find((category) => category.id === categoryId)
                    ?.name ?? "未分类",
                description,
                mediaInfo: String(formData.get("media-info") ?? "").trim(),
                anonymous: formData.has("anonymous"),
                screenshots: screenshots.map((screenshot) => ({
                  id: screenshot.id,
                  previewUrl: screenshot.previewUrl,
                })),
                facets: buildFacetPreview(
                  categoryFacets.data ?? [],
                  facetSelections
                ),
              })
            }}
          >
            <EyeIcon data-icon="inline-start" />
            预览
          </Button>
          <Button
            type="submit"
            size="lg"
            className="w-30"
            disabled={
              submit.isPending ||
              categoryFacets.isPending ||
              categoryFacets.isError
            }
          >
            {submit.isPending ? (
              <>
                <Spinner data-icon="inline-start" />
                {progress?.phase === "processing" ? "正在验证…" : "正在上传…"}
              </>
            ) : (
              "上传种子"
            )}
          </Button>
        </div>

        <Card className="gap-0 py-0">
          <CardContent className="p-4 text-sm text-muted-foreground">
            Tracker 地址由 PeerGo 在下载时自动写入。上传失败时可直接重试，
            已填写内容会保留。
          </CardContent>
        </Card>
      </form>
      <TorrentUploadPreviewDialog
        preview={preview}
        onOpenChange={(open) => {
          if (!open) setPreview(undefined)
        }}
      />
    </div>
  )
}

type TorrentUploadPreview = {
  title: string
  subtitle: string
  category: string
  description: string
  mediaInfo: string
  anonymous: boolean
  screenshots: Array<{ id: string; previewUrl: string }>
  facets: Array<{ name: string; values: string[] }>
}

type TorrentScreenshotItem = {
  id: string
  file: File
  previewUrl: string
}

const torrentScreenshotTypes = new Set([
  "image/jpeg",
  "image/png",
  "image/webp",
])

function TorrentUploadPreviewDialog({
  preview,
  onOpenChange,
}: {
  preview?: TorrentUploadPreview
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={Boolean(preview)} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-hidden sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>种子预览</DialogTitle>
          <DialogDescription>
            预览只使用当前表单内容，不会上传文件或公开发布。
          </DialogDescription>
        </DialogHeader>
        {preview ? (
          <div className="flex min-h-0 flex-col gap-4 overflow-y-auto pr-1">
            <Card className="gap-0 py-0">
              <CardHeader className="p-6 pb-2">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant="outline">{preview.category}</Badge>
                  {preview.anonymous ? <Badge>匿名上传</Badge> : null}
                </div>
                <CardTitle className="text-2xl font-semibold break-words">
                  {preview.title || "未填写标题"}
                </CardTitle>
                {preview.subtitle ? (
                  <CardDescription>{preview.subtitle}</CardDescription>
                ) : null}
                {preview.facets.length ? (
                  <dl className="mt-2 grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
                    {preview.facets.map((facet) => (
                      <div key={facet.name} className="flex min-w-0 gap-2">
                        <dt className="shrink-0 text-muted-foreground">
                          {facet.name}:
                        </dt>
                        <dd className="truncate">{facet.values.join(" / ")}</dd>
                      </div>
                    ))}
                  </dl>
                ) : null}
              </CardHeader>
            </Card>
            {preview.screenshots.length ? (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                {preview.screenshots.map((screenshot, index) => (
                  <div
                    key={screenshot.id}
                    className="relative aspect-video overflow-hidden rounded-lg border bg-muted"
                  >
                    <img
                      src={screenshot.previewUrl}
                      alt={`截图 ${index + 1}`}
                      className="size-full object-cover"
                    />
                    {index === 0 ? (
                      <Badge className="absolute top-2 left-2">封面</Badge>
                    ) : null}
                  </div>
                ))}
              </div>
            ) : null}
            {preview.mediaInfo ? (
              <TorrentMediaInfoCard mediaInfo={preview.mediaInfo} />
            ) : null}
            {preview.description ? (
              <TorrentDescriptionCard
                description={preview.description}
                format="markdown"
              />
            ) : null}
            {!preview.description && !preview.mediaInfo ? (
              <Alert>
                <CircleAlertIcon />
                <AlertTitle>还没有可预览的正文</AlertTitle>
                <AlertDescription>
                  填写描述或 MediaInfo 后，这里会按详情页样式显示。
                </AlertDescription>
              </Alert>
            ) : null}
          </div>
        ) : null}
        <DialogFooter showCloseButton />
      </DialogContent>
    </Dialog>
  )
}

function CategoryFacetFields({
  facets,
  selections,
  errors,
  isPending,
  isError,
  disabled,
  onRetry,
  onValueChange,
  onGroupValueChange,
  children,
}: {
  facets: TorrentCategoryFacet[]
  selections: Record<string, string[]>
  errors: Record<string, string>
  isPending: boolean
  isError: boolean
  disabled: boolean
  onRetry: () => void
  onValueChange: (facetId: string, optionKeys: string[]) => void
  onGroupValueChange: (
    group: string,
    facetIds: string[],
    selection?: { facetId: string; optionKey: string }
  ) => void
  children: React.ReactNode
}) {
  const presentations = buildFacetPresentations(facets)

  return (
    <div
      className="grid grid-cols-1 gap-4 rounded-lg bg-muted/30 p-4 md:grid-cols-2"
      aria-label={isPending ? "正在读取分类属性" : undefined}
    >
      {isPending ? (
        <>
          <div className="flex flex-col gap-2 md:col-span-2">
            <Skeleton className="h-4 w-12" />
            <div className="flex flex-wrap gap-2">
              <Skeleton className="h-8 w-16 rounded-full" />
              <Skeleton className="h-8 w-20 rounded-full" />
              <Skeleton className="h-8 w-14 rounded-full" />
            </div>
          </div>
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </>
      ) : null}

      {isError ? (
        <Alert variant="destructive" className="md:col-span-2">
          <CircleAlertIcon />
          <AlertTitle>分类属性暂时无法读取</AlertTitle>
          <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
            <span>请重新读取后再提交，避免遗漏这个分类的必填属性。</span>
            <Button type="button" variant="outline" size="sm" onClick={onRetry}>
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      {!isPending && !isError
        ? presentations.map((presentation) => {
            if (presentation.kind === "group") {
              const error = errors[presentation.key]
              const options = presentation.members.flatMap((facet) =>
                facet.options.map((option) => ({
                  facetId: facet.id,
                  optionKey: option.key,
                  label: option.label,
                  value: groupedFacetOptionValue(facet.id, option.key),
                }))
              )
              const uniqueOptions = options.filter(
                (option, index) =>
                  options.findIndex(
                    (candidate) => candidate.label === option.label
                  ) === index
              )
              uniqueOptions.sort(
                (left, right) =>
                  groupedFacetOptionOrder(presentation.group, left.label) -
                  groupedFacetOptionOrder(presentation.group, right.label)
              )
              const selected = uniqueOptions.find((option) =>
                selections[option.facetId]?.includes(option.optionKey)
              )
              const facetIds = presentation.members.map((facet) => facet.id)
              return (
                <Field
                  key={presentation.key}
                  data-facet-id={presentation.key}
                  data-invalid={Boolean(error)}
                >
                  <FieldLabel htmlFor={`torrent-facet-${presentation.group}`}>
                    {requiredFacetLabel(presentation.name, true)}
                  </FieldLabel>
                  <Select
                    items={uniqueOptions.map((option) => ({
                      label: option.label,
                      value: option.value,
                    }))}
                    value={selected?.value ?? null}
                    onValueChange={(value) => {
                      const option = uniqueOptions.find(
                        (candidate) => candidate.value === value
                      )
                      onGroupValueChange(
                        presentation.group,
                        facetIds,
                        option
                          ? {
                              facetId: option.facetId,
                              optionKey: option.optionKey,
                            }
                          : undefined
                      )
                    }}
                    disabled={disabled}
                  >
                    <SelectTrigger
                      id={`torrent-facet-${presentation.group}`}
                      className="h-10 w-full rounded-md"
                      aria-invalid={Boolean(error)}
                    >
                      <SelectValue placeholder="请选择" />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {uniqueOptions.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldError errors={error ? [{ message: error }] : []} />
                </Field>
              )
            }

            const facet = presentation.facet
            const selected = selections[facet.id] ?? []
            const error = errors[facet.id]
            const label = requiredFacetLabel(facet.name, facet.required)

            if (facet.selection_mode === "multi_option") {
              return (
                <FieldSet
                  key={facet.id}
                  data-facet-id={facet.id}
                  data-invalid={Boolean(error)}
                  className="gap-2 md:col-span-2"
                >
                  <FieldLegend
                    id={`torrent-facet-${facet.id}-label`}
                    variant="label"
                    className="mb-2"
                  >
                    {label}
                  </FieldLegend>
                  <ToggleGroup
                    multiple
                    value={selected}
                    onValueChange={(values) => onValueChange(facet.id, values)}
                    variant="outline"
                    className="w-full flex-wrap justify-start"
                    disabled={disabled}
                    aria-labelledby={`torrent-facet-${facet.id}-label`}
                    aria-invalid={Boolean(error)}
                  >
                    {facet.options.map((option) => {
                      const isSelected = selected.includes(option.key)
                      return (
                        <ToggleGroupItem
                          key={option.key}
                          value={option.key}
                          className="h-auto rounded-full border-input bg-background px-3 py-1.5 hover:border-primary/50 hover:bg-accent aria-pressed:border-primary aria-pressed:bg-primary aria-pressed:text-primary-foreground"
                        >
                          {isSelected ? (
                            <CheckIcon className="size-3.5" />
                          ) : null}
                          {option.label}
                        </ToggleGroupItem>
                      )
                    })}
                  </ToggleGroup>
                  <FieldError errors={error ? [{ message: error }] : []} />
                </FieldSet>
              )
            }

            return (
              <Field
                key={facet.id}
                data-facet-id={facet.id}
                data-invalid={Boolean(error)}
              >
                <FieldLabel htmlFor={`torrent-facet-${facet.id}`}>
                  {label}
                </FieldLabel>
                <Select
                  items={facet.options.map((option) => ({
                    label: option.label,
                    value: option.key,
                  }))}
                  value={selected[0] ?? null}
                  onValueChange={(value) =>
                    onValueChange(facet.id, value ? [value] : [])
                  }
                  disabled={disabled}
                >
                  <SelectTrigger
                    id={`torrent-facet-${facet.id}`}
                    className="h-10 w-full rounded-md"
                    aria-invalid={Boolean(error)}
                  >
                    <SelectValue placeholder="请选择" />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {facet.options.map((option) => (
                        <SelectItem key={option.key} value={option.key}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldError errors={error ? [{ message: error }] : []} />
              </Field>
            )
          })
        : null}

      {children}
    </div>
  )
}

type CategoryFacetPresentation =
  | {
      kind: "facet"
      key: string
      facet: TorrentCategoryFacet
    }
  | {
      kind: "group"
      key: string
      group: string
      name: string
      members: TorrentCategoryFacet[]
    }

function buildFacetPresentations(facets: TorrentCategoryFacet[]) {
  const result: CategoryFacetPresentation[] = []
  const groups = new Map<
    string,
    Extract<CategoryFacetPresentation, { kind: "group" }>
  >()
  for (const facet of facets) {
    const group = facet.requirement_group
    if (!group) {
      result.push({ kind: "facet", key: facet.id, facet })
      continue
    }
    const existing = groups.get(group)
    if (existing) {
      existing.members.push(facet)
      continue
    }
    const presentation: Extract<CategoryFacetPresentation, { kind: "group" }> =
      {
        kind: "group",
        key: `requirement:${group}`,
        group,
        name: facet.name,
        members: [facet],
      }
    groups.set(group, presentation)
    result.push(presentation)
  }
  return result
}

function requiredFacetLabel(name: string, required: boolean) {
  return (
    <>
      {name}
      {required ? (
        <>
          {" "}
          <span className="text-destructive">*</span>
        </>
      ) : null}
    </>
  )
}

function groupedFacetOptionValue(facetId: string, optionKey: string) {
  return `${facetId}::${optionKey}`
}

const sourceOptionOrder = new Map(
  ["Blu-ray", "UHD Blu-ray", "WEB-DL", "HDTV", "DVDRip", "CAM", "其它"].map(
    (label, index) => [label, index] as const
  )
)

function groupedFacetOptionOrder(group: string, label: string) {
  if (group !== "source") return 0
  return sourceOptionOrder.get(label) ?? sourceOptionOrder.size
}

function findMissingFacetRequirement(
  facets: TorrentCategoryFacet[],
  selections: Record<string, string[]>
) {
  const missingFacet = facets.find(
    (facet) => facet.required && !(selections[facet.id]?.length ?? 0)
  )
  if (missingFacet) {
    return { key: missingFacet.id, name: missingFacet.name }
  }

  const groups = new Map<string, TorrentCategoryFacet[]>()
  for (const facet of facets) {
    if (!facet.requirement_group) continue
    const members = groups.get(facet.requirement_group) ?? []
    members.push(facet)
    groups.set(facet.requirement_group, members)
  }
  for (const [group, members] of groups) {
    const satisfied = members.some(
      (facet) => (selections[facet.id]?.length ?? 0) > 0
    )
    if (!satisfied) {
      return { key: `requirement:${group}`, name: members[0]?.name ?? group }
    }
  }
  return undefined
}

function buildFacetSelectionInputs(
  facets: TorrentCategoryFacet[],
  selections: Record<string, string[]>
) {
  return facets.flatMap((facet) => {
    const allowed = new Set(facet.options.map((option) => option.key))
    const optionKeys = (selections[facet.id] ?? []).filter((key) =>
      allowed.has(key)
    )
    return optionKeys.length
      ? [{ facet_id: facet.id, option_keys: optionKeys }]
      : []
  })
}

function buildFacetPreview(
  facets: TorrentCategoryFacet[],
  selections: Record<string, string[]>
) {
  return facets.flatMap((facet) => {
    const selected = new Set(selections[facet.id] ?? [])
    const values = facet.options
      .filter((option) => selected.has(option.key))
      .map((option) => option.label)
    return values.length ? [{ name: facet.name, values }] : []
  })
}

function ExternalIdentifierField({
  id,
  name,
  provider,
  label,
  placeholder,
  error,
  disabled,
}: {
  id: string
  name: string
  provider: Parameters<typeof parseExternalIdentifier>[0]
  label: string
  placeholder: string
  error?: string
  disabled: boolean
}) {
  const [value, setValue] = React.useState("")
  const [parseError, setParseError] = React.useState("")
  const visibleError = error ?? parseError

  return (
    <Field data-invalid={Boolean(visibleError)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <div className="flex gap-2">
        <Input
          id={id}
          name={name}
          value={value}
          onChange={(event) => {
            setValue(event.currentTarget.value)
            setParseError("")
          }}
          maxLength={300}
          placeholder={placeholder}
          aria-invalid={Boolean(visibleError)}
          disabled={disabled}
          className="h-10 min-w-0 rounded-md"
        />
        <Button
          type="button"
          variant="outline"
          className="shrink-0"
          aria-label={`解析 ${label}`}
          disabled={disabled || !value.trim()}
          onClick={() => {
            const parsed = parseExternalIdentifier(provider, value)
            if (!parsed) {
              setParseError(`请输入有效的${label}链接或编号`)
              return
            }
            setValue(parsed)
            setParseError("")
          }}
        >
          解析
        </Button>
      </div>
      <FieldError errors={visibleError ? [{ message: visibleError }] : []} />
    </Field>
  )
}

function TorrentSubmissionReceipt({
  submission,
  onReset,
}: {
  submission: TorrentSubmission
  onReset: () => void
}) {
  return (
    <Card size="sm" aria-labelledby="torrent-submission-receipt-title">
      <CardHeader>
        <CardTitle>
          <h2 id="torrent-submission-receipt-title">已进入审核队列</h2>
        </CardTitle>
        <CardDescription>审核通过后，种子会出现在公开列表中。</CardDescription>
        <CardAction>
          <Badge>待审核</Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Alert>
          <CircleCheckIcon />
          <AlertTitle>提交成功</AlertTitle>
          <AlertDescription>
            种子已保存并等待审核，目前还没有公开发布。
          </AlertDescription>
        </Alert>
        <dl className="grid gap-4 sm:grid-cols-2">
          <ReceiptFact label="内容名称" value={submission.content_name} />
          <ReceiptFact
            label="内容大小"
            value={formatBytes(submission.total_size_bytes)}
          />
          <ReceiptFact
            label="文件数量"
            value={`${submission.file_count.toLocaleString("zh-CN")} 个`}
          />
          <ReceiptFact
            label="提交时间"
            value={formatDateTime(submission.submitted_at)}
          />
        </dl>
      </CardContent>
      <CardFooter className="justify-between gap-3 bg-transparent">
        <Button
          variant="outline"
          nativeButton={false}
          render={<Link to="/account/submissions" />}
        >
          <FileCheck2Icon data-icon="inline-start" />
          查看我的发布
        </Button>
        <Button onClick={onReset}>
          <FileUpIcon data-icon="inline-start" />
          继续上传
        </Button>
      </CardFooter>
    </Card>
  )
}

function ReceiptFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 font-medium break-words">{value}</dd>
    </div>
  )
}

function TorrentUploadLayout({ children }: { children: React.ReactNode }) {
  return (
    <PageLayout className="gap-6">
      <PageHeader title="上传种子" />
      {children}
    </PageLayout>
  )
}

function UploadAccessCard({
  icon: Icon,
  title,
  description,
  action,
}: {
  icon: React.ComponentType
  title: string
  description: string
  action?: React.ReactNode
}) {
  return (
    <Card className="max-w-2xl">
      <CardContent>
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Icon />
            </EmptyMedia>
            <EmptyTitle>{title}</EmptyTitle>
            <EmptyDescription>{description}</EmptyDescription>
          </EmptyHeader>
          {action ? <EmptyContent>{action}</EmptyContent> : null}
        </Empty>
      </CardContent>
    </Card>
  )
}

function TorrentUploadSkeleton() {
  return (
    <div
      className="flex flex-col gap-4"
      aria-label="正在准备种子上传页面"
      aria-busy="true"
    >
      <Skeleton className="h-16 w-full rounded-xl" />
      <div className="flex flex-col gap-4">
        <Card size="sm">
          <CardHeader>
            <Skeleton className="h-5 w-28" />
            <Skeleton className="h-4 w-64 max-w-full" />
          </CardHeader>
          <CardContent>
            <Skeleton className="h-36 w-full" />
          </CardContent>
        </Card>
        <Card size="sm">
          <CardHeader>
            <Skeleton className="h-5 w-28" />
            <Skeleton className="h-4 w-80 max-w-full" />
          </CardHeader>
          <CardContent className="flex flex-col gap-5">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-20 w-full" />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function torrentUploadErrorTitle(error: Error) {
  return error instanceof ApiProblemError ? error.message : "提交失败"
}

function torrentUploadErrorDescription(error: Error) {
  if (!(error instanceof ApiProblemError)) {
    return error.message || "请检查网络后使用当前页面重试。"
  }
  switch (error.code) {
    case "invalid_torrent_submission":
      return "种子文件不符合要求，请确认它是已设置为私有的纯 v1 种子。"
    case "torrent_upload_too_large":
      return "文件超过当前站点配置的上传限制，请重新制作体积更小的 .torrent。"
    case "torrent_category_unavailable":
      return "所选分类刚刚被停用，请重新读取页面后选择其他分类。"
    case "torrent_already_exists":
      return "相同内容已经提交，无需再次上传。"
    case "torrent_upload_idempotency_conflict":
      return "本次内容与上一次尝试不同，请核对后重新提交。"
    case "torrent_upload_expired":
      return "上一次提交已经失效，可以重新提交。"
    case "torrent_upload_state_conflict":
      return "这次提交仍在处理中，请保留表单并稍后重试。"
    case "torrent_storage_unavailable":
      return "文件存储暂时不可用，请保留表单并稍后重试。"
    case "csrf_invalid":
      return "会话验证已变化，请刷新页面后重新提交。"
    case "verified_email_required":
      return "当前账户尚未完成邮箱验证。"
    case "torrent_submit_denied":
      return "当前账户没有上传权限。"
    default:
      return error.requestId
        ? `请稍后重试。请求编号：${error.requestId}`
        : "请稍后重试。"
  }
}
