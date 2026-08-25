import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CheckIcon,
  CircleAlertIcon,
  ClipboardIcon,
  KeyRoundIcon,
  LogInIcon,
  PencilIcon,
  PlusIcon,
  RefreshCwIcon,
  RssIcon,
  ShieldXIcon,
  Trash2Icon,
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
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
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
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
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
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type IssuedRSSSubscription,
  type RSSSubscription,
  type RSSSubscriptionInput,
  rssSubscriptionsQueryOptions,
  useCreateRSSSubscription,
  useDeleteRSSSubscription,
  useRotateRSSSubscriptionToken,
  useUpdateRSSSubscription,
} from "~/features/rss/api/rss.queries"
import { categoryListQueryOptions } from "~/features/torrent/api/torrent.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatDateTime } from "~/shared/formatters/date-time"

const promotionOptions: Array<{
  value: RSSSubscriptionInput["promotion_filters"][number]
  label: string
}> = [
  { value: "free", label: "免费" },
  { value: "double_upload", label: "2× 上传" },
  { value: "double_upload_free", label: "2× 上传 / 免费" },
  { value: "half_download", label: "50% 下载" },
  { value: "double_upload_half_download", label: "2× 上传 / 50% 下载" },
  { value: "thirty_percent_download", label: "30% 下载" },
]

const priceOptions = [
  { value: "all", label: "全部价格" },
  { value: "free", label: "仅免费种子" },
  { value: "paid", label: "仅付费种子" },
] satisfies Array<{
  value: RSSSubscriptionInput["price_filter"]
  label: string
}>

const defaultInput: RSSSubscriptionInput = {
  name: "最新种子",
  enabled: true,
  category_ids: [],
  promotion_filters: [],
  price_filter: "all",
  bookmarked_only: false,
  item_limit: 50,
  include_category: true,
  include_subtitle: true,
  include_size: true,
  include_promotion: true,
}

export function RSSSubscriptionsPage() {
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const canRead = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "rss.subscription.read.self"
    )
  )
  const canManage = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "rss.subscription.manage.self"
    )
  )
  const subscriptions = useQuery({
    ...rssSubscriptionsQueryOptions(session.data?.user.id),
    enabled: Boolean(session.data && capabilities.data && canRead),
  })
  const categories = useQuery({
    ...categoryListQueryOptions,
    enabled: Boolean(session.data && canManage),
  })
  const create = useCreateRSSSubscription()
  const update = useUpdateRSSSubscription()
  const rotate = useRotateRSSSubscriptionToken()
  const remove = useDeleteRSSSubscription()
  const [editing, setEditing] = React.useState<RSSSubscription | "new">()
  const [issued, setIssued] = React.useState<IssuedRSSSubscription>()
  const [rotateTarget, setRotateTarget] = React.useState<RSSSubscription>()
  const [deleteTarget, setDeleteTarget] = React.useState<RSSSubscription>()
  const [copied, setCopied] = React.useState(false)

  const mutationError =
    (create.isError ? create.error : undefined) ??
    (update.isError ? update.error : undefined) ??
    (rotate.isError ? rotate.error : undefined) ??
    (remove.isError ? remove.error : undefined)

  async function save(input: RSSSubscriptionInput) {
    if (!session.data || !editing) return
    if (editing === "new") {
      const result = await create.mutateAsync({
        csrfToken: session.data.csrf_token,
        body: input,
      })
      setIssued(result)
    } else {
      await update.mutateAsync({
        csrfToken: session.data.csrf_token,
        subscription: editing,
        body: input,
      })
    }
    setEditing(undefined)
  }

  async function confirmRotate() {
    if (!session.data || !rotateTarget) return
    const result = await rotate.mutateAsync({
      csrfToken: session.data.csrf_token,
      subscription: rotateTarget,
    })
    setRotateTarget(undefined)
    setCopied(false)
    setIssued(result)
  }

  async function confirmDelete() {
    if (!session.data || !deleteTarget) return
    await remove.mutateAsync({
      csrfToken: session.data.csrf_token,
      subscription: deleteTarget,
    })
    setDeleteTarget(undefined)
  }

  async function copyURL() {
    if (!issued) return
    await navigator.clipboard.writeText(issued.feed_url)
    setCopied(true)
  }

  return (
    <PageLayout className="gap-4">
      <PageHeader
        title="RSS 订阅"
        description="为下载器创建固定筛选条件的私密订阅；优惠状态按 RSS 生成时刻计算。"
      />

      <Alert>
        <KeyRoundIcon />
        <AlertTitle>RSS 地址也是登录凭据</AlertTitle>
        <AlertDescription>
          创建和轮换后只显示一次。请勿公开、截图或填入第三方分析服务；泄露后立即轮换。
        </AlertDescription>
      </Alert>

      {session.isPending || (session.data && capabilities.isPending) ? (
        <RSSSkeleton />
      ) : null}

      {session.isError || capabilities.isError ? (
        <RSSFailure
          description={requestErrorDescription(
            session.error ?? capabilities.error,
            "暂时无法确认当前账户权限。"
          )}
          retry={() => {
            void session.refetch()
            void capabilities.refetch()
          }}
        />
      ) : null}

      {!session.isPending && !session.isError && !session.data ? (
        <Empty className="border bg-card">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <LogInIcon />
            </EmptyMedia>
            <EmptyTitle>登录后管理 RSS</EmptyTitle>
            <EmptyDescription>每个 RSS 地址都绑定到当前账户。</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Link to="/login" className={buttonVariants()}>
              前往登录
            </Link>
          </EmptyContent>
        </Empty>
      ) : null}

      {session.data && capabilities.data && !canRead ? (
        <Empty className="border bg-card">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ShieldXIcon />
            </EmptyMedia>
            <EmptyTitle>当前账户不能使用 RSS</EmptyTitle>
            <EmptyDescription>
              请联系站点管理人员确认账户状态。
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : null}

      {mutationError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>RSS 设置未能保存</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(mutationError)}
          </AlertDescription>
        </Alert>
      ) : null}

      {session.data && canRead && subscriptions.isError ? (
        <RSSFailure
          description={requestErrorDescription(subscriptions.error)}
          retry={() => void subscriptions.refetch()}
        />
      ) : null}

      {session.data && canRead && subscriptions.data ? (
        <section className="flex flex-col gap-3" aria-label="RSS 订阅列表">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-sm text-muted-foreground">
              共 {subscriptions.data.length} 个订阅
            </p>
            {canManage ? (
              <Button onClick={() => setEditing("new")}>
                <PlusIcon data-icon="inline-start" />
                新建订阅
              </Button>
            ) : null}
          </div>
          {subscriptions.data.length === 0 ? (
            <Empty className="border bg-card">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <RssIcon />
                </EmptyMedia>
                <EmptyTitle>还没有 RSS 订阅</EmptyTitle>
                <EmptyDescription>
                  创建后，将私密地址添加到支持 RSS 的下载器。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="grid gap-3 lg:grid-cols-2">
              {subscriptions.data.map((subscription) => (
                <SubscriptionCard
                  key={subscription.id}
                  subscription={subscription}
                  categoryNames={
                    new Map(
                      categories.data?.map((category) => [
                        category.id,
                        category.name,
                      ]) ?? []
                    )
                  }
                  canManage={canManage}
                  onEdit={() => setEditing(subscription)}
                  onRotate={() => setRotateTarget(subscription)}
                  onDelete={() => setDeleteTarget(subscription)}
                />
              ))}
            </div>
          )}
        </section>
      ) : null}

      {editing ? (
        <SubscriptionDialog
          key={editing === "new" ? "new" : `${editing.id}:${editing.version}`}
          subscription={editing === "new" ? undefined : editing}
          categories={categories.data ?? []}
          pending={create.isPending || update.isPending}
          onOpenChange={(open) => !open && setEditing(undefined)}
          onSave={save}
        />
      ) : null}

      <IssuedDialog
        issued={issued}
        copied={copied}
        onCopy={() => void copyURL()}
        onOpenChange={(open) => {
          if (!open) {
            setIssued(undefined)
            setCopied(false)
          }
        }}
      />

      <AlertDialog
        open={Boolean(rotateTarget)}
        onOpenChange={(open) => !open && setRotateTarget(undefined)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <KeyRoundIcon />
            </AlertDialogMedia>
            <AlertDialogTitle>轮换 RSS 地址？</AlertDialogTitle>
            <AlertDialogDescription>
              旧地址会立即失效，需要在下载器中替换为新地址。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => void confirmRotate()}
              disabled={rotate.isPending}
            >
              {rotate.isPending ? "轮换中…" : "确认轮换"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => !open && setDeleteTarget(undefined)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <Trash2Icon />
            </AlertDialogMedia>
            <AlertDialogTitle>删除 RSS 订阅？</AlertDialogTitle>
            <AlertDialogDescription>
              此操作会永久撤销对应地址，无法恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => void confirmDelete()}
              disabled={remove.isPending}
            >
              {remove.isPending ? "删除中…" : "确认删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </PageLayout>
  )
}

function SubscriptionCard({
  subscription,
  categoryNames,
  canManage,
  onEdit,
  onRotate,
  onDelete,
}: {
  subscription: RSSSubscription
  categoryNames: Map<string, string>
  canManage: boolean
  onEdit: () => void
  onRotate: () => void
  onDelete: () => void
}) {
  const categoryLabel = subscription.category_ids.length
    ? subscription.category_ids
        .map((id) => categoryNames.get(id) ?? id)
        .join("、")
    : "全部分类"
  const promotionLabel = subscription.promotion_filters.length
    ? subscription.promotion_filters
        .map(
          (value) =>
            promotionOptions.find((item) => item.value === value)?.label ??
            value
        )
        .join("、")
    : "全部优惠"

  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-4 pb-3">
        <CardTitle className="flex items-center gap-2">
          <RssIcon className="size-4" />
          {subscription.name}
          <Badge variant={subscription.enabled ? "secondary" : "outline"}>
            {subscription.enabled ? "已启用" : "已停用"}
          </Badge>
        </CardTitle>
        <CardDescription>
          {categoryLabel} · {promotionLabel} · 最多 {subscription.item_limit} 条
        </CardDescription>
        {canManage ? (
          <CardAction className="flex gap-1">
            <Button
              size="icon-sm"
              variant="ghost"
              onClick={onEdit}
              aria-label={`编辑 ${subscription.name}`}
            >
              <PencilIcon />
            </Button>
            <Button
              size="icon-sm"
              variant="ghost"
              onClick={onRotate}
              aria-label={`轮换 ${subscription.name} 地址`}
            >
              <KeyRoundIcon />
            </Button>
            <Button
              size="icon-sm"
              variant="ghost"
              onClick={onDelete}
              aria-label={`删除 ${subscription.name}`}
            >
              <Trash2Icon />
            </Button>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className="border-t px-4 py-3 text-xs text-muted-foreground">
        地址第 {subscription.token_version} 版 · 设置第 {subscription.version}{" "}
        版 · {formatDateTime(subscription.updated_at)}
      </CardContent>
    </Card>
  )
}

function SubscriptionDialog({
  subscription,
  categories,
  pending,
  onOpenChange,
  onSave,
}: {
  subscription?: RSSSubscription
  categories: Array<{ id: string; name: string }>
  pending: boolean
  onOpenChange: (open: boolean) => void
  onSave: (input: RSSSubscriptionInput) => Promise<void>
}) {
  const [draft, setDraft] = React.useState<RSSSubscriptionInput>(
    subscription ? subscriptionInput(subscription) : defaultInput
  )

  function toggleArray<T extends string>(
    values: T[],
    value: T,
    checked: boolean
  ) {
    return checked
      ? [...values, value]
      : values.filter((item) => item !== value)
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {subscription ? "编辑 RSS 订阅" : "新建 RSS 订阅"}
          </DialogTitle>
          <DialogDescription>
            空分类或空优惠表示不过滤。设置只影响以后生成的条目。
          </DialogDescription>
        </DialogHeader>
        <form
          id="rss-subscription-form"
          onSubmit={(event) => {
            event.preventDefault()
            if (draft.name.trim())
              void onSave({ ...draft, name: draft.name.trim() })
          }}
        >
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="rss-name">名称</FieldLabel>
              <Input
                id="rss-name"
                maxLength={80}
                value={draft.name}
                onChange={(event) =>
                  setDraft({ ...draft, name: event.target.value })
                }
                required
              />
            </Field>

            <FieldSet>
              <FieldLegend>分类</FieldLegend>
              <FieldDescription>
                不选择时包含全部启用分类，最多选择 20 个。
              </FieldDescription>
              <div className="grid gap-2 sm:grid-cols-2">
                {categories.map((category) => {
                  const id = `rss-category-${category.id}`
                  return (
                    <Field key={category.id} orientation="horizontal">
                      <Checkbox
                        id={id}
                        checked={draft.category_ids.includes(category.id)}
                        onCheckedChange={(checked) =>
                          setDraft({
                            ...draft,
                            category_ids: toggleArray(
                              draft.category_ids,
                              category.id,
                              checked
                            ).slice(0, 20),
                          })
                        }
                      />
                      <FieldLabel htmlFor={id} className="font-normal">
                        {category.name}
                      </FieldLabel>
                    </Field>
                  )
                })}
              </div>
            </FieldSet>

            <FieldSet>
              <FieldLegend>优惠</FieldLegend>
              <FieldDescription>
                不选择时同时包含无优惠和有优惠种子。
              </FieldDescription>
              <div className="grid gap-2 sm:grid-cols-2">
                {promotionOptions.map((option) => {
                  const id = `rss-promotion-${option.value}`
                  return (
                    <Field key={option.value} orientation="horizontal">
                      <Checkbox
                        id={id}
                        checked={draft.promotion_filters.includes(option.value)}
                        onCheckedChange={(checked) =>
                          setDraft({
                            ...draft,
                            promotion_filters: toggleArray(
                              draft.promotion_filters,
                              option.value,
                              checked
                            ),
                          })
                        }
                      />
                      <FieldLabel htmlFor={id} className="font-normal">
                        {option.label}
                      </FieldLabel>
                    </Field>
                  )
                })}
              </div>
            </FieldSet>

            <div className="grid gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="rss-price">付费筛选</FieldLabel>
                <Select
                  items={priceOptions}
                  value={draft.price_filter}
                  onValueChange={(value) =>
                    value && setDraft({ ...draft, price_filter: value })
                  }
                >
                  <SelectTrigger id="rss-price" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectLabel>付费筛选</SelectLabel>
                      {priceOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor="rss-limit">条目数</FieldLabel>
                <Input
                  id="rss-limit"
                  type="number"
                  min={1}
                  max={50}
                  value={draft.item_limit}
                  onChange={(event) =>
                    setDraft({
                      ...draft,
                      item_limit: Math.max(
                        1,
                        Math.min(50, Number(event.target.value) || 1)
                      ),
                    })
                  }
                />
              </Field>
            </div>

            <div className="grid gap-2 sm:grid-cols-2">
              <ToggleField
                label="启用订阅"
                checked={draft.enabled}
                onChange={(enabled) => setDraft({ ...draft, enabled })}
              />
              <ToggleField
                label="仅我的收藏"
                checked={draft.bookmarked_only}
                onChange={(bookmarked_only) =>
                  setDraft({ ...draft, bookmarked_only })
                }
              />
              <ToggleField
                label="显示分类"
                checked={draft.include_category}
                onChange={(include_category) =>
                  setDraft({ ...draft, include_category })
                }
              />
              <ToggleField
                label="显示副标题"
                checked={draft.include_subtitle}
                onChange={(include_subtitle) =>
                  setDraft({ ...draft, include_subtitle })
                }
              />
              <ToggleField
                label="显示文件大小"
                checked={draft.include_size}
                onChange={(include_size) =>
                  setDraft({ ...draft, include_size })
                }
              />
              <ToggleField
                label="显示优惠"
                checked={draft.include_promotion}
                onChange={(include_promotion) =>
                  setDraft({ ...draft, include_promotion })
                }
              />
            </div>
          </FieldGroup>
        </form>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            取消
          </Button>
          <Button
            type="submit"
            form="rss-subscription-form"
            disabled={pending || !draft.name.trim()}
          >
            {pending ? "保存中…" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ToggleField({
  label,
  checked,
  onChange,
}: {
  label: string
  checked: boolean
  onChange: (checked: boolean) => void
}) {
  return (
    <Field orientation="horizontal" className="rounded-lg border p-3">
      <FieldLabel className="font-normal">{label}</FieldLabel>
      <Switch checked={checked} onCheckedChange={onChange} />
    </Field>
  )
}

function IssuedDialog({
  issued,
  copied,
  onCopy,
  onOpenChange,
}: {
  issued?: IssuedRSSSubscription
  copied: boolean
  onCopy: () => void
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={Boolean(issued)} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>保存私密 RSS 地址</DialogTitle>
          <DialogDescription>
            关闭后 PeerGo 不会再次显示这个地址；丢失时请轮换。
          </DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="issued-rss-url">RSS 地址</FieldLabel>
          <div className="flex gap-2">
            <Input
              id="issued-rss-url"
              readOnly
              value={issued?.feed_url ?? ""}
              className="font-mono text-xs"
            />
            <Button type="button" variant="outline" onClick={onCopy}>
              {copied ? <CheckIcon /> : <ClipboardIcon />}
              <span className="sr-only">复制地址</span>
            </Button>
          </div>
        </Field>
        <DialogFooter showCloseButton />
      </DialogContent>
    </Dialog>
  )
}

function RSSFailure({
  description,
  retry,
}: {
  description: string
  retry: () => void
}) {
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>RSS 暂时无法读取</AlertTitle>
      <AlertDescription>{description}</AlertDescription>
      <AlertAction>
        <Button size="sm" variant="outline" onClick={retry}>
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </AlertAction>
    </Alert>
  )
}

function RSSSkeleton() {
  return (
    <div className="grid gap-3 lg:grid-cols-2" aria-busy="true">
      <Skeleton className="h-32" />
      <Skeleton className="h-32" />
    </div>
  )
}

function subscriptionInput(
  subscription: RSSSubscription
): RSSSubscriptionInput {
  return {
    name: subscription.name,
    enabled: subscription.enabled,
    category_ids: subscription.category_ids,
    promotion_filters: subscription.promotion_filters,
    price_filter: subscription.price_filter,
    bookmarked_only: subscription.bookmarked_only,
    item_limit: subscription.item_limit,
    include_category: subscription.include_category,
    include_subtitle: subscription.include_subtitle,
    include_size: subscription.include_size,
    include_promotion: subscription.include_promotion,
  }
}
