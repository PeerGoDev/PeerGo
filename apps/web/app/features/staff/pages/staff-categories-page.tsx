import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CheckCircle2Icon,
  CircleAlertIcon,
  FolderTreeIcon,
  PlusIcon,
  RefreshCwIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Skeleton } from "~/components/ui/skeleton"
import {
  type ManagedCategory,
  managedCategoryListQueryOptions,
} from "~/features/staff/api/category-administration.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { CategoryEditorSheet } from "~/features/staff/components/category-editor-sheet"
import { CategoryFacetManagerSheet } from "~/features/staff/components/category-facet-manager-sheet"
import { CategoryTable } from "~/features/staff/components/category-table"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"

type CapabilityList = components["schemas"]["CapabilityList"]

type EditorState =
  | { mode: "create" }
  | { mode: "edit"; category: ManagedCategory }

export function StaffCategoriesPage() {
  return (
    <StaffAccessGate
      requiredAction="category.manage.read"
      pageHeader={{
        title: "分类管理",
        description:
          "每个分类拥有独立名称和排序；停用后仍保留已有种子的历史归属。",
        descriptionClassName: "mt-3",
      }}
    >
      {({ session, capabilities }) => (
        <CategoriesContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function CategoriesContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const categories = useQuery(managedCategoryListQueryOptions)
  const [editor, setEditor] = React.useState<EditorState>()
  const [facetCategoryId, setFacetCategoryId] = React.useState("")
  const [successMessage, setSuccessMessage] = React.useState("")
  const canCreate = hasCapability(capabilities, "category.create")
  const canUpdate = hasCapability(capabilities, "category.update")
  const facetCategory = categories.data?.find(
    (category) => category.id === facetCategoryId
  )

  if (categories.isPending) {
    return <CategoriesSkeleton />
  }
  if (categories.isError || !categories.data) {
    return (
      <CategoriesFrame>
        <CategoriesHeader />
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>分类列表暂时无法读取</AlertTitle>
          <AlertDescription>
            暂时无法取得分类数据，请稍后重试。
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void categories.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      </CategoriesFrame>
    )
  }

  return (
    <CategoriesFrame>
      <CategoriesHeader>
        {canCreate ? (
          <Button
            variant="outline"
            size="sm"
            className="w-26"
            onClick={() => {
              setSuccessMessage("")
              setEditor({ mode: "create" })
            }}
          >
            <PlusIcon data-icon="inline-start" />
            新建分类
          </Button>
        ) : null}
        <Button
          variant="outline"
          size="sm"
          className="w-19"
          onClick={() => void categories.refetch()}
          disabled={categories.isFetching}
        >
          <RefreshCwIcon data-icon="inline-start" />
          {categories.isFetching ? "刷新中…" : "刷新"}
        </Button>
      </CategoriesHeader>

      <p className="text-sm text-muted-foreground">
        每个分类拥有独立名称和排序；停用后仍保留已有种子的历史归属。使用编辑操作调整顺序与状态。
      </p>

      {successMessage ? (
        <Alert>
          <CheckCircle2Icon />
          <AlertTitle>分类目录已更新</AlertTitle>
          <AlertDescription>{successMessage}</AlertDescription>
        </Alert>
      ) : null}

      {!canCreate && !canUpdate ? (
        <Alert>
          <FolderTreeIcon />
          <AlertTitle>当前权限仅可查看</AlertTitle>
          <AlertDescription>
            可以查看分类状态和引用数量，但不能创建或更新分类。
          </AlertDescription>
        </Alert>
      ) : null}

      <CategoryTable
        categories={categories.data}
        canUpdate={canUpdate}
        onEdit={(category) => {
          setSuccessMessage("")
          setEditor({ mode: "edit", category })
        }}
        onManageFacets={(category) => {
          setSuccessMessage("")
          setFacetCategoryId(category.id)
        }}
      />

      {editor ? (
        <CategoryEditorSheet
          key={
            editor.mode === "create"
              ? "create"
              : `${editor.category.id}:${editor.category.version}`
          }
          category={editor.mode === "edit" ? editor.category : undefined}
          csrfToken={csrfToken}
          onOpenChange={(open) => {
            if (!open) {
              setEditor(undefined)
            }
          }}
          onSaved={(savedCategory, mode) => {
            setSuccessMessage(
              mode === "created"
                ? `已创建“${savedCategory.name}”。`
                : `已保存“${savedCategory.name}”。`
            )
            setEditor(undefined)
          }}
        />
      ) : null}

      {facetCategory ? (
        <CategoryFacetManagerSheet
          category={facetCategory}
          csrfToken={csrfToken}
          canUpdate={canUpdate}
          onOpenChange={(open) => {
            if (!open) setFacetCategoryId("")
          }}
          onSaved={setSuccessMessage}
        />
      ) : null}
    </CategoriesFrame>
  )
}

function CategoriesHeader({ children }: { children?: React.ReactNode }) {
  return (
    <header className="flex min-h-9 items-center justify-between gap-4">
      <h1 className="font-heading text-xl font-semibold">分类管理</h1>
      {children ? (
        <div className="flex flex-wrap items-center justify-end gap-2">
          {children}
        </div>
      ) : null}
    </header>
  )
}

function CategoriesFrame({ children }: { children: React.ReactNode }) {
  return <StaffPageFrame className="gap-4">{children}</StaffPageFrame>
}

function CategoriesSkeleton() {
  return (
    <CategoriesFrame>
      <div
        className="flex flex-col gap-2"
        aria-label="正在加载分类目录"
        aria-busy="true"
      >
        <Skeleton className="h-9 w-48" />
        <Skeleton className="h-4 w-full max-w-lg" />
      </div>
      <div className="grid gap-2">
        {Array.from({ length: 7 }, (_, index) => (
          <Skeleton key={index} className="h-[62px] w-full rounded-lg" />
        ))}
      </div>
    </CategoriesFrame>
  )
}
