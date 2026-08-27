import type { ReactNode } from "react"
import { ChevronRightIcon, FolderTreeIcon, PencilIcon } from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader } from "~/components/ui/card"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "~/components/ui/collapsible"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import type { ManagedCategory } from "~/features/staff/api/category-administration.queries"
import { torrentCategoryIcon } from "~/features/torrent/model/category-icon"
import { cn } from "~/lib/utils"

export function CategoryTable({
  categories,
  canUpdate,
  expandedCategoryId,
  onEdit,
  onToggleFacets,
  renderFacetManager,
}: {
  categories: ManagedCategory[]
  canUpdate: boolean
  expandedCategoryId: string
  onEdit: (category: ManagedCategory) => void
  onToggleFacets: (category: ManagedCategory) => void
  renderFacetManager: (category: ManagedCategory) => ReactNode
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
    <div className="grid gap-2">
      {categories.map((category) => {
        const CategoryIcon = torrentCategoryIcon(category.id, category.name)
        const expanded = category.id === expandedCategoryId

        return (
          <Collapsible
            key={category.id}
            open={expanded}
            onOpenChange={() => onToggleFacets(category)}
          >
            <Card
              size="sm"
              className={cn("gap-0 py-0", !category.enabled && "opacity-70")}
            >
              <CardHeader className="min-h-[62px] px-3 py-2 sm:px-4">
                <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center">
                  <CollapsibleTrigger
                    render={
                      <Button
                        type="button"
                        variant="ghost"
                        className="h-auto min-w-0 flex-1 justify-start gap-3 px-1 py-2 text-left sm:px-2"
                        aria-label={`${expanded ? "收起" : "展开"} ${category.name} 的类型与属性`}
                      />
                    }
                  >
                    <ChevronRightIcon
                      data-icon="inline-start"
                      className="transition-transform in-data-open:rotate-90"
                    />
                    <CategoryIcon
                      data-icon="inline-start"
                      className="text-primary"
                      aria-hidden="true"
                      data-category-icon={category.id}
                    />
                    <span className="flex min-w-0 flex-col items-start gap-0.5">
                      <span className="flex max-w-full min-w-0 items-baseline gap-2">
                        <span className="truncate font-semibold">
                          {category.name}
                        </span>
                        <code className="truncate text-xs font-normal text-muted-foreground">
                          ({category.id})
                        </code>
                      </span>
                      <span className="text-xs font-normal text-muted-foreground sm:hidden">
                        {category.facets.length} 个属性 · 排序{" "}
                        {category.display_order.toLocaleString("zh-CN")} ·{" "}
                        {category.torrent_count.toLocaleString("zh-CN")} 个种子
                      </span>
                    </span>
                  </CollapsibleTrigger>

                  <div className="flex shrink-0 items-center justify-end gap-2 px-1 sm:px-0">
                    <span className="hidden text-xs text-muted-foreground md:inline">
                      {category.facets.length} 个属性
                    </span>
                    <span className="hidden text-xs text-muted-foreground lg:inline">
                      排序 {category.display_order.toLocaleString("zh-CN")}
                    </span>
                    <span className="hidden min-w-20 text-right text-xs text-muted-foreground sm:inline">
                      {category.torrent_count.toLocaleString("zh-CN")} 个种子
                    </span>
                    <CategoryStatusBadge enabled={category.enabled} />
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
                      <span className="text-xs text-muted-foreground">
                        只读
                      </span>
                    )}
                  </div>
                </div>
              </CardHeader>

              <CollapsibleContent>
                <CardContent className="border-t bg-muted/20 px-3 py-4 sm:px-4">
                  {renderFacetManager(category)}
                </CardContent>
              </CollapsibleContent>
            </Card>
          </Collapsible>
        )
      })}
    </div>
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
