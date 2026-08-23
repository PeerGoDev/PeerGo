import * as React from "react"
import {
  CircleAlertIcon,
  ListTreeIcon,
  PencilIcon,
  PlusIcon,
  SaveIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
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
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "~/components/ui/sheet"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import {
  type ManagedCategory,
  type ManagedCategoryFacet,
  type ManagedCategoryFacetOption,
  useUpsertManagedCategoryFacetOption,
} from "~/features/staff/api/category-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"

type OptionEditor = {
  facet: ManagedCategoryFacet
  option?: ManagedCategoryFacetOption
}

export function CategoryFacetManagerSheet({
  category,
  csrfToken,
  canUpdate,
  onOpenChange,
  onSaved,
}: {
  category: ManagedCategory
  csrfToken: string
  canUpdate: boolean
  onOpenChange: (open: boolean) => void
  onSaved: (message: string) => void
}) {
  const [editor, setEditor] = React.useState<OptionEditor>()

  return (
    <Sheet open onOpenChange={onOpenChange}>
      <SheetContent className="w-full sm:max-w-3xl">
        <SheetHeader className="border-b pr-12">
          <SheetTitle>{category.name} · 类型与属性</SheetTitle>
          <SheetDescription>
            与发种页共用同一套分类属性。停用只影响新发种，历史种子引用仍会保留。
          </SheetDescription>
        </SheetHeader>

        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-6">
          {category.facets.length === 0 ? (
            <Empty className="min-h-64 border">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ListTreeIcon />
                </EmptyMedia>
                <EmptyTitle>这个分类还没有属性</EmptyTitle>
                <EmptyDescription>
                  当前版本先管理已有属性下的类型选项；属性定义仍由受控目录维护。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            category.facets.map((facet) => (
              <FacetCard
                key={facet.id}
                facet={facet}
                canUpdate={canUpdate}
                onAdd={() => setEditor({ facet })}
                onEdit={(option) => setEditor({ facet, option })}
              />
            ))
          )}
        </div>

        {editor ? (
          <CategoryFacetOptionDialog
            key={`${editor.facet.id}:${editor.option?.key ?? "new"}:${editor.option?.version ?? 0}`}
            category={category}
            facet={editor.facet}
            option={editor.option}
            csrfToken={csrfToken}
            onOpenChange={(open) => {
              if (!open) setEditor(undefined)
            }}
            onSaved={(message) => {
              setEditor(undefined)
              onSaved(message)
            }}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

function FacetCard({
  facet,
  canUpdate,
  onAdd,
  onEdit,
}: {
  facet: ManagedCategoryFacet
  canUpdate: boolean
  onAdd: () => void
  onEdit: (option: ManagedCategoryFacetOption) => void
}) {
  const enabledCount = facet.options.filter((option) => option.enabled).length
  return (
    <Card size="sm">
      <CardHeader className="border-b">
        <CardTitle className="flex flex-wrap items-center gap-2">
          <span>{facet.name}</span>
          <code className="text-xs font-normal text-muted-foreground">
            {facet.id}
          </code>
          {facet.required ? <Badge variant="secondary">必填</Badge> : null}
          {facet.requirement_group ? (
            <Badge variant="outline">
              至少一项 · {facet.requirement_group}
            </Badge>
          ) : null}
        </CardTitle>
        <p className="text-xs text-muted-foreground">
          {facet.selection_mode === "multi_option" ? "可多选" : "单选"} · 已启用{" "}
          {enabledCount}/{facet.options.length}
        </p>
        {canUpdate ? (
          <CardAction>
            <Button variant="outline" size="sm" onClick={onAdd}>
              <PlusIcon data-icon="inline-start" />
              添加选项
            </Button>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className="grid gap-1.5 pt-3">
        {facet.options.map((option) => (
          <div
            key={option.key}
            className="flex min-h-11 items-center gap-3 rounded-md border px-3 py-2"
          >
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="truncate font-medium">{option.label}</span>
                <code className="truncate text-xs text-muted-foreground">
                  {option.key}
                </code>
              </div>
              <p className="text-xs text-muted-foreground">
                顺序 {option.display_order.toLocaleString("zh-CN")} · 引用{" "}
                {option.torrent_count.toLocaleString("zh-CN")} 个种子
              </p>
            </div>
            <Badge variant={option.enabled ? "outline" : "destructive"}>
              {option.enabled ? "启用" : "停用"}
            </Badge>
            {canUpdate ? (
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => onEdit(option)}
                aria-label={`编辑类型选项 ${option.label}`}
              >
                <PencilIcon />
              </Button>
            ) : null}
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function CategoryFacetOptionDialog({
  category,
  facet,
  option,
  csrfToken,
  onOpenChange,
  onSaved,
}: {
  category: ManagedCategory
  facet: ManagedCategoryFacet
  option?: ManagedCategoryFacetOption
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onSaved: (message: string) => void
}) {
  const mutation = useUpsertManagedCategoryFacetOption()
  const [enabled, setEnabled] = React.useState(option?.enabled ?? true)
  const [error, setError] = React.useState("")

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const optionKey = option?.key ?? String(form.get("optionKey") ?? "").trim()
    const label = String(form.get("label") ?? "").trim()
    const displayOrder = Number(form.get("displayOrder"))
    const reason = String(form.get("reason") ?? "").trim()
    if (!optionKey || optionKey.length > 80 || /[/\?#]/u.test(optionKey)) {
      setError("稳定值需为 1–80 个字符，且不能包含 /、? 或 #。")
      return
    }
    if (!label || label.length > 80) {
      setError("显示名称需为 1–80 个字符。")
      return
    }
    if (
      !Number.isInteger(displayOrder) ||
      displayOrder < 0 ||
      displayOrder > 1_000_000
    ) {
      setError("排序权重必须是 0–1,000,000 之间的整数。")
      return
    }
    if ([...reason].length < 10 || [...reason].length > 500) {
      setError("变更理由需为 10–500 个字符。")
      return
    }
    setError("")
    try {
      await mutation.mutateAsync({
        csrfToken,
        categoryId: category.id,
        facetId: facet.id,
        optionKey,
        body: {
          label,
          display_order: displayOrder,
          enabled,
          expected_version: option?.version ?? 0,
          reason,
        },
      })
      onSaved(option ? `已更新“${label}”。` : `已添加“${label}”。`)
    } catch {
      // The typed API problem is rendered below.
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => !mutation.isPending && onOpenChange(open)}
    >
      <DialogContent className="sm:max-w-lg">
        <form
          onSubmit={handleSubmit}
          className="flex flex-col gap-5"
          noValidate
        >
          <DialogHeader>
            <DialogTitle>
              {option ? "编辑类型选项" : "添加类型选项"}
            </DialogTitle>
            <DialogDescription>
              {category.name} / {facet.name}。稳定值创建后不可修改。
            </DialogDescription>
          </DialogHeader>

          {mutation.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>类型选项未保存</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(mutation.error)}
              </AlertDescription>
            </Alert>
          ) : null}
          {error ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>请检查填写内容</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="category-option-key">稳定值</FieldLabel>
              <Input
                id="category-option-key"
                name="optionKey"
                defaultValue={option?.key}
                disabled={Boolean(option) || mutation.isPending}
                maxLength={80}
                placeholder="例如 action"
                autoFocus={!option}
              />
              <FieldDescription>
                用于筛选与存储，创建后不可修改。
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="category-option-label">显示名称</FieldLabel>
              <Input
                id="category-option-label"
                name="label"
                defaultValue={option?.label}
                disabled={mutation.isPending}
                maxLength={80}
                placeholder="例如 动作"
                autoFocus={Boolean(option)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="category-option-order">排序权重</FieldLabel>
              <Input
                id="category-option-order"
                name="displayOrder"
                type="number"
                min={0}
                max={1_000_000}
                step={1}
                defaultValue={
                  option?.display_order ?? (facet.options.length + 1) * 10
                }
                disabled={mutation.isPending}
              />
            </Field>
            <Field orientation="horizontal">
              <FieldLabel htmlFor="category-option-enabled">
                新发种可选
              </FieldLabel>
              <Switch
                id="category-option-enabled"
                checked={enabled}
                onCheckedChange={setEnabled}
                disabled={mutation.isPending}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="category-option-reason">变更理由</FieldLabel>
              <Textarea
                id="category-option-reason"
                name="reason"
                minLength={10}
                maxLength={500}
                disabled={mutation.isPending}
                placeholder="说明添加、改名、排序或停用的运营原因（至少 10 个字符）"
              />
              <FieldError errors={error ? [{ message: error }] : []} />
            </Field>
          </FieldGroup>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={mutation.isPending}
            >
              取消
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <SaveIcon data-icon="inline-start" />
              )}
              保存
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
