import * as React from "react"
import { useBeforeUnload, useBlocker } from "react-router"
import { useQuery } from "@tanstack/react-query"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CircleAlertIcon,
  CircleCheckIcon,
  ClipboardCheckIcon,
  ExternalLinkIcon,
  Globe2Icon,
  Grid2X2Icon,
  ListIcon,
  MonitorCogIcon,
  PlusIcon,
  RefreshCwIcon,
  SaveIcon,
  Settings2Icon,
  Trash2Icon,
  TriangleAlertIcon,
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
  AlertDialogMedia,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type CustomNavigationItem,
  type SiteDisplaySettings,
  siteDisplaySettingsQueryOptions,
  useUpdateSiteDisplaySettings,
} from "~/features/staff/api/site-display-settings.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import {
  type SiteDisplaySettingsBusinessValues,
  type SiteDisplaySettingsFormField,
  type SiteDisplaySettingsFormValues,
  hasSiteDisplaySettingsChanges,
  siteDisplaySettingsDiff,
  siteDisplaySettingsFormSchema,
} from "~/features/staff/model/site-display-settings-form"
import type { components } from "~/generated/api"
import { ApiProblemError } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

type CapabilityList = components["schemas"]["CapabilityList"]
type FormErrors = Partial<Record<SiteDisplaySettingsFormField | "form", string>>

export function StaffSiteSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="site.display.manage.read"
      pageHeader={{
        title: "站点设置",
        description: "管理站点基本信息、下载文件名、首页展示与自定义菜单。",
      }}
    >
      {({ session, capabilities }) => (
        <SiteSettingsContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function SiteSettingsContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const settings = useQuery(siteDisplaySettingsQueryOptions)
  const canUpdate = hasCapability(capabilities, "site.display.update")

  if (settings.isPending) {
    return <SiteSettingsSkeleton />
  }
  if (settings.isError || !settings.data) {
    return (
      <SettingsFrame>
        <SiteSettingsCard saveAction={<DisabledSaveButton />}>
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>站点与展示设置暂时无法读取</AlertTitle>
            <AlertDescription>
              暂时无法取得当前设置，请稍后重试。
            </AlertDescription>
            <AlertAction>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void settings.refetch()}
              >
                <RefreshCwIcon data-icon="inline-start" />
                重试
              </Button>
            </AlertAction>
          </Alert>
        </SiteSettingsCard>
      </SettingsFrame>
    )
  }

  return (
    <SettingsFrame>
      <SiteDisplaySettingsForm
        initialSettings={settings.data}
        csrfToken={csrfToken}
        canUpdate={canUpdate}
      />
    </SettingsFrame>
  )
}

function SiteDisplaySettingsForm({
  initialSettings,
  csrfToken,
  canUpdate,
}: {
  initialSettings: SiteDisplaySettings
  csrfToken: string
  canUpdate: boolean
}) {
  const mutation = useUpdateSiteDisplaySettings()
  const [baseline, setBaseline] = React.useState(initialSettings)
  const [name, setName] = React.useState(initialSettings.name)
  const [description, setDescription] = React.useState(
    initialSettings.description
  )
  const [torrentFilenamePrefix, setTorrentFilenamePrefix] = React.useState(
    initialSettings.torrent_filename_prefix
  )
  const [defaultView, setDefaultView] = React.useState<"list" | "poster">(
    initialSettings.default_torrent_view
  )
  const [showLatestAnnouncement, setShowLatestAnnouncement] = React.useState(
    initialSettings.show_latest_announcement
  )
  const [customNavigationItems, setCustomNavigationItems] = React.useState(() =>
    initialSettings.custom_navigation_items.map((item) => ({ ...item }))
  )
  const [reason, setReason] = React.useState("")
  const [errors, setErrors] = React.useState<FormErrors>({})
  const [pendingValues, setPendingValues] =
    React.useState<SiteDisplaySettingsFormValues>()
  const [confirmationOpen, setConfirmationOpen] = React.useState(false)
  const [successMessage, setSuccessMessage] = React.useState("")

  React.useEffect(() => {
    if (mutation.isPending || initialSettings.version === baseline.version) {
      return
    }
    // A conflict refetch replaces the whole bounded section. Keeping any
    // field from the stale draft would recreate the silent-merge behavior the
    // expected_version contract is designed to prevent.
    setBaseline(initialSettings)
    setName(initialSettings.name)
    setDescription(initialSettings.description)
    setTorrentFilenamePrefix(initialSettings.torrent_filename_prefix)
    setDefaultView(initialSettings.default_torrent_view)
    setShowLatestAnnouncement(initialSettings.show_latest_announcement)
    setCustomNavigationItems(
      initialSettings.custom_navigation_items.map((item) => ({ ...item }))
    )
    setReason("")
    setPendingValues(undefined)
    setConfirmationOpen(false)
  }, [baseline.version, initialSettings, mutation.isPending])

  const businessValues: SiteDisplaySettingsBusinessValues = {
    name,
    description,
    torrentFilenamePrefix,
    defaultTorrentView: defaultView,
    showLatestAnnouncement,
    customNavigationItems,
  }
  const hasBusinessChanges = hasSiteDisplaySettingsChanges(
    baseline,
    businessValues
  )
  const dirty = hasBusinessChanges || reason.trim().length > 0
  const blocker = useBlocker(
    React.useCallback(
      () => dirty && !mutation.isPending,
      [dirty, mutation.isPending]
    )
  )
  useBeforeUnload(
    React.useCallback(
      (event) => {
        if (dirty && !mutation.isPending) {
          event.preventDefault()
          event.returnValue = ""
        }
      },
      [dirty, mutation.isPending]
    )
  )

  function handleReview(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const formElement = event.currentTarget
    mutation.reset()
    setSuccessMessage("")
    const result = siteDisplaySettingsFormSchema.safeParse({
      ...businessValues,
      reason,
    })
    if (!result.success) {
      const nextErrors: FormErrors = {}
      for (const issue of result.error.issues) {
        const field = issue.path[0]
        if (
          typeof field === "string" &&
          !nextErrors[field as SiteDisplaySettingsFormField]
        ) {
          nextErrors[field as SiteDisplaySettingsFormField] = issue.message
        }
      }
      setErrors(nextErrors)
      requestAnimationFrame(() => {
        formElement.querySelector<HTMLElement>("[aria-invalid='true']")?.focus()
      })
      return
    }
    if (!hasSiteDisplaySettingsChanges(baseline, result.data)) {
      setErrors({ form: "站点设置字段均未变化，无需创建空版本。" })
      return
    }
    setErrors({})
    setPendingValues(result.data)
    setConfirmationOpen(true)
  }

  async function handleConfirm() {
    if (!pendingValues) {
      return
    }
    try {
      const updated = await mutation.mutateAsync({
        csrfToken,
        body: {
          name: pendingValues.name,
          description: pendingValues.description,
          torrent_filename_prefix: pendingValues.torrentFilenamePrefix,
          default_torrent_view: pendingValues.defaultTorrentView,
          show_latest_announcement: pendingValues.showLatestAnnouncement,
          custom_navigation_items: pendingValues.customNavigationItems,
          expected_version: baseline.version,
          reason: pendingValues.reason,
        },
      })
      setBaseline(updated)
      setName(updated.name)
      setDescription(updated.description)
      setTorrentFilenamePrefix(updated.torrent_filename_prefix)
      setDefaultView(updated.default_torrent_view)
      setShowLatestAnnouncement(updated.show_latest_announcement)
      setCustomNavigationItems(
        updated.custom_navigation_items.map((item) => ({ ...item }))
      )
      setReason("")
      setPendingValues(undefined)
      setConfirmationOpen(false)
      setSuccessMessage(
        `站点与展示设置已直接生效，当前为第 ${updated.version} 版。`
      )
    } catch {
      setConfirmationOpen(false)
      // The typed API problem remains visible beside the form for correction.
    }
  }

  const disabled = !canUpdate || mutation.isPending

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {successMessage ? (
        <Alert>
          <CircleCheckIcon />
          <AlertTitle>设置已更新</AlertTitle>
          <AlertDescription>{successMessage}</AlertDescription>
        </Alert>
      ) : null}

      {mutation.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>{settingsMutationErrorTitle(mutation.error)}</AlertTitle>
          <AlertDescription>
            {settingsMutationErrorDescription(mutation.error)}
          </AlertDescription>
        </Alert>
      ) : null}

      {!canUpdate ? (
        <Alert>
          <Settings2Icon />
          <AlertTitle>当前权限仅可查看</AlertTitle>
          <AlertDescription>
            可以查看当前设置，但当前后台权限不能保存变更。
          </AlertDescription>
        </Alert>
      ) : null}

      {errors.form ? (
        <Alert>
          <CircleAlertIcon />
          <AlertTitle>没有可保存的变更</AlertTitle>
          <AlertDescription>{errors.form}</AlertDescription>
        </Alert>
      ) : null}

      <SiteSettingsCard
        saveAction={
          canUpdate ? (
            <Button
              type="submit"
              form="site-display-settings-form"
              className="w-28"
              disabled={!hasBusinessChanges || mutation.isPending}
            >
              {mutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <SaveIcon data-icon="inline-start" />
              )}
              {mutation.isPending ? "保存中…" : "保存修改"}
            </Button>
          ) : null
        }
      >
        <form
          id="site-display-settings-form"
          onSubmit={handleReview}
          noValidate
        >
          <div className="flex flex-col gap-6">
            <SettingsSection
              title="站点基本信息"
              icon={<Globe2Icon className="size-[18px] text-info" />}
            >
              <FieldGroup className="grid gap-4 md:grid-cols-2">
                <Field
                  data-invalid={Boolean(errors.name)}
                  data-disabled={disabled}
                >
                  <FieldLabel htmlFor="site-name">站点名称</FieldLabel>
                  <Input
                    id="site-name"
                    name="name"
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    maxLength={80}
                    disabled={disabled}
                    aria-invalid={Boolean(errors.name)}
                  />
                  <FieldDescription className="text-xs">
                    用于主站侧栏与页面标题；不会改变 Tracker 域名。
                  </FieldDescription>
                  <FieldError
                    errors={errors.name ? [{ message: errors.name }] : []}
                  />
                </Field>

                <Field
                  data-invalid={Boolean(errors.description)}
                  data-disabled={disabled}
                >
                  <FieldLabel htmlFor="site-description">站点说明</FieldLabel>
                  <Input
                    id="site-description"
                    name="description"
                    value={description}
                    onChange={(event) => setDescription(event.target.value)}
                    maxLength={500}
                    disabled={disabled}
                    aria-invalid={Boolean(errors.description)}
                    placeholder="简短说明站点定位…"
                  />
                  <FieldDescription className="text-xs">
                    显示在首页标题下方；允许留空，最多 500 个字符。
                  </FieldDescription>
                  <FieldError
                    errors={
                      errors.description
                        ? [{ message: errors.description }]
                        : []
                    }
                  />
                </Field>

                <Field
                  data-invalid={Boolean(errors.torrentFilenamePrefix)}
                  data-disabled={disabled}
                >
                  <FieldLabel htmlFor="torrent-filename-prefix">
                    种子文件名前缀
                  </FieldLabel>
                  <Input
                    id="torrent-filename-prefix"
                    name="torrentFilenamePrefix"
                    value={torrentFilenamePrefix}
                    onChange={(event) =>
                      setTorrentFilenamePrefix(event.target.value)
                    }
                    maxLength={40}
                    disabled={disabled}
                    aria-invalid={Boolean(errors.torrentFilenamePrefix)}
                    placeholder="[ROUSI]"
                  />
                  <FieldDescription className="text-xs">
                    下载文件将命名为“前缀.种子标题.torrent”；留空可关闭前缀，普通下载和
                    RSS 下载都会立即使用新值。
                  </FieldDescription>
                  <FieldError
                    errors={
                      errors.torrentFilenamePrefix
                        ? [{ message: errors.torrentFilenamePrefix }]
                        : []
                    }
                  />
                </Field>
              </FieldGroup>
            </SettingsSection>

            <SettingsSection
              title="首页展示"
              icon={<MonitorCogIcon className="size-[18px] text-info" />}
            >
              <FieldGroup className="grid gap-4 md:grid-cols-2">
                <Field data-disabled={disabled}>
                  <FieldLabel>默认种子视图</FieldLabel>
                  <ToggleGroup
                    value={[defaultView]}
                    onValueChange={(values) => {
                      const nextView = values[0]
                      if (nextView === "list" || nextView === "poster") {
                        setDefaultView(nextView)
                      }
                    }}
                    variant="outline"
                    spacing={0}
                    className="w-full"
                    disabled={disabled}
                    aria-label="首页默认种子视图"
                  >
                    <ToggleGroupItem value="list" className="h-10 flex-1">
                      <ListIcon data-icon="inline-start" />
                      列表
                    </ToggleGroupItem>
                    <ToggleGroupItem value="poster" className="h-10 flex-1">
                      <Grid2X2Icon data-icon="inline-start" />
                      海报
                    </ToggleGroupItem>
                  </ToggleGroup>
                  <FieldDescription className="text-xs">
                    只决定访客首次进入时的默认布局，访客仍可临时切换。
                  </FieldDescription>
                </Field>

                <Field
                  orientation="horizontal"
                  data-disabled={disabled}
                  className="min-h-10 rounded-md border bg-background px-3 py-2"
                >
                  <FieldContent>
                    <FieldLabel htmlFor="show-latest-announcement">
                      首页显示最新公告
                    </FieldLabel>
                    <FieldDescription className="text-xs">
                      关闭后公共首页和最新公告接口都不再公开当前公告。
                    </FieldDescription>
                  </FieldContent>
                  <Switch
                    id="show-latest-announcement"
                    checked={showLatestAnnouncement}
                    onCheckedChange={setShowLatestAnnouncement}
                    disabled={disabled}
                    aria-label="首页显示最新公告"
                  />
                </Field>
              </FieldGroup>
            </SettingsSection>

            <SettingsSection
              title="自定义左侧菜单"
              icon={<ExternalLinkIcon className="size-[18px] text-info" />}
            >
              <CustomNavigationItemsEditor
                items={customNavigationItems}
                disabled={disabled}
                error={errors.customNavigationItems}
                onChange={setCustomNavigationItems}
              />
            </SettingsSection>

            {canUpdate ? (
              <SettingsSection
                title="变更与审计"
                icon={
                  <ClipboardCheckIcon className="size-[18px] text-warning" />
                }
              >
                <FieldGroup>
                  <Field
                    data-invalid={Boolean(errors.reason)}
                    data-disabled={mutation.isPending}
                  >
                    <FieldLabel htmlFor="site-display-change-reason">
                      变更理由
                    </FieldLabel>
                    <Textarea
                      id="site-display-change-reason"
                      name="reason"
                      value={reason}
                      onChange={(event) => setReason(event.target.value)}
                      rows={3}
                      maxLength={500}
                      disabled={mutation.isPending}
                      aria-invalid={Boolean(errors.reason)}
                      placeholder="可留空；系统会自动记录变更理由"
                    />
                    <FieldDescription className="text-xs">
                      完整理由会安全保存，审计记录仅保留必要摘要。
                    </FieldDescription>
                    <FieldError
                      errors={errors.reason ? [{ message: errors.reason }] : []}
                    />
                  </Field>
                </FieldGroup>
              </SettingsSection>
            ) : null}

            <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
              <span>最近生效于 {formatDateTime(baseline.effective_at)}</span>
              <span>低风险设置保存后直接激活</span>
            </div>
          </div>
        </form>
      </SiteSettingsCard>

      <SettingsConfirmationDialog
        open={confirmationOpen}
        baseline={baseline}
        values={pendingValues}
        pending={mutation.isPending}
        onOpenChange={setConfirmationOpen}
        onConfirm={() => void handleConfirm()}
      />

      <AlertDialog open={blocker.state === "blocked"}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <TriangleAlertIcon />
            </AlertDialogMedia>
            <AlertDialogTitle>有未保存的站点设置</AlertDialogTitle>
            <AlertDialogDescription>
              离开后当前输入和变更理由会丢失，已经生效的设置不会改变。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => blocker.reset?.()}>
              继续编辑
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => blocker.proceed?.()}
            >
              放弃并离开
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function CustomNavigationItemsEditor({
  items,
  disabled,
  error,
  onChange,
}: {
  items: CustomNavigationItem[]
  disabled: boolean
  error?: string
  onChange: (items: CustomNavigationItem[]) => void
}) {
  function updateItem(index: number, patch: Partial<CustomNavigationItem>) {
    onChange(
      items.map((item, itemIndex) =>
        itemIndex === index ? { ...item, ...patch } : item
      )
    )
  }

  function moveItem(index: number, offset: -1 | 1) {
    const nextIndex = index + offset
    if (nextIndex < 0 || nextIndex >= items.length) return
    const nextItems = [...items]
    const current = nextItems[index]
    const target = nextItems[nextIndex]
    if (!current || !target) return
    nextItems[index] = target
    nextItems[nextIndex] = current
    onChange(nextItems)
  }

  return (
    <Field data-invalid={Boolean(error)} data-disabled={disabled}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <FieldContent>
          <FieldLabel>菜单链接</FieldLabel>
          <FieldDescription className="text-xs">
            最多 12 项；支持站内路径和 HTTPS 站外地址，不记录点击或访问历史。
          </FieldDescription>
        </FieldContent>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled || items.length >= 12}
          onClick={() =>
            onChange([
              ...items,
              {
                label: "",
                url: "",
                open_in_new_tab: true,
                enabled: true,
              },
            ])
          }
        >
          <PlusIcon data-icon="inline-start" />
          新增链接
        </Button>
      </div>

      {items.length === 0 ? (
        <div className="rounded-md border border-dashed bg-background px-4 py-6 text-center text-sm text-muted-foreground">
          暂无自定义菜单；可新增 Wiki、帮助中心或其他站点链接。
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {items.map((item, index) => (
            <div
              key={index}
              className="rounded-lg border bg-background p-4 shadow-xs"
            >
              <div className="mb-4 flex items-center justify-between gap-3">
                <span className="text-sm font-medium">菜单项 {index + 1}</span>
                <div className="flex items-center gap-1">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    disabled={disabled || index === 0}
                    aria-label={`上移菜单项 ${index + 1}`}
                    onClick={() => moveItem(index, -1)}
                  >
                    <ArrowUpIcon />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    disabled={disabled || index === items.length - 1}
                    aria-label={`下移菜单项 ${index + 1}`}
                    onClick={() => moveItem(index, 1)}
                  >
                    <ArrowDownIcon />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    disabled={disabled}
                    aria-label={`删除菜单项 ${index + 1}`}
                    onClick={() =>
                      onChange(
                        items.filter((_, itemIndex) => itemIndex !== index)
                      )
                    }
                  >
                    <Trash2Icon />
                  </Button>
                </div>
              </div>

              <FieldGroup className="grid gap-4 md:grid-cols-2">
                <Field data-disabled={disabled}>
                  <FieldLabel htmlFor={`custom-navigation-label-${index}`}>
                    菜单名称
                  </FieldLabel>
                  <Input
                    id={`custom-navigation-label-${index}`}
                    value={item.label}
                    maxLength={32}
                    disabled={disabled}
                    placeholder="Wiki"
                    onChange={(event) =>
                      updateItem(index, { label: event.target.value })
                    }
                  />
                </Field>
                <Field data-disabled={disabled}>
                  <FieldLabel htmlFor={`custom-navigation-url-${index}`}>
                    链接地址
                  </FieldLabel>
                  <Input
                    id={`custom-navigation-url-${index}`}
                    value={item.url}
                    maxLength={2048}
                    disabled={disabled}
                    inputMode="url"
                    placeholder="https://wiki.example.com"
                    onChange={(event) =>
                      updateItem(index, { url: event.target.value })
                    }
                  />
                </Field>
                <Field
                  orientation="horizontal"
                  data-disabled={disabled}
                  className="min-h-10 rounded-md border px-3 py-2"
                >
                  <FieldContent>
                    <FieldLabel htmlFor={`custom-navigation-enabled-${index}`}>
                      在侧栏显示
                    </FieldLabel>
                  </FieldContent>
                  <Switch
                    id={`custom-navigation-enabled-${index}`}
                    checked={item.enabled}
                    disabled={disabled}
                    onCheckedChange={(enabled) =>
                      updateItem(index, { enabled })
                    }
                  />
                </Field>
                <Field
                  orientation="horizontal"
                  data-disabled={disabled}
                  className="min-h-10 rounded-md border px-3 py-2"
                >
                  <FieldContent>
                    <FieldLabel htmlFor={`custom-navigation-new-tab-${index}`}>
                      新标签页打开
                    </FieldLabel>
                  </FieldContent>
                  <Switch
                    id={`custom-navigation-new-tab-${index}`}
                    checked={item.open_in_new_tab}
                    disabled={disabled}
                    onCheckedChange={(openInNewTab) =>
                      updateItem(index, { open_in_new_tab: openInNewTab })
                    }
                  />
                </Field>
              </FieldGroup>
            </div>
          ))}
        </div>
      )}

      <FieldError errors={error ? [{ message: error }] : []} />
    </Field>
  )
}

function SettingsSection({
  title,
  icon,
  children,
}: {
  title: string
  icon: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <Card className="bg-muted/30 shadow-none [--card-spacing:--spacing(4)]">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm leading-5">
          {icon}
          <h2>{title}</h2>
        </CardTitle>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  )
}

function SettingsConfirmationDialog({
  open,
  baseline,
  values,
  pending,
  onOpenChange,
  onConfirm,
}: {
  open: boolean
  baseline: SiteDisplaySettings
  values?: SiteDisplaySettingsFormValues
  pending: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}) {
  if (!values) {
    return null
  }
  const changes = siteDisplaySettingsDiff(baseline, values)
  return (
    <AlertDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && pending) {
          return
        }
        onOpenChange(nextOpen)
      }}
    >
      <AlertDialogContent className="sm:max-w-lg">
        <AlertDialogHeader>
          <AlertDialogMedia>
            <ClipboardCheckIcon />
          </AlertDialogMedia>
          <AlertDialogTitle>确认站点与展示变更</AlertDialogTitle>
          <AlertDialogDescription>
            将基于第 {baseline.version} 版创建第 {baseline.version + 1} 版
            ；成功后立即影响公共页面、左侧菜单与新生成的种子下载文件名。
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="flex max-h-72 flex-col gap-2 overflow-y-auto">
          {changes.map((change) => (
            <div
              key={change.field}
              className="grid gap-1 rounded-lg border bg-muted/30 p-3 text-sm sm:grid-cols-[8rem_1fr]"
            >
              <span className="font-medium">{change.field}</span>
              <div className="flex min-w-0 flex-col gap-1 text-muted-foreground">
                <span className="break-words line-through">
                  {change.before}
                </span>
                <span className="break-words text-foreground">
                  {change.after}
                </span>
              </div>
            </div>
          ))}
        </div>

        <Alert>
          <MonitorCogIcon />
          <AlertTitle>影响范围：公共展示、左侧菜单与下载文件名</AlertTitle>
          <AlertDescription>
            自定义菜单只保存有界链接配置；文件名前缀只改变浏览器保存的 .torrent
            名称。两者都不修改种子内容、Info Hash、Tracker
            连接策略、注册准入、身份资料或部署密钥。
          </AlertDescription>
        </Alert>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>返回修改</AlertDialogCancel>
          <AlertDialogAction disabled={pending} onClick={onConfirm}>
            {pending ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <SaveIcon data-icon="inline-start" />
            )}
            {pending ? "正在提交" : "确认并立即生效"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function SiteSettingsCard({
  saveAction,
  children,
}: {
  saveAction?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <Card
      data-site-settings-card
      className="gap-0 py-0 [--card-spacing:--spacing(6)]"
    >
      <CardHeader className="min-h-[88px] content-center items-center py-6">
        <CardTitle className="self-center text-xl leading-6 font-semibold">
          <h1 className="w-fit">站点设置</h1>
        </CardTitle>
        {saveAction ? <CardAction>{saveAction}</CardAction> : null}
      </CardHeader>
      <CardContent className="pb-6">{children}</CardContent>
    </Card>
  )
}

function DisabledSaveButton() {
  return (
    <Button className="w-28" disabled>
      <SaveIcon data-icon="inline-start" />
      保存修改
    </Button>
  )
}

function SettingsFrame({ children }: { children: React.ReactNode }) {
  return <StaffPageFrame>{children}</StaffPageFrame>
}

function SiteSettingsSkeleton() {
  return (
    <SettingsFrame>
      <SiteSettingsCard saveAction={<DisabledSaveButton />}>
        <div
          role="status"
          aria-label="正在读取站点设置"
          className="flex flex-col gap-4"
        >
          <Skeleton className="h-6 w-40" aria-hidden="true" />
          <Skeleton className="h-10 w-full" aria-hidden="true" />
          <Skeleton className="h-10 w-full" aria-hidden="true" />
          <Skeleton className="h-20 w-full" aria-hidden="true" />
        </div>
      </SiteSettingsCard>
    </SettingsFrame>
  )
}

function settingsMutationErrorTitle(error: Error) {
  if (
    error instanceof ApiProblemError &&
    error.code === "site_display_settings_version_conflict"
  ) {
    return "设置已被其他管理员更新"
  }
  if (error instanceof ApiProblemError && error.status === 403) {
    return "当前后台权限不能保存设置"
  }
  return "站点与展示设置保存失败"
}

function settingsMutationErrorDescription(error: Error) {
  if (
    error instanceof ApiProblemError &&
    error.code === "site_display_settings_version_conflict"
  ) {
    return "另一位管理员已经保存了设置。页面正在获取最新值，请重新核对后提交。"
  }
  if (error instanceof ApiProblemError && error.requestId) {
    return `服务器拒绝了本次变更。反馈时请附上请求编号：${error.requestId}`
  }
  return "当前有效设置没有改变，请核对后台登录状态后重试。"
}
