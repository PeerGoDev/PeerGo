import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CalendarClockIcon,
  CheckCircle2Icon,
  CircleAlertIcon,
  ClipboardCheckIcon,
  EyeIcon,
  PlusIcon,
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
import { Input } from "~/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "~/components/ui/sheet"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { Textarea } from "~/components/ui/textarea"
import {
  type WorkgroupKind,
  type WorkgroupTask,
  type WorkgroupTaskAssignment,
  type WorkgroupTaskReviewDecision,
  type WorkgroupTaskType,
  adminWorkgroupTaskAssignmentsQueryOptions,
  adminWorkgroupTasksQueryOptions,
  usePublishWorkgroupTask,
  useReviewWorkgroupTaskSubmission,
} from "~/features/staff/api/workgroup-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

const taskTypeOptions: { value: WorkgroupTaskType; label: string }[] = [
  { value: "task", label: "工作任务" },
  { value: "activity", label: "限时活动" },
]

type ReviewDialogState = {
  assignment: WorkgroupTaskAssignment
  decision: WorkgroupTaskReviewDecision
}

export function StaffWorkgroupTaskPanel({
  groupKind,
  csrfToken,
  canPublish,
  canReview,
}: {
  groupKind: WorkgroupKind
  csrfToken: string
  canPublish: boolean
  canReview: boolean
}) {
  const [publishOpen, setPublishOpen] = React.useState(false)
  const [selectedTask, setSelectedTask] = React.useState<WorkgroupTask>()
  const [reviewDialog, setReviewDialog] = React.useState<ReviewDialogState>()
  const tasks = useQuery(adminWorkgroupTasksQueryOptions(groupKind))

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle className="text-lg">
            任务与活动 · {groupLabel(groupKind)}
          </CardTitle>
          <CardDescription>
            发布时一次性冻结当前有效成员，后加入的成员不会被补分配；成果由工作人员人工验收。
          </CardDescription>
          <CardAction className="flex items-center gap-2">
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
            {canPublish ? (
              <Button size="sm" onClick={() => setPublishOpen(true)}>
                <PlusIcon data-icon="inline-start" />
                发布任务
              </Button>
            ) : null}
          </CardAction>
        </CardHeader>
        <CardContent>
          {tasks.isPending ? <Skeleton className="h-40 w-full" /> : null}
          {tasks.isError ? (
            <ReadError error={tasks.error} onRetry={() => tasks.refetch()} />
          ) : null}
          {tasks.data?.items.length === 0 ? (
            <Empty className="border">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ClipboardCheckIcon />
                </EmptyMedia>
                <EmptyTitle>还没有任务或活动</EmptyTitle>
                <EmptyDescription>
                  先维护有效成员，再发布首个任务；没有成员时不会创建空任务。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : null}
          {tasks.data?.items.length ? (
            <TaskTable
              items={tasks.data.items}
              onViewAssignments={setSelectedTask}
            />
          ) : null}
        </CardContent>
      </Card>

      <PublishTaskDialog
        open={publishOpen}
        groupKind={groupKind}
        csrfToken={csrfToken}
        onOpenChange={setPublishOpen}
      />
      {selectedTask ? (
        <TaskAssignmentsSheet
          task={selectedTask}
          groupKind={groupKind}
          canReview={canReview}
          onReview={(assignment, decision) =>
            setReviewDialog({ assignment, decision })
          }
          onOpenChange={(open) => !open && setSelectedTask(undefined)}
        />
      ) : null}
      {reviewDialog ? (
        <ReviewSubmissionDialog
          state={reviewDialog}
          csrfToken={csrfToken}
          onOpenChange={(open) => !open && setReviewDialog(undefined)}
        />
      ) : null}
    </>
  )
}

function TaskTable({
  items,
  onViewAssignments,
}: {
  items: WorkgroupTask[]
  onViewAssignments: (task: WorkgroupTask) => void
}) {
  return (
    <div className="overflow-x-auto rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>任务</TableHead>
            <TableHead>起止时间</TableHead>
            <TableHead>进度</TableHead>
            <TableHead>状态</TableHead>
            <TableHead className="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((task) => (
            <TableRow key={task.id}>
              <TableCell className="min-w-72">
                <div className="flex items-center gap-2 font-medium">
                  {task.title}
                  <Badge variant="outline">
                    {taskTypeLabel(task.task_type)}
                  </Badge>
                </div>
                <p className="mt-1 line-clamp-1 max-w-xl text-xs text-muted-foreground">
                  {task.description}
                </p>
              </TableCell>
              <TableCell className="min-w-56 text-sm text-muted-foreground">
                {formatDateTime(task.starts_at)}
                <span className="mx-1">—</span>
                {formatDateTime(task.due_at)}
              </TableCell>
              <TableCell className="min-w-48 text-sm tabular-nums">
                <div>
                  已交 {task.submitted_count}/{task.assignment_count}
                </div>
                <div className="mt-1 text-xs text-muted-foreground">
                  待验收 {task.pending_review_count} · 已通过{" "}
                  {task.accepted_count}
                </div>
              </TableCell>
              <TableCell>
                <Badge
                  variant={
                    task.timeline_state === "open" ? "default" : "secondary"
                  }
                >
                  {timelineStateLabel(task.timeline_state)}
                </Badge>
              </TableCell>
              <TableCell className="text-right">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => onViewAssignments(task)}
                >
                  <EyeIcon data-icon="inline-start" />
                  查看成员
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function PublishTaskDialog({
  open,
  groupKind,
  csrfToken,
  onOpenChange,
}: {
  open: boolean
  groupKind: WorkgroupKind
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const mutation = usePublishWorkgroupTask()
  const [taskType, setTaskType] = React.useState<WorkgroupTaskType>("task")
  const [title, setTitle] = React.useState("")
  const [description, setDescription] = React.useState("")
  const [startsAt, setStartsAt] = React.useState("")
  const [dueAt, setDueAt] = React.useState("")
  const titleValid = title.trim().length >= 2 && title.trim().length <= 100
  const descriptionValid =
    description.trim().length >= 10 && description.trim().length <= 2000
  const datesValid =
    Boolean(startsAt) &&
    Boolean(dueAt) &&
    Number.isFinite(new Date(startsAt).getTime()) &&
    new Date(dueAt).getTime() > new Date(startsAt).getTime()
  const valid = titleValid && descriptionValid && datesValid

  React.useEffect(() => {
    if (!open) return
    const start = new Date(Date.now() + 10 * 60 * 1000)
    const due = new Date(start.getTime() + 7 * 24 * 60 * 60 * 1000)
    setTaskType("task")
    setTitle("")
    setDescription("")
    setStartsAt(toDateTimeLocal(start))
    setDueAt(toDateTimeLocal(due))
    mutation.reset()
    // Reset publication fields whenever a fresh dialog is opened.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, groupKind])

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!valid) return
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        groupKind,
        taskType,
        title: title.trim(),
        description: description.trim(),
        startsAt: new Date(startsAt).toISOString(),
        dueAt: new Date(dueAt).toISOString(),
      })
      onOpenChange(false)
    } catch {
      // Keep the form open so the typed API problem remains visible.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>发布{groupLabel(groupKind)}任务或活动</DialogTitle>
          <DialogDescription>
            发布后内容和受众不可修改。当前所有有效成员会立即获得一份独立任务。
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="flex flex-col gap-5">
          <FieldGroup>
            {mutation.isError ? <MutationError error={mutation.error} /> : null}
            <Field>
              <FieldLabel htmlFor="workgroup-task-type">类型</FieldLabel>
              <Select
                items={taskTypeOptions}
                value={taskType}
                onValueChange={(value) => value && setTaskType(value)}
              >
                <SelectTrigger id="workgroup-task-type" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {taskTypeOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field data-invalid={title.length > 0 && !titleValid}>
              <FieldLabel htmlFor="workgroup-task-title">标题</FieldLabel>
              <Input
                id="workgroup-task-title"
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                maxLength={100}
                aria-invalid={title.length > 0 && !titleValid}
              />
              <FieldError>
                {title.length > 0 && !titleValid
                  ? "标题需要 2–100 个字符。"
                  : null}
              </FieldError>
            </Field>
            <Field data-invalid={description.length > 0 && !descriptionValid}>
              <FieldLabel htmlFor="workgroup-task-description">
                任务说明
              </FieldLabel>
              <Textarea
                id="workgroup-task-description"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                rows={5}
                maxLength={2000}
                aria-invalid={description.length > 0 && !descriptionValid}
              />
              <FieldDescription>
                写清完成标准和需要提交的核对信息，10–2000 个字符。
              </FieldDescription>
              <FieldError>
                {description.length > 0 && !descriptionValid
                  ? "任务说明长度不符合要求。"
                  : null}
              </FieldError>
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="workgroup-task-start">开始时间</FieldLabel>
                <Input
                  id="workgroup-task-start"
                  type="datetime-local"
                  value={startsAt}
                  onChange={(event) => setStartsAt(event.target.value)}
                />
              </Field>
              <Field data-invalid={Boolean(startsAt && dueAt && !datesValid)}>
                <FieldLabel htmlFor="workgroup-task-due">截止时间</FieldLabel>
                <Input
                  id="workgroup-task-due"
                  type="datetime-local"
                  value={dueAt}
                  onChange={(event) => setDueAt(event.target.value)}
                  aria-invalid={Boolean(startsAt && dueAt && !datesValid)}
                />
                <FieldError>
                  {startsAt && dueAt && !datesValid
                    ? "截止时间必须晚于开始时间。"
                    : null}
                </FieldError>
              </Field>
            </div>
          </FieldGroup>
          <DialogFooter>
            <DialogClose
              render={
                <Button variant="outline" disabled={mutation.isPending} />
              }
            >
              取消
            </DialogClose>
            <Button type="submit" disabled={!valid || mutation.isPending}>
              {mutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <SendIcon data-icon="inline-start" />
              )}
              确认发布
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function TaskAssignmentsSheet({
  task,
  groupKind,
  canReview,
  onReview,
  onOpenChange,
}: {
  task: WorkgroupTask
  groupKind: WorkgroupKind
  canReview: boolean
  onReview: (
    assignment: WorkgroupTaskAssignment,
    decision: WorkgroupTaskReviewDecision
  ) => void
  onOpenChange: (open: boolean) => void
}) {
  const assignments = useQuery(
    adminWorkgroupTaskAssignmentsQueryOptions(groupKind, task.id)
  )

  return (
    <Sheet open onOpenChange={onOpenChange}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-4xl">
        <SheetHeader>
          <SheetTitle>{task.title}</SheetTitle>
          <SheetDescription>
            {task.assignment_count} 位冻结受众 · {task.pending_review_count}{" "}
            份待验收
          </SheetDescription>
        </SheetHeader>
        <div className="p-4">
          {assignments.isPending ? <Skeleton className="h-56 w-full" /> : null}
          {assignments.isError ? (
            <ReadError
              error={assignments.error}
              onRetry={() => assignments.refetch()}
            />
          ) : null}
          {assignments.data ? (
            <div className="overflow-x-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>成员</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>最新成果</TableHead>
                    <TableHead className="text-right">验收</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {assignments.data.items.map((assignment) => (
                    <TableRow key={assignment.id}>
                      <TableCell className="min-w-44">
                        <div className="font-medium">
                          {assignment.display_name}
                        </div>
                        <div className="text-xs text-muted-foreground">
                          @{assignment.username} · #{assignment.user_numeric_id}
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant={
                            assignment.state === "accepted"
                              ? "default"
                              : "secondary"
                          }
                        >
                          {assignmentStateLabel(assignment.state)}
                        </Badge>
                      </TableCell>
                      <TableCell className="max-w-xl min-w-72 whitespace-normal">
                        {assignment.latest_submission ? (
                          <>
                            <p className="line-clamp-3 text-sm">
                              {assignment.latest_submission.statement}
                            </p>
                            <p className="mt-1 text-xs text-muted-foreground">
                              第 {assignment.latest_submission.sequence} 次 ·{" "}
                              {formatDateTime(
                                assignment.latest_submission.submitted_at
                              )}
                            </p>
                          </>
                        ) : (
                          <span className="text-sm text-muted-foreground">
                            尚未提交
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="text-right">
                        {canReview && assignment.state === "pending_review" ? (
                          <div className="flex justify-end gap-2">
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => onReview(assignment, "rejected")}
                            >
                              要求修改
                            </Button>
                            <Button
                              size="sm"
                              onClick={() => onReview(assignment, "accepted")}
                            >
                              <CheckCircle2Icon data-icon="inline-start" />
                              通过
                            </Button>
                          </div>
                        ) : (
                          <span className="text-xs text-muted-foreground">
                            —
                          </span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function ReviewSubmissionDialog({
  state,
  csrfToken,
  onOpenChange,
}: {
  state: ReviewDialogState
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const mutation = useReviewWorkgroupTaskSubmission()
  const [reason, setReason] = React.useState("")
  const valid = reason.trim().length >= 10 && reason.trim().length <= 1000
  const submission = state.assignment.latest_submission

  React.useEffect(() => {
    setReason("")
    mutation.reset()
    // Reset only when a different submission is selected.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [submission?.id, state.decision])

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!submission || !valid) return
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        submissionId: submission.id,
        decision: state.decision,
        reason: reason.trim(),
      })
      onOpenChange(false)
    } catch {
      // Keep the dialog open so the typed API problem remains visible.
    }
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {state.decision === "accepted" ? "通过任务成果" : "要求成员修改"}
          </DialogTitle>
          <DialogDescription>
            {state.assignment.display_name} 的第 {submission?.sequence ?? "—"}{" "}
            次提交。
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="flex flex-col gap-5">
          <FieldGroup>
            {mutation.isError ? <MutationError error={mutation.error} /> : null}
            <Field data-invalid={reason.length > 0 && !valid}>
              <FieldLabel htmlFor="workgroup-task-review-reason">
                验收说明
              </FieldLabel>
              <Textarea
                id="workgroup-task-review-reason"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                rows={5}
                maxLength={1000}
                aria-invalid={reason.length > 0 && !valid}
              />
              <FieldDescription>
                说明核对结果；要求修改时请写清具体缺项，10–1000 个字符。
              </FieldDescription>
              <FieldError>
                {reason.length > 0 && !valid
                  ? "验收说明长度不符合要求。"
                  : null}
              </FieldError>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <DialogClose
              render={
                <Button variant="outline" disabled={mutation.isPending} />
              }
            >
              取消
            </DialogClose>
            <Button
              type="submit"
              variant={
                state.decision === "accepted" ? "default" : "destructive"
              }
              disabled={!valid || mutation.isPending}
            >
              {mutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : state.decision === "accepted" ? (
                <CheckCircle2Icon data-icon="inline-start" />
              ) : (
                <CircleAlertIcon data-icon="inline-start" />
              )}
              确认{state.decision === "accepted" ? "通过" : "退回"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function MutationError({ error }: { error: Error }) {
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>操作失败</AlertTitle>
      <AlertDescription>
        {requestErrorDescription(error, "状态可能已经变化，请刷新后重试。")}
      </AlertDescription>
    </Alert>
  )
}

function ReadError({
  error,
  onRetry,
}: {
  error: unknown
  onRetry: () => void | Promise<unknown>
}) {
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>任务暂时无法读取</AlertTitle>
      <AlertDescription>
        {requestErrorDescription(error, "请稍后刷新后重试。")}
      </AlertDescription>
      <Button variant="outline" size="sm" onClick={() => void onRetry()}>
        <RefreshCwIcon data-icon="inline-start" />
        重试
      </Button>
    </Alert>
  )
}

function toDateTimeLocal(value: Date) {
  const offset = value.getTimezoneOffset() * 60_000
  return new Date(value.getTime() - offset).toISOString().slice(0, 16)
}

function groupLabel(value: WorkgroupKind) {
  switch (value) {
    case "reseed":
      return "转种组"
    case "review":
      return "种审组"
    case "retention":
      return "保种组"
  }
}

function taskTypeLabel(value: WorkgroupTaskType) {
  return value === "activity" ? "限时活动" : "工作任务"
}

function timelineStateLabel(value: WorkgroupTask["timeline_state"]) {
  switch (value) {
    case "scheduled":
      return "等待开始"
    case "open":
      return "进行中"
    case "closed":
      return "已截止"
  }
}

function assignmentStateLabel(value: WorkgroupTaskAssignment["state"]) {
  switch (value) {
    case "pending_review":
      return "等待验收"
    case "changes_requested":
      return "需要修改"
    case "accepted":
      return "已通过"
    case "not_submitted":
      return "待提交"
  }
}
