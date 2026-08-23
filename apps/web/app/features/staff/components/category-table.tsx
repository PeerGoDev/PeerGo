import { FolderTreeIcon, ListTreeIcon, PencilIcon } from "lucide-react"

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
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import type { ManagedCategory } from "~/features/staff/api/category-administration.queries"
import { torrentCategoryIcon } from "~/features/torrent/model/category-icon"
import { formatDateTime } from "~/shared/formatters/date-time"

export function CategoryTable({
  categories,
  canUpdate,
  onEdit,
  onManageFacets,
}: {
  categories: ManagedCategory[]
  canUpdate: boolean
  onEdit: (category: ManagedCategory) => void
  onManageFacets: (category: ManagedCategory) => void
}) {
  if (categories.length === 0) {
    return (
      <Empty className="min-h-64 border">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FolderTreeIcon />
          </EmptyMedia>
          <EmptyTitle>尚未建立分类</EmptyTitle>
          <EmptyDescription>
            创建后分类标识不可修改；名称、顺序和状态可以随运营需要调整。
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <>
      <div className="hidden gap-2 md:grid">
        {categories.map((category) => {
          const CategoryIcon = torrentCategoryIcon(category.id, category.name)
          return (
            <div
              key={category.id}
              className="flex h-[62px] items-center gap-3 rounded-lg border bg-card px-4 shadow-sm"
            >
              <CategoryIcon
                className="size-5 shrink-0 text-primary"
                aria-hidden="true"
                data-category-icon={category.id}
              />
              <div className="flex min-w-0 flex-1 items-baseline gap-2">
                <span className="truncate font-semibold">{category.name}</span>
                <code className="truncate text-xs text-muted-foreground">
                  ({category.id})
                </code>
              </div>
              <span className="hidden text-xs text-muted-foreground lg:inline">
                排序 {category.display_order.toLocaleString("zh-CN")}
              </span>
              <span className="hidden min-w-20 text-right text-xs text-muted-foreground sm:inline">
                {category.torrent_count.toLocaleString("zh-CN")} 个种子
              </span>
              <CategoryStatusBadge enabled={category.enabled} />
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onManageFacets(category)}
                aria-label={`管理 ${category.name} 的类型与属性`}
              >
                <ListTreeIcon data-icon="inline-start" />
                类型
              </Button>
              {canUpdate ? (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => onEdit(category)}
                  aria-label={`编辑分类 ${category.name}`}
                >
                  <PencilIcon />
                </Button>
              ) : (
                <span className="text-xs text-muted-foreground">只读</span>
              )}
            </div>
          )
        })}
      </div>

      <div className="grid gap-3 md:hidden">
        {categories.map((category) => (
          <Card key={category.id} size="sm">
            <CardHeader>
              <CardTitle>{category.name}</CardTitle>
              <code className="text-xs text-muted-foreground">
                {category.id} · 第 {category.version} 版
              </code>
              <CardAction>
                <CategoryStatusBadge enabled={category.enabled} />
              </CardAction>
            </CardHeader>
            <CardContent className="grid grid-cols-2 gap-3 text-xs">
              <CategoryFact label="排序权重" value={category.display_order} />
              <CategoryFact label="引用种子" value={category.torrent_count} />
              <div className="col-span-2 flex items-center justify-between gap-3 border-t pt-3">
                <time
                  dateTime={category.updated_at}
                  className="text-muted-foreground"
                >
                  更新于 {formatDateTime(category.updated_at)}
                </time>
                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => onManageFacets(category)}
                  >
                    <ListTreeIcon data-icon="inline-start" />
                    类型
                  </Button>
                  {canUpdate ? (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => onEdit(category)}
                      aria-label={`编辑分类 ${category.name}`}
                    >
                      <PencilIcon data-icon="inline-start" />
                      编辑
                    </Button>
                  ) : null}
                </div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </>
  )
}

function CategoryStatusBadge({ enabled }: { enabled: boolean }) {
  return enabled ? (
    <Badge
      variant="outline"
      className="border-success/30 bg-success/10 text-success-foreground"
    >
      启用
    </Badge>
  ) : (
    <Badge variant="destructive">停用</Badge>
  )
}

function CategoryFact({ label, value }: { label: string; value: number }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium tabular-nums">
        {value.toLocaleString("zh-CN")}
      </span>
    </div>
  )
}
