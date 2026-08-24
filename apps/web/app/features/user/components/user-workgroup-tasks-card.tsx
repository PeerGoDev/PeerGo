import { Link } from "react-router"

import { Badge } from "~/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import type { WorkgroupTaskAssignmentPage } from "~/features/workgroups/api/workgroups.queries"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

const groupLabel = {
  reseed: "补种组",
  review: "种审组",
  retention: "保种组",
} as const
const stateLabel = {
  not_submitted: "待提交",
  pending_review: "待验收",
  changes_requested: "需修改",
  accepted: "已完成",
} as const

export function UserWorkgroupTasksCard({
  page,
  loading,
}: {
  page: WorkgroupTaskAssignmentPage | undefined
  loading: boolean
}) {
  const active = (page?.items ?? []).filter(
    (item) => item.state !== "accepted" && item.task.timeline_state !== "closed"
  )
  return (
    <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
      <CardHeader className="px-6 pt-6 pb-4">
        <CardTitle>
          <h2>活跃工作组任务</h2>
        </CardTitle>
        <CardDescription>仅自己可见的种审、补种与保种任务。</CardDescription>
      </CardHeader>
      <CardContent className="px-6 pb-6">
        {loading && !page ? (
          <p className="py-5 text-sm text-muted-foreground">正在加载任务…</p>
        ) : active.length === 0 ? (
          <p className="py-5 text-sm text-muted-foreground">
            当前没有待处理任务。
          </p>
        ) : (
          <div className="divide-y rounded-lg border">
            {active.map((assignment) => (
              <Link
                key={assignment.id}
                to="/workgroups"
                className="flex flex-wrap items-center justify-between gap-3 p-3 transition-colors hover:bg-muted/50"
              >
                <div className="min-w-0">
                  <p className="truncate font-medium">
                    {assignment.task.title}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    截止{" "}
                    <time dateTime={assignment.task.due_at}>
                      {formatCompactDateTime(assignment.task.due_at)}
                    </time>
                  </p>
                </div>
                <div className="flex gap-2">
                  <Badge variant="outline">
                    {groupLabel[assignment.task.group_kind]}
                  </Badge>
                  <Badge variant="secondary">
                    {stateLabel[assignment.state]}
                  </Badge>
                </div>
              </Link>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
