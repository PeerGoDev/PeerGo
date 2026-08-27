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
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import {
  type ManagedCategory,
  type ManagedCategoryFacet,
  type ManagedCategoryFacetOption,
  useUpsertManagedCategoryFacet,
  useUpsertManagedCategoryFacetOption,
} from "~/features/staff/api/category-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"

type OptionEditor = {
  facet: ManagedCategoryFacet
  option?: ManagedCategoryFacetOption
}

type FacetEditor = {
  facet?: ManagedCategoryFacet
}

export function CategoryFacetManager({
  category,
  csrfToken,
  canUpdate,
  onSaved,
}: {
  category: ManagedCategory
  csrfToken: string
  canUpdate: boolean
  onSaved: (message: string) => void
}) {
  const [facetEditor, setFacetEditor] = React.useState<FacetEditor>()
  const [optionEditor, setOptionEditor] = React.useState<OptionEditor>()

  return (
    <section
      className="flex flex-col gap-4"
      aria-labelledby={`category-facets-${category.id}`}
    >
      <header className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 flex-col gap-1">
          <h3
            id={`category-facets-${category.id}`}
            className="font-heading font-semibold"
          >
            {category.name} · 类型与属性
          </h3>
          <p className="text-sm text-muted-foreground">
            与发种页共用同一套分类属性。停用只影响新发种，历史种子引用仍会保留。
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-3">
          <span className="text-xs text-muted-foreground">
            已配置 {category.facets.length}/20
          </span>
          {canUpdate ? (
            <Button
              size="sm"
              onClick={() => setFacetEditor({})}
              disabled={category.facets.length >= 20}
            >
              <PlusIcon data-icon="inline-start" />
              添加属性
            </Button>
          ) : null}
        </div>
      </header>

      {category.facets.length === 0 ? (
        <Empty className="min-h-48 border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ListTreeIcon />
            </EmptyMedia>
            <EmptyTitle>这个分类还没有属性</EmptyTitle>
            <EmptyDescription>
              还没有发种属性。可新增分辨率、来源、类型等单选或多选参数。
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="flex flex-col gap-3">
          {category.facets.map((facet) => (
            <FacetCard
              key={facet.id}
              facet={facet}
              canUpdate={canUpdate}
              onEditFacet={() => setFacetEditor({ facet })}
              onAdd={() => setOptionEditor({ facet })}
              onEdit={(option) => setOptionEditor({ facet, option })}
            />
          ))}
        </div>
      )}

      {facetEditor ? (
        <CategoryFacetDialog
          key={`${facetEditor.facet?.id ?? "new"}:${facetEditor.facet?.version ?? 0}`}
          category={category}
          facet={facetEditor.facet}
          csrfToken={csrfToken}
          onOpenChange={(open) => {
            if (!open) setFacetEditor(undefined)
          }}
          onSaved={(message) => {
            setFacetEditor(undefined)
            onSaved(message)
          }}
        />
      ) : null}

      {optionEditor ? (
        <CategoryFacetOptionDialog
          key={`${optionEditor.facet.id}:${optionEditor.option?.key ?? "new"}:${optionEditor.option?.version ?? 0}`}
          category={category}
          facet={optionEditor.facet}
          option={optionEditor.option}
          csrfToken={csrfToken}
          onOpenChange={(open) => {
            if (!open) setOptionEditor(undefined)
          }}
          onSaved={(message) => {
            setOptionEditor(undefined)
            onSaved(message)
          }}
        />
      ) : null}
    </section>
  )
}

function FacetCard({
  facet,
  canUpdate,
  onEditFacet,
  onAdd,
  onEdit,
}: {
  facet: ManagedCategoryFacet
  canUpdate: boolean
  onEditFacet: () => void
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
          <Badge variant={facet.enabled ? "outline" : "destructive"}>
            {facet.enabled ? "启用" : "停用"}
          </Badge>
          {facet.requirement_group ? (
            <Badge variant="outline">
              至少一项 · {facet.requirement_group}
            </Badge>
          ) : null}
        </CardTitle>
        <CardDescription className="text-xs">
          {facet.selection_mode === "multi_option" ? "可多选" : "单选"} · 已启用{" "}
          {enabledCount}/{facet.options.length} · 上限 200
        </CardDescription>
        {canUpdate ? (
          <CardAction className="col-start-1 row-start-3 mt-2 justify-self-stretch sm:col-start-2 sm:row-span-2 sm:row-start-1 sm:mt-0 sm:justify-self-end">
            <div className="flex flex-wrap items-center gap-2 sm:justify-end">
              <Button variant="ghost" size="sm" onClick={onEditFacet}>
                <PencilIcon data-icon="inline-start" />
                编辑属性
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={onAdd}
                disabled={facet.options.length >= 200}
              >
                <PlusIcon data-icon="inline-start" />
                添加选项
              </Button>
            </div>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className="flex flex-wrap gap-2 pt-3">
        {facet.options.length === 0 ? (
          <p className="w-full rounded-md border border-dashed px-3 py-4 text-center text-sm text-muted-foreground">
            尚无选项；添加后发种页会自动显示这个属性。
          </p>
        ) : null}
        {facet.options.map((option) => (
          <FacetOptionChip
            key={option.key}
            option={option}
            canUpdate={canUpdate}
            onEdit={() => onEdit(option)}
          />
        ))}
      </CardContent>
    </Card>
  )
}

function FacetOptionChip({
  option,
  canUpdate,
  onEdit,
}: {
  option: ManagedCategoryFacetOption
  canUpdate: boolean
  onEdit: () => void
}) {
  const details = `稳定值 ${option.key} · 顺序 ${option.display_order.toLocaleString("zh-CN")} · 引用 ${option.torrent_count.toLocaleString("zh-CN")} 个种子`
  const content = (
    <>
      <span className="max-w-48 truncate">{option.label}</span>
      <code className="max-w-36 truncate font-normal text-muted-foreground">
        {option.key}
      </code>
      {!option.enabled && canUpdate ? (
        <Badge variant="destructive">停用</Badge>
      ) : null}
    </>
  )

  return canUpdate ? (
    <Button
      variant="outline"
      size="xs"
      className="h-auto max-w-full py-1"
      onClick={onEdit}
      aria-label={`编辑类型选项 ${option.label}`}
      title={details}
    >
      {content}
      <PencilIcon data-icon="inline-end" />
    </Button>
  ) : (
    <Badge variant={option.enabled ? "outline" : "destructive"} title={details}>
      {content}
    </Badge>
  )
}

function CategoryFacetDialog({
  category,
  facet,
  csrfToken,
  onOpenChange,
  onSaved,
}: {
  category: ManagedCategory
  facet?: ManagedCategoryFacet
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onSaved: (message: string) => void
}) {
  const mutation = useUpsertManagedCategoryFacet()
  const [selectionMode, setSelectionMode] = React.useState<
    "single_option" | "multi_option"
  >(facet?.selection_mode ?? "single_option")
  const [required, setRequired] = React.useState(facet?.required ?? false)
  const [enabled, setEnabled] = React.useState(facet?.enabled ?? true)
  const [error, setError] = React.useState("")

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const facetId = facet?.id ?? String(form.get("facetId") ?? "").trim()
    const name = String(form.get("name") ?? "").trim()
    const requirementGroup = String(form.get("requirementGroup") ?? "").trim()
    const displayOrder = Number(form.get("displayOrder"))
    const reason = String(form.get("reason") ?? "").trim()
    if (!/^[a-z0-9][a-z0-9-]{0,63}$/u.test(facetId)) {
      setError("属性稳定标识只能使用小写字母、数字与连字符，最长 64 位。")
      return
    }
    if (!name || [...name].length > 40) {
      setError("属性名称需为 1–40 个字符。")
      return
    }
    if (
      requirementGroup &&
      !/^[a-z0-9][a-z0-9-]{0,63}$/u.test(requirementGroup)
    ) {
      setError("条件必填组只能使用小写字母、数字与连字符。")
      return
    }
    if (required && requirementGroup) {
      setError("直接必填与条件必填组不能同时设置。")
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
    if ([...reason].length > 500) {
      setError("变更理由不能超过 500 个字符。")
      return
    }
    setError("")
    try {
      await mutation.mutateAsync({
        csrfToken,
        categoryId: category.id,
        facetId,
        body: {
          name,
          selection_mode: selectionMode,
          required,
          ...(requirementGroup ? { requirement_group: requirementGroup } : {}),
          display_order: displayOrder,
          enabled,
          expected_version: facet?.version ?? 0,
          reason,
        },
      })
      onSaved(facet ? `已更新属性“${name}”。` : `已添加属性“${name}”。`)
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
            <DialogTitle>{facet ? "编辑分类属性" : "添加分类属性"}</DialogTitle>
            <DialogDescription>
              {category.name}。稳定标识与单选/多选模式创建后不可修改。
            </DialogDescription>
          </DialogHeader>

          {mutation.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>分类属性未保存</AlertTitle>
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
              <FieldLabel htmlFor="category-facet-id">稳定标识</FieldLabel>
              <Input
                id="category-facet-id"
                name="facetId"
                defaultValue={facet?.id}
                disabled={Boolean(facet) || mutation.isPending}
                maxLength={64}
                placeholder="例如 resolution"
                autoFocus={!facet}
              />
              <FieldDescription>
                用于 API、筛选与种子属性存储，创建后不可修改。
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="category-facet-name">显示名称</FieldLabel>
              <Input
                id="category-facet-name"
                name="name"
                defaultValue={facet?.name}
                disabled={mutation.isPending}
                maxLength={40}
                placeholder="例如 分辨率"
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="category-facet-mode">选择方式</FieldLabel>
              <Select
                value={selectionMode}
                onValueChange={(value) =>
                  setSelectionMode(value as "single_option" | "multi_option")
                }
                disabled={Boolean(facet) || mutation.isPending}
              >
                <SelectTrigger id="category-facet-mode" className="w-full">
                  <SelectValue>
                    {selectionMode === "multi_option" ? "多选" : "单选"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectItem value="single_option">单选</SelectItem>
                  <SelectItem value="multi_option">多选</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field orientation="horizontal">
              <FieldLabel htmlFor="category-facet-required">
                每次发种必填
              </FieldLabel>
              <Switch
                id="category-facet-required"
                checked={required}
                onCheckedChange={(value) => {
                  setRequired(value)
                }}
                disabled={mutation.isPending}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="category-facet-group">
                条件必填组（可选）
              </FieldLabel>
              <Input
                id="category-facet-group"
                name="requirementGroup"
                defaultValue={facet?.requirement_group}
                disabled={required || mutation.isPending}
                maxLength={64}
                placeholder="例如 source"
              />
              <FieldDescription>
                同组属性至少填写一项；直接必填时无需设置。
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="category-facet-order">排序权重</FieldLabel>
              <Input
                id="category-facet-order"
                name="displayOrder"
                type="number"
                min={0}
                max={1_000_000}
                step={1}
                defaultValue={
                  facet?.display_order ?? (category.facets.length + 1) * 10
                }
                disabled={mutation.isPending}
              />
            </Field>
            <Field orientation="horizontal">
              <FieldLabel htmlFor="category-facet-enabled">
                新发种可见
              </FieldLabel>
              <Switch
                id="category-facet-enabled"
                checked={enabled}
                onCheckedChange={setEnabled}
                disabled={mutation.isPending}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="category-facet-reason">变更理由</FieldLabel>
              <Textarea
                id="category-facet-reason"
                name="reason"
                maxLength={500}
                disabled={mutation.isPending}
                placeholder="可留空；系统会自动记录本次变更理由"
              />
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
    if ([...reason].length > 500) {
      setError("变更理由不能超过 500 个字符。")
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
                maxLength={500}
                disabled={mutation.isPending}
                placeholder="可留空；系统会自动记录本次变更理由"
              />
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
