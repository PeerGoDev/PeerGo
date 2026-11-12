import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CalendarClockIcon,
  CheckCircle2Icon,
  CircleAlertIcon,
  ClipboardCheckIcon,
  RefreshCwIcon,
  SendIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Card } from "~/components/ui/card"
import {
  Dialog,
  DialogClose,
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
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import {
  type WorkgroupTaskAssignment,
  myWorkgroupTasksQueryOptions,
  useSubmitWorkgroupTask,
} from "~/features/workgroups/api/workgroups.queries"
import { cn } from "~/lib/utils"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

export function WorkgroupTaskPanel({
  userId,
  csrfToken,
}: {
  userId: string
  csrfToken: string
}) {
  const [selected, setSelected] = React.useState<WorkgroupTaskAssignment>()
  const tasks = useQuery(myWorkgroupTasksQueryOptions(userId))

  return (
    <section
      aria-labelledby="workgroup-tasks-title"
      className="flex flex-col gap-3"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-0.5">
          <h2
            id="workgroup-tasks-title"
            className="text-md font-semibold tracking-title"
          >
            我的工作组任务
          </h2>
          <p className="text-xs text-muted-foreground">
            这里只显示发布时已经分配给你的任务和活动；结果由工作人员人工验收。
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          className="shrink-0"
          onClick={() => void tasks.refetch()}
          disabled={tasks.isFetching}
        >
          {tasks.isFetching ? (
            <Spinner data-icon="inline-start" />
          ) : (
            <RefreshCwIcon data-icon="inline-start" />
          )}
          刷新
        </Button>
      </div>
      {tasks.isPending ? <Skeleton className="h-32 w-full rounded-xl" /> : null}
      {tasks.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>任务暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              tasks.error,
              "请稍后刷新，不会影响已经提交的成果。"
            )}
          </AlertDescription>
        </Alert>
      ) : null}
      {tasks.data?.items.length === 0 ? (
        <Empty className="border bg-card">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ClipboardCheckIcon />
            </EmptyMedia>
            <EmptyTitle>目前没有分配给你的任务</EmptyTitle>
            <EmptyDescription>
              新任务只会分配给发布当时处于有效状态的工作组成员。
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : null}
      {tasks.data?.items.length ? (
        <Card className="gap-0 py-0">
          <ul className="divide-y">
            {tasks.data.items.map((assignment) => (
              <TaskRow
                key={assignment.id}
                assignment={assignment}
                onSubmit={() => setSelected(assignment)}
              />
            ))}
          </ul>
        </Card>
      ) : null}
      <TaskSubmissionDialog
        assignment={selected}
        userId={userId}
        csrfToken={csrfToken}
        onOpenChange={(open) => !open && setSelected(undefined)}
      />
    </section>
  )
}

function TaskRow({
  assignment,
  onSubmit,
}: {
  assignment: WorkgroupTaskAssignment
  onSubmit: () => void
}) {
  const submission = assignment.latest_submission
  const accepted = submission?.decision === "accepted"

  return (
    <li className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:gap-4 sm:px-5">
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="text-sm font-semibold">{assignment.task.title}</h3>
          <Badge variant="secondary">
            {taskTypeLabel(assignment.task.task_type)}
          </Badge>
          <Badge variant={assignmentStateVariant(assignment.state)}>
            {assignmentStateLabel(assignment.state)}
          </Badge>
        </div>
        {assignment.task.description ? (
          <p className="text-xs text-muted-foreground">
            {assignment.task.description}
          </p>
        ) : null}
        <p className="mt-1 flex items-start gap-1.5 text-xs text-faint">
          <CalendarClockIcon className="mt-px size-3.5 shrink-0" />
          <span>
            {formatDateTime(assignment.task.starts_at)} 至{" "}
            {formatDateTime(assignment.task.due_at)}
          </span>
        </p>
        {submission ? (
          <div className="mt-2 flex flex-col gap-1.5 rounded-lg bg-muted px-3 py-2.5 text-xs">
            <p className="font-medium text-foreground">
              第 {submission.sequence} 次成果说明
            </p>
            <p className="whitespace-pre-wrap text-muted-foreground">
              {submission.statement}
            </p>
            {submission.review_reason ? (
              <p
                className={cn(
                  "flex items-start gap-1.5",
                  accepted ? "text-success-foreground" : "text-destructive"
                )}
              >
                {accepted ? (
                  <CheckCircle2Icon className="mt-px size-3.5 shrink-0" />
                ) : (
                  <CircleAlertIcon className="mt-px size-3.5 shrink-0" />
                )}
                <span>
                  <span className="font-medium">
                    {accepted ? "已通过验收" : "需要修改"}：
                  </span>
                  {submission.review_reason}
                </span>
              </p>
            ) : null}
          </div>
        ) : null}
      </div>
      {assignment.can_submit ? (
        <Button
          size="sm"
          className="shrink-0 max-sm:w-full sm:self-center"
          onClick={onSubmit}
        >
          <SendIcon data-icon="inline-start" />
          {assignment.state === "changes_requested" ? "重新提交" : "提交成果"}
        </Button>
      ) : null}
    </li>
  )
}

function TaskSubmissionDialog({
  assignment,
  userId,
  csrfToken,
  onOpenChange,
}: {
  assignment?: WorkgroupTaskAssignment
  userId: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const mutation = useSubmitWorkgroupTask(userId)
  const [statement, setStatement] = React.useState("")
  const valid = statement.trim().length >= 10 && statement.trim().length <= 2000

  React.useEffect(() => {
    if (assignment) {
      setStatement(assignment.latest_submission?.statement ?? "")
      mutation.reset()
    }
  }, [assignment]) // eslint-disable-line react-hooks/exhaustive-deps

  const submit = () => {
    if (!assignment || !valid) return
    mutation.mutate(
      {
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        assignmentId: assignment.id,
        statement: statement.trim(),
      },
      { onSuccess: () => onOpenChange(false) }
    )
  }

  return (
    <Dialog open={Boolean(assignment)} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {assignment?.state === "changes_requested"
              ? "重新提交成果"
              : "提交任务成果"}
          </DialogTitle>
          <DialogDescription>
            请写明完成内容和可核对信息。提交后需要工作人员人工验收。
          </DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field data-invalid={statement.length > 0 && !valid}>
            <FieldLabel htmlFor="workgroup-task-statement">成果说明</FieldLabel>
            <Textarea
              id="workgroup-task-statement"
              value={statement}
              onChange={(event) => setStatement(event.target.value)}
              rows={7}
              maxLength={2000}
              aria-invalid={statement.length > 0 && !valid}
              disabled={mutation.isPending}
            />
            <FieldDescription>
              10–2000 个字符，不要填写敏感信息。
            </FieldDescription>
            <FieldError>
              {statement.length > 0 && !valid
                ? "成果说明长度不符合要求。"
                : null}
            </FieldError>
          </Field>
        </FieldGroup>
        {mutation.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>提交失败</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(
                mutation.error,
                "任务状态可能已经变化，请刷新后重试。"
              )}
            </AlertDescription>
          </Alert>
        ) : null}
        <DialogFooter>
          <DialogClose
            render={<Button variant="outline" disabled={mutation.isPending} />}
          >
            取消
          </DialogClose>
          <Button onClick={submit} disabled={!valid || mutation.isPending}>
            {mutation.isPending ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <SendIcon data-icon="inline-start" />
            )}
            提交成果
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function taskTypeLabel(value: string) {
  return value === "activity" ? "限时活动" : "工作任务"
}

function assignmentStateVariant(value: string) {
  switch (value) {
    case "pending_review":
      return "warning" as const
    case "changes_requested":
      return "destructive" as const
    case "accepted":
      return "success" as const
    default:
      return "primary" as const
  }
}

function assignmentStateLabel(value: string) {
  switch (value) {
    case "pending_review":
      return "等待验收"
    case "changes_requested":
      return "需要修改"
    case "accepted":
      return "已通过"
    default:
      return "待提交"
  }
}
