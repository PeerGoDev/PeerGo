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
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
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
    <>
      <Card>
        <CardHeader>
          <CardTitle>我的工作组任务</CardTitle>
          <CardDescription>
            这里只显示发布时已经分配给你的任务和活动；结果由工作人员人工验收。
          </CardDescription>
          <CardAction>
            <Button
              variant="outline"
              size="sm"
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
          </CardAction>
        </CardHeader>
        <CardContent>
          {tasks.isPending ? <Skeleton className="h-40 w-full" /> : null}
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
            <Empty className="border">
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
            <div className="grid gap-3 lg:grid-cols-2">
              {tasks.data.items.map((assignment) => (
                <TaskCard
                  key={assignment.id}
                  assignment={assignment}
                  onSubmit={() => setSelected(assignment)}
                />
              ))}
            </div>
          ) : null}
        </CardContent>
      </Card>
      <TaskSubmissionDialog
        assignment={selected}
        userId={userId}
        csrfToken={csrfToken}
        onOpenChange={(open) => !open && setSelected(undefined)}
      />
    </>
  )
}

function TaskCard({
  assignment,
  onSubmit,
}: {
  assignment: WorkgroupTaskAssignment
  onSubmit: () => void
}) {
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>{assignment.task.title}</CardTitle>
        <CardDescription>{assignment.task.description}</CardDescription>
        <CardAction className="flex items-center gap-2">
          <Badge variant="outline">
            {taskTypeLabel(assignment.task.task_type)}
          </Badge>
          <Badge
            variant={assignment.state === "accepted" ? "default" : "secondary"}
          >
            {assignmentStateLabel(assignment.state)}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex items-start gap-2 text-sm text-muted-foreground">
          <CalendarClockIcon className="mt-0.5 size-4 shrink-0" />
          <span>
            {formatDateTime(assignment.task.starts_at)} 至{" "}
            {formatDateTime(assignment.task.due_at)}
          </span>
        </div>
        {assignment.latest_submission ? (
          <div className="flex flex-col gap-2 rounded-md border p-3 text-sm">
            <p className="font-medium">
              第 {assignment.latest_submission.sequence} 次成果说明
            </p>
            <p className="whitespace-pre-wrap text-muted-foreground">
              {assignment.latest_submission.statement}
            </p>
            {assignment.latest_submission.review_reason ? (
              <Alert
                variant={
                  assignment.latest_submission.decision === "accepted"
                    ? "default"
                    : "destructive"
                }
              >
                {assignment.latest_submission.decision === "accepted" ? (
                  <CheckCircle2Icon />
                ) : (
                  <CircleAlertIcon />
                )}
                <AlertTitle>
                  {assignment.latest_submission.decision === "accepted"
                    ? "已通过验收"
                    : "需要修改"}
                </AlertTitle>
                <AlertDescription>
                  {assignment.latest_submission.review_reason}
                </AlertDescription>
              </Alert>
            ) : null}
          </div>
        ) : null}
      </CardContent>
      {assignment.can_submit ? (
        <CardFooter>
          <Button size="sm" onClick={onSubmit}>
            <SendIcon data-icon="inline-start" />
            {assignment.state === "changes_requested" ? "重新提交" : "提交成果"}
          </Button>
        </CardFooter>
      ) : null}
    </Card>
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
  const requestId = React.useRef<string | undefined>(undefined)
  const valid = statement.trim().length >= 10 && statement.trim().length <= 2000

  React.useEffect(() => {
    if (assignment) {
      setStatement(assignment.latest_submission?.statement ?? "")
      requestId.current = undefined
      mutation.reset()
    }
  }, [assignment]) // eslint-disable-line react-hooks/exhaustive-deps

  const submit = () => {
    if (!assignment || !valid) return
    const idempotencyKey = requestId.current ?? crypto.randomUUID()
    requestId.current = idempotencyKey
    mutation.mutate(
      {
        csrfToken,
        idempotencyKey,
        assignmentId: assignment.id,
        statement: statement.trim(),
      },
      {
        onSuccess: () => {
          requestId.current = undefined
          onOpenChange(false)
        },
      }
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
              onChange={(event) => {
                requestId.current = undefined
                setStatement(event.target.value)
              }}
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
