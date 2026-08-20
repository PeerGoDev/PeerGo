import * as React from "react"
import {
  CircleAlertIcon,
  ClipboardCheckIcon,
  PlusIcon,
  PowerIcon,
  PowerOffIcon,
  SaveIcon,
  TriangleAlertIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
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
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type ManagedCategory,
  useCreateManagedCategory,
  useUpdateManagedCategory,
} from "~/features/staff/api/category-administration.queries"
import {
  type CategoryFormField,
  type CategoryFormValues,
  categoryFormSchema,
  hasCategoryBusinessChanges,
} from "~/features/staff/model/category-form"
import { ApiProblemError } from "~/shared/api/problem"

type FormErrors = Partial<Record<CategoryFormField | "form", string>>

export function CategoryEditorSheet({
  category,
  csrfToken,
  onOpenChange,
  onSaved,
}: {
  category?: ManagedCategory
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onSaved: (category: ManagedCategory, mode: "created" | "updated") => void
}) {
  const createMutation = useCreateManagedCategory()
  const updateMutation = useUpdateManagedCategory()
  const [enabled, setEnabled] = React.useState(category?.enabled ?? true)
  const [errors, setErrors] = React.useState<FormErrors>({})
  const [confirmationOpen, setConfirmationOpen] = React.useState(false)
  const [pendingValues, setPendingValues] = React.useState<CategoryFormValues>()
  const mutation = category ? updateMutation : createMutation
  const isPending = mutation.isPending
  const disablingReferencedCategory = Boolean(
    category?.enabled && !enabled && category.torrent_count > 0
  )

  function handleSheetOpenChange(nextOpen: boolean) {
    if (!nextOpen && isPending) {
      return
    }
    onOpenChange(nextOpen)
  }

  function handleReview(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const formElement = event.currentTarget
    mutation.reset()
    const form = new FormData(formElement)
    const result = categoryFormSchema.safeParse({
      id: category?.id ?? String(form.get("id") ?? ""),
      name: String(form.get("name") ?? ""),
      displayOrder: String(form.get("displayOrder") ?? ""),
      enabled,
      reason: String(form.get("reason") ?? ""),
    })
    if (!result.success) {
      const nextErrors: FormErrors = {}
      for (const issue of result.error.issues) {
        const field = issue.path[0]
        if (
          typeof field === "string" &&
          !nextErrors[field as CategoryFormField]
        ) {
          nextErrors[field as CategoryFormField] = issue.message
        }
      }
      setErrors(nextErrors)
      requestAnimationFrame(() => {
        formElement.querySelector<HTMLElement>("[aria-invalid='true']")?.focus()
      })
      return
    }
    if (category && !hasCategoryBusinessChanges(category, result.data)) {
      setErrors({ form: "名称、排序权重和状态均未变化，无需创建空变更。" })
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
      if (category) {
        const updated = await updateMutation.mutateAsync({
          csrfToken,
          categoryId: category.id,
          body: {
            name: pendingValues.name,
            display_order: pendingValues.displayOrder,
            enabled: pendingValues.enabled,
            expected_version: category.version,
            reason: pendingValues.reason,
          },
        })
        setConfirmationOpen(false)
        onSaved(updated, "updated")
      } else {
        const created = await createMutation.mutateAsync({
          csrfToken,
          body: {
            id: pendingValues.id,
            name: pendingValues.name,
            display_order: pendingValues.displayOrder,
            enabled: pendingValues.enabled,
            reason: pendingValues.reason,
          },
        })
        setConfirmationOpen(false)
        onSaved(created, "created")
      }
    } catch {
      setConfirmationOpen(false)
      // The reviewed API problem remains visible in the sheet for correction.
    }
  }

  return (
    <Sheet open onOpenChange={handleSheetOpenChange}>
      <SheetContent className="w-full sm:max-w-lg">
        <SheetHeader className="border-b pr-12">
          <SheetTitle>{category ? "编辑分类" : "创建分类"}</SheetTitle>
          <SheetDescription>
            {category
              ? "分类标识不可修改；保存前会重新确认分类状态并记录操作。"
              : "填写分类标识、显示名称、排序和可用状态。"}
          </SheetDescription>
        </SheetHeader>

        <form
          id="category-editor-form"
          className="flex min-h-0 flex-1 flex-col"
          onSubmit={handleReview}
          noValidate
        >
          <div className="flex flex-1 flex-col gap-5 overflow-y-auto px-4 pb-6">
            {mutation.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>
                  {categoryMutationErrorTitle(mutation.error)}
                </AlertTitle>
                <AlertDescription>
                  {categoryMutationErrorDescription(mutation.error)}
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

            <FieldGroup>
              <Field data-invalid={Boolean(errors.id)}>
                <FieldLabel htmlFor="category-id">分类标识</FieldLabel>
                <Input
                  id="category-id"
                  name="id"
                  defaultValue={category?.id}
                  placeholder="例如 documentary"
                  maxLength={64}
                  pattern="[a-z0-9][a-z0-9-]{0,63}"
                  disabled={Boolean(category) || isPending}
                  aria-invalid={Boolean(errors.id)}
                  autoFocus={!category}
                />
                <FieldDescription>
                  创建后不可修改，只使用小写字母、数字和连字符。
                </FieldDescription>
                <FieldError
                  errors={errors.id ? [{ message: errors.id }] : []}
                />
              </Field>

              <Field data-invalid={Boolean(errors.name)}>
                <FieldLabel htmlFor="category-name">显示名称</FieldLabel>
                <Input
                  id="category-name"
                  name="name"
                  defaultValue={category?.name}
                  placeholder="例如 纪录片"
                  maxLength={40}
                  disabled={isPending}
                  aria-invalid={Boolean(errors.name)}
                  autoFocus={Boolean(category)}
                />
                <FieldError
                  errors={errors.name ? [{ message: errors.name }] : []}
                />
              </Field>

              <Field data-invalid={Boolean(errors.displayOrder)}>
                <FieldLabel htmlFor="category-display-order">
                  排序权重
                </FieldLabel>
                <Input
                  id="category-display-order"
                  name="displayOrder"
                  type="number"
                  inputMode="numeric"
                  min={0}
                  max={1_000_000}
                  step={1}
                  defaultValue={category?.display_order ?? 100}
                  disabled={isPending}
                  aria-invalid={Boolean(errors.displayOrder)}
                />
                <FieldDescription>
                  数字越小越靠前；相同权重按分类标识排序。
                </FieldDescription>
                <FieldError
                  errors={
                    errors.displayOrder
                      ? [{ message: errors.displayOrder }]
                      : []
                  }
                />
              </Field>

              <Field>
                <FieldLabel>可用状态</FieldLabel>
                <ToggleGroup
                  value={[enabled ? "enabled" : "disabled"]}
                  onValueChange={(values) => {
                    const nextValue = values[0]
                    if (nextValue === "enabled" || nextValue === "disabled") {
                      setEnabled(nextValue === "enabled")
                    }
                  }}
                  variant="outline"
                  spacing={0}
                  disabled={isPending}
                  aria-label="分类可用状态"
                >
                  <ToggleGroupItem
                    value="enabled"
                    className="aria-pressed:bg-success/10 aria-pressed:text-success-foreground"
                  >
                    <PowerIcon data-icon="inline-start" />
                    启用
                  </ToggleGroupItem>
                  <ToggleGroupItem
                    value="disabled"
                    className="aria-pressed:bg-destructive/10 aria-pressed:text-destructive"
                  >
                    <PowerOffIcon data-icon="inline-start" />
                    停用
                  </ToggleGroupItem>
                </ToggleGroup>
                <FieldDescription>
                  停用只会从公开分类和种子列表中隐藏，不会删除已有引用。
                </FieldDescription>
              </Field>

              {disablingReferencedCategory ? (
                <Alert>
                  <TriangleAlertIcon />
                  <AlertTitle>将影响公开内容</AlertTitle>
                  <AlertDescription>
                    当前有 {category?.torrent_count.toLocaleString("zh-CN")}
                    个种子引用此分类。停用后它们不会被删除，但会退出公开列表。
                  </AlertDescription>
                </Alert>
              ) : null}

              <Field data-invalid={Boolean(errors.reason)}>
                <FieldLabel htmlFor="category-change-reason">
                  变更理由
                </FieldLabel>
                <Textarea
                  id="category-change-reason"
                  name="reason"
                  rows={4}
                  minLength={10}
                  maxLength={500}
                  placeholder="记录依据、预期影响和必要背景（10–500 字）…"
                  disabled={isPending}
                  aria-invalid={Boolean(errors.reason)}
                />
                <FieldDescription>
                  完整理由会安全保存，审计记录仅保留必要摘要。
                </FieldDescription>
                <FieldError
                  errors={errors.reason ? [{ message: errors.reason }] : []}
                />
              </Field>
            </FieldGroup>
          </div>

          <SheetFooter className="border-t bg-muted/30 sm:flex-row sm:justify-end">
            <Button
              type="button"
              variant="outline"
              disabled={isPending}
              onClick={() => onOpenChange(false)}
            >
              取消
            </Button>
            <Button type="submit" disabled={isPending}>
              {category ? (
                <SaveIcon data-icon="inline-start" />
              ) : (
                <PlusIcon data-icon="inline-start" />
              )}
              审阅变更
            </Button>
          </SheetFooter>
        </form>

        <CategoryConfirmationDialog
          open={confirmationOpen}
          category={category}
          values={pendingValues}
          pending={isPending}
          onOpenChange={setConfirmationOpen}
          onConfirm={() => void handleConfirm()}
        />
      </SheetContent>
    </Sheet>
  )
}

function CategoryConfirmationDialog({
  open,
  category,
  values,
  pending,
  onOpenChange,
  onConfirm,
}: {
  open: boolean
  category?: ManagedCategory
  values?: CategoryFormValues
  pending: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}) {
  if (!values) {
    return null
  }
  const disabling = Boolean(category?.enabled && !values.enabled)
  const changes = category ? categoryChanges(category, values) : []

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
            {disabling ? <TriangleAlertIcon /> : <ClipboardCheckIcon />}
          </AlertDialogMedia>
          <AlertDialogTitle>
            {category ? "确认分类变更" : "确认创建分类"}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {category
              ? "保存前会重新确认分类状态；若已被其他管理员修改，本次操作会停止。"
              : "分类标识创建后不可修改，请在提交前核对。"}
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="grid gap-2 rounded-lg border bg-muted/30 p-3 text-sm">
          {category ? (
            changes.map((change) => (
              <div
                key={change.label}
                className="grid grid-cols-[5rem_1fr] gap-2"
              >
                <span className="text-muted-foreground">{change.label}</span>
                <span className="min-w-0 break-words">
                  <span className="text-muted-foreground line-through">
                    {change.before}
                  </span>{" "}
                  → {change.after}
                </span>
              </div>
            ))
          ) : (
            <>
              <ConfirmationFact label="分类标识" value={values.id} />
              <ConfirmationFact label="显示名称" value={values.name} />
              <ConfirmationFact
                label="排序权重"
                value={String(values.displayOrder)}
              />
              <ConfirmationFact
                label="状态"
                value={categoryStatusLabel(values.enabled)}
              />
            </>
          )}
        </div>

        {disabling && category && category.torrent_count > 0 ? (
          <Alert>
            <TriangleAlertIcon />
            <AlertTitle>确认停用影响</AlertTitle>
            <AlertDescription>
              {category.torrent_count.toLocaleString("zh-CN")}
              个引用种子将退出公开列表，但不会被删除。
            </AlertDescription>
          </Alert>
        ) : null}

        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>返回修改</AlertDialogCancel>
          <AlertDialogAction
            variant={disabling ? "destructive" : "default"}
            disabled={pending}
            onClick={onConfirm}
          >
            {pending ? <Spinner /> : null}
            {pending ? "正在提交…" : category ? "确认变更" : "确认创建"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function categoryChanges(
  category: ManagedCategory,
  values: CategoryFormValues
) {
  return [
    category.name !== values.name
      ? { label: "显示名称", before: category.name, after: values.name }
      : null,
    category.display_order !== values.displayOrder
      ? {
          label: "排序权重",
          before: String(category.display_order),
          after: String(values.displayOrder),
        }
      : null,
    category.enabled !== values.enabled
      ? {
          label: "状态",
          before: categoryStatusLabel(category.enabled),
          after: categoryStatusLabel(values.enabled),
        }
      : null,
  ].filter((change): change is NonNullable<typeof change> => change !== null)
}

function ConfirmationFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[5rem_1fr] gap-2">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words">{value}</span>
    </div>
  )
}

function categoryStatusLabel(enabled: boolean) {
  return enabled ? "启用" : "停用"
}

function categoryMutationErrorTitle(error: Error) {
  if (error instanceof ApiProblemError) {
    switch (error.code) {
      case "category_version_conflict":
        return "分类已被其他操作更新"
      case "category_exists":
        return "分类标识已经存在"
      case "category_change_denied":
        return "当前职责不能执行这项变更"
      case "invalid_category":
        return "分类信息未通过服务端校验"
    }
  }
  return "分类变更提交失败"
}

function categoryMutationErrorDescription(error: Error) {
  if (
    error instanceof ApiProblemError &&
    error.code === "category_version_conflict"
  ) {
    return "列表正在重新读取最新版本。请关闭编辑面板后，基于新版本重新发起变更。"
  }
  return "服务端没有提交任何部分状态；请核对字段、权限与当前任期后重试。"
}
