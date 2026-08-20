import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  BadgeCheckIcon,
  CircleAlertIcon,
  Clock3Icon,
  FileCheck2Icon,
  RefreshCwIcon,
  ShieldAlertIcon,
  ShieldCheckIcon,
  UserRoundXIcon,
  UsersRoundIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
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
  AlertDialogTrigger,
} from "~/components/ui/alert-dialog"
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
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Field, FieldError, FieldLabel } from "~/components/ui/field"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  governanceOverviewQueryOptions,
  type GrantAdministrationGrant,
  type GrantRevocationRequest,
  type ReviewDomain,
  useCreateGrantRevocation,
  useReviewGrantRevocation,
} from "~/features/staff/api/governance.queries"
import type { StaffSession } from "~/features/staff/api/staff-session.mutations"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"
import { ApiProblemError } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

type CapabilityList = components["schemas"]["CapabilityList"]
type GovernanceSection = "grants" | "requests"

export function StaffGovernancePage() {
  return (
    <StaffAccessGate
      requiredAction="authz.grant.read"
      pageHeader={{
        title: "权限与任期",
        description: "管理后台角色任期与独立撤权复核",
        icon: UsersRoundIcon,
      }}
    >
      {({ session, capabilities }) => (
        <GovernanceContent session={session} capabilities={capabilities} />
      )}
    </StaffAccessGate>
  )
}

function GovernanceContent({
  session,
  capabilities,
}: {
  session: StaffSession
  capabilities: CapabilityList
}) {
  const overview = useQuery(governanceOverviewQueryOptions)
  const [section, setSection] = React.useState<GovernanceSection>("grants")

  if (overview.isPending) {
    return <GovernanceSkeleton />
  }
  if (overview.isError || !overview.data) {
    return (
      <GovernanceFrame>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>权限与任期暂时无法读取</AlertTitle>
          <AlertDescription>
            暂时无法取得权限与任期数据，请稍后重试。
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void overview.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      </GovernanceFrame>
    )
  }

  const canPropose = hasCapability(capabilities, "authz.grant.revoke.propose")
  const canReviewGovernance = hasCapability(
    capabilities,
    "authz.grant.revoke.approve.governance"
  )
  const canReviewSecurity = hasCapability(
    capabilities,
    "authz.grant.revoke.approve.security"
  )
  const pendingCount = overview.data.requests.filter(
    (request) => request.status === "pending"
  ).length
  const activeGrantCount = overview.data.grants.filter(
    (grant) => grant.mandate_status === "active" && !grant.revoked_at
  ).length

  return (
    <GovernanceFrame>
      <GovernanceHeader
        refreshing={overview.isFetching}
        onRefresh={() => void overview.refetch()}
      />

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <SummaryCard
          icon={BadgeCheckIcon}
          title="授权记录"
          value={overview.data.grants.length}
          description="当前可查看总数"
        />
        <SummaryCard
          icon={Clock3Icon}
          title="有效任期"
          value={activeGrantCount}
          description="当前仍可用"
        />
        <SummaryCard
          icon={FileCheck2Icon}
          title="待复核撤权"
          value={pendingCount}
          description="等待双人复核"
        />
        <SummaryCard
          icon={ShieldCheckIcon}
          title="权限策略"
          value="已加载"
          description="当前授权数据可用"
          compact
        />
      </div>

      <ToggleGroup
        value={[section]}
        onValueChange={(values) => {
          const selected = values[0] as GovernanceSection | undefined
          if (selected) setSection(selected)
        }}
        className="w-full justify-start border-b"
        aria-label="权限治理页面"
      >
        <ToggleGroupItem
          value="grants"
          className="h-10 rounded-b-none aria-pressed:border-b-2 aria-pressed:border-primary"
        >
          角色授权
        </ToggleGroupItem>
        <ToggleGroupItem
          value="requests"
          className="h-10 rounded-b-none aria-pressed:border-b-2 aria-pressed:border-primary"
        >
          撤权复核
          {pendingCount > 0 ? (
            <Badge variant="secondary">{pendingCount}</Badge>
          ) : null}
        </ToggleGroupItem>
      </ToggleGroup>

      {section === "grants" ? (
        <GrantTableCard
          grants={overview.data.grants}
          requests={overview.data.requests}
          session={session}
          canPropose={canPropose}
        />
      ) : (
        <RevocationListCard
          grants={overview.data.grants}
          requests={overview.data.requests}
          session={session}
          canReviewGovernance={canReviewGovernance}
          canReviewSecurity={canReviewSecurity}
        />
      )}
    </GovernanceFrame>
  )
}

function GovernanceHeader({
  refreshing,
  onRefresh,
}: {
  refreshing: boolean
  onRefresh: () => void
}) {
  return (
    <header className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex items-start gap-3">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <UsersRoundIcon className="size-6" />
        </span>
        <div>
          <h1 className="font-heading text-2xl font-bold">权限与任期</h1>
          <p className="text-sm text-muted-foreground">
            管理后台角色任期与独立撤权复核
          </p>
        </div>
      </div>
      <Button
        variant="outline"
        size="sm"
        className="min-w-[78px] sm:mt-2"
        disabled={refreshing}
        onClick={onRefresh}
      >
        <RefreshCwIcon
          data-icon="inline-start"
          className={refreshing ? "animate-spin" : undefined}
        />
        {refreshing ? "刷新中" : "刷新"}
      </Button>
    </header>
  )
}

function GrantTableCard({
  grants,
  requests,
  session,
  canPropose,
}: {
  grants: GrantAdministrationGrant[]
  requests: GrantRevocationRequest[]
  session: StaffSession
  canPropose: boolean
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6">
        <CardTitle className="leading-none">
          <h2 className="text-2xl leading-none font-semibold">
            角色授权与任期
          </h2>
        </CardTitle>
        <CardDescription>
          撤权申请固定目标授权版本，审批期间发生变化时必须重新发起。
        </CardDescription>
        <CardAction>
          <Badge variant="outline">{grants.length} 条</Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="px-6 pb-6">
        {grants.length === 0 ? (
          <Empty className="min-h-40">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <BadgeCheckIcon />
              </EmptyMedia>
              <EmptyTitle>没有可查看的授权</EmptyTitle>
              <EmptyDescription>当前管理范围内没有授权记录。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <Table>
              <TableCaption>
                每次变更都会在保存前重新检查权限、任期与授权版本。
              </TableCaption>
              <TableHeader>
                <TableRow>
                  <TableHead>账号</TableHead>
                  <TableHead>角色 / 任期</TableHead>
                  <TableHead>范围</TableHead>
                  <TableHead>有效期</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {grants.map((grant) => {
                  const pendingRequest = requests.find(
                    (request) =>
                      request.grant_id === grant.id &&
                      request.status === "pending"
                  )
                  const canProposeThisGrant =
                    canPropose &&
                    grant.subject_id !== session.user.id &&
                    grant.mandate_status === "active" &&
                    !grant.revoked_at &&
                    !pendingRequest
                  return (
                    <TableRow key={grant.id}>
                      <TableCell>
                        <div className="flex min-w-40 flex-col gap-0.5">
                          <span className="font-medium">
                            {grant.subject_display_name}
                          </span>
                          <span className="text-xs text-muted-foreground">
                            @{grant.subject_username}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex min-w-44 flex-col gap-1">
                          <span>{grant.role_name}</span>
                          <div className="flex flex-wrap items-center gap-1.5">
                            <code className="text-xs text-muted-foreground">
                              {grant.role_id}
                            </code>
                            <MandateBadge status={grant.mandate_status} />
                            <Badge variant="outline">
                              第 {grant.version} 版
                            </Badge>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant="secondary">
                          {grant.scope.type === "site" ? "全站" : "分类"} /{" "}
                          {grant.scope.id}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <div className="flex min-w-40 flex-col gap-0.5 text-xs">
                          <time dateTime={grant.valid_from}>
                            自 {formatDateTime(grant.valid_from)}
                          </time>
                          <time
                            className="text-muted-foreground"
                            dateTime={grant.valid_until}
                          >
                            至 {formatDateTime(grant.valid_until)}
                          </time>
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        {canProposeThisGrant ? (
                          <ProposeRevocationDialog
                            grant={grant}
                            csrfToken={session.csrf_token}
                          />
                        ) : pendingRequest ? (
                          <Badge variant="outline">撤权待复核</Badge>
                        ) : grant.subject_id === session.user.id ? (
                          <span className="text-xs text-muted-foreground">
                            不可对自己操作
                          </span>
                        ) : null}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function RevocationListCard({
  grants,
  requests,
  session,
  canReviewGovernance,
  canReviewSecurity,
}: {
  grants: GrantAdministrationGrant[]
  requests: GrantRevocationRequest[]
  session: StaffSession
  canReviewGovernance: boolean
  canReviewSecurity: boolean
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6">
        <CardTitle className="leading-none">
          <h2 className="text-2xl leading-none font-semibold">撤权复核</h2>
        </CardTitle>
        <CardDescription>
          提议者、治理复核者和安全复核者职责分离，双批准后才执行撤权。
        </CardDescription>
        <CardAction>
          <Badge variant="outline">{requests.length} 条</Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 px-6 pb-6">
        {requests.length === 0 ? (
          <Empty className="min-h-40">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <FileCheck2Icon />
              </EmptyMedia>
              <EmptyTitle>没有撤权申请</EmptyTitle>
              <EmptyDescription>
                当前授权保持原状，没有待处理的撤权申请。
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          requests.map((request) => {
            const grant = grants.find((item) => item.id === request.grant_id)
            return (
              <RevocationRequestCard
                key={request.id}
                request={request}
                grant={grant}
                session={session}
                canReviewGovernance={canReviewGovernance}
                canReviewSecurity={canReviewSecurity}
              />
            )
          })
        )}
      </CardContent>
    </Card>
  )
}

function ProposeRevocationDialog({
  grant,
  csrfToken,
}: {
  grant: GrantAdministrationGrant
  csrfToken: string
}) {
  const mutation = useCreateGrantRevocation()
  const [open, setOpen] = React.useState(false)
  const [reasonError, setReasonError] = React.useState("")

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const reason = String(new FormData(form).get("reason") ?? "").trim()
    if (reason.length < 10 || reason.length > 1000) {
      setReasonError("请填写 10–1000 字的完整撤权理由")
      return
    }
    setReasonError("")
    try {
      await mutation.mutateAsync({
        csrfToken,
        grantId: grant.id,
        expectedGrantVersion: grant.version,
        reason,
      })
      form.reset()
      setOpen(false)
    } catch {
      // Mutation state renders the reviewed problem without closing the dialog.
    }
  }

  return (
    <AlertDialog
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        if (nextOpen) {
          mutation.reset()
          setReasonError("")
        }
      }}
    >
      <AlertDialogTrigger
        render={
          <Button variant="outline" size="sm">
            <UserRoundXIcon data-icon="inline-start" />
            提议撤权
          </Button>
        }
      />
      <AlertDialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit} className="contents">
          <AlertDialogHeader>
            <AlertDialogMedia>
              <ShieldAlertIcon />
            </AlertDialogMedia>
            <AlertDialogTitle>确认创建撤权申请</AlertDialogTitle>
            <AlertDialogDescription>
              这不会立即撤销权限；申请会记录当前第 {grant.version} 版
              版本，并等待治理与安全人员分别复核。
            </AlertDialogDescription>
          </AlertDialogHeader>

          <div className="grid gap-3 rounded-md border bg-muted/30 p-3 text-sm">
            <div className="grid grid-cols-[5rem_1fr] gap-2">
              <span className="text-muted-foreground">目标账号</span>
              <span>@{grant.subject_username}</span>
              <span className="text-muted-foreground">角色</span>
              <span>{grant.role_name}</span>
              <span className="text-muted-foreground">状态变化</span>
              <span>有效 → 双复核后撤销</span>
            </div>
          </div>

          {mutation.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>{mutationErrorTitle(mutation.error)}</AlertTitle>
              <AlertDescription>
                当前授权可能已经变化或存在待处理申请，请刷新后重试。
              </AlertDescription>
            </Alert>
          ) : null}

          <Field data-invalid={Boolean(reasonError)}>
            <FieldLabel htmlFor={`revoke-reason-${grant.id}`}>
              撤权理由
            </FieldLabel>
            <Textarea
              id={`revoke-reason-${grant.id}`}
              name="reason"
              minLength={10}
              maxLength={1000}
              rows={4}
              placeholder="说明为何需要撤销、依据和预期影响…"
              aria-invalid={Boolean(reasonError)}
              disabled={mutation.isPending}
            />
            <FieldError
              errors={reasonError ? [{ message: reasonError }] : []}
            />
          </Field>

          <AlertDialogFooter>
            <AlertDialogCancel disabled={mutation.isPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              type="submit"
              variant="destructive"
              disabled={mutation.isPending}
            >
              {mutation.isPending ? <Spinner /> : <UserRoundXIcon />}
              {mutation.isPending ? "正在提交…" : "创建撤权申请"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </form>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function RevocationRequestCard({
  request,
  grant,
  session,
  canReviewGovernance,
  canReviewSecurity,
}: {
  request: GrantRevocationRequest
  grant: GrantAdministrationGrant | undefined
  session: StaffSession
  canReviewGovernance: boolean
  canReviewSecurity: boolean
}) {
  const actorParticipated = request.reviews.some(
    (review) => review.reviewer_id === session.user.id
  )
  const canReview =
    request.status === "pending" &&
    request.proposer_id !== session.user.id &&
    !actorParticipated

  return (
    <div className="flex flex-col gap-4 rounded-lg border p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">
              {grant
                ? `${grant.subject_display_name} · ${grant.role_name}`
                : `授权 ${request.grant_id.slice(0, 8)}`}
            </span>
            <RequestStatusBadge status={request.status} />
            <Badge variant="outline">
              第 {request.expected_grant_version} 版
            </Badge>
          </div>
          <p className="max-w-3xl text-sm text-muted-foreground">
            {request.reason}
          </p>
          <p className="text-xs text-muted-foreground">
            创建于 {formatDateTime(request.created_at)} · 截止{" "}
            {formatDateTime(request.expires_at)}
          </p>
        </div>

        {canReview ? (
          <div className="flex flex-wrap gap-2">
            {canReviewGovernance &&
            !request.reviews.some(
              (review) => review.domain === "governance"
            ) ? (
              <ReviewButtons
                request={request}
                domain="governance"
                csrfToken={session.csrf_token}
              />
            ) : null}
            {canReviewSecurity &&
            !request.reviews.some((review) => review.domain === "security") ? (
              <ReviewButtons
                request={request}
                domain="security"
                csrfToken={session.csrf_token}
              />
            ) : null}
          </div>
        ) : null}
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        <ReviewState request={request} domain="governance" />
        <ReviewState request={request} domain="security" />
      </div>
    </div>
  )
}

function ReviewButtons({
  request,
  domain,
  csrfToken,
}: {
  request: GrantRevocationRequest
  domain: ReviewDomain
  csrfToken: string
}) {
  return (
    <div className="flex items-center gap-1.5">
      <ReviewDialog
        request={request}
        domain={domain}
        decision="approve"
        csrfToken={csrfToken}
      />
      <ReviewDialog
        request={request}
        domain={domain}
        decision="reject"
        csrfToken={csrfToken}
      />
    </div>
  )
}

function ReviewDialog({
  request,
  domain,
  decision,
  csrfToken,
}: {
  request: GrantRevocationRequest
  domain: ReviewDomain
  decision: "approve" | "reject"
  csrfToken: string
}) {
  const mutation = useReviewGrantRevocation()
  const [open, setOpen] = React.useState(false)
  const [reasonError, setReasonError] = React.useState("")
  const approving = decision === "approve"

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const reason = String(new FormData(form).get("reason") ?? "").trim()
    if (reason.length < 10 || reason.length > 1000) {
      setReasonError("请填写 10–1000 字的独立复核意见")
      return
    }
    setReasonError("")
    try {
      await mutation.mutateAsync({
        csrfToken,
        requestId: request.id,
        domain,
        decision,
        reason,
      })
      form.reset()
      setOpen(false)
    } catch {
      // Keep the dialog open so the reviewer can inspect the server rejection.
    }
  }

  return (
    <AlertDialog
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        if (nextOpen) {
          mutation.reset()
          setReasonError("")
        }
      }}
    >
      <AlertDialogTrigger
        render={
          <Button variant={approving ? "outline" : "destructive"} size="sm">
            {reviewDomainLabel(domain)}
            {approving ? "批准" : "拒绝"}
          </Button>
        }
      />
      <AlertDialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit} className="contents">
          <AlertDialogHeader>
            <AlertDialogMedia>
              {approving ? <FileCheck2Icon /> : <ShieldAlertIcon />}
            </AlertDialogMedia>
            <AlertDialogTitle>
              {reviewDomainLabel(domain)}复核{approving ? "批准" : "拒绝"}撤权
            </AlertDialogTitle>
            <AlertDialogDescription>
              这是独立复核决定。批准后不会立即执行；另一类复核也必须由不同人员完成。
            </AlertDialogDescription>
          </AlertDialogHeader>

          <div className="rounded-md border bg-muted/30 p-3 text-sm">
            <p className="font-medium">原始提议理由</p>
            <p className="mt-1 text-muted-foreground">{request.reason}</p>
          </div>

          {mutation.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>{mutationErrorTitle(mutation.error)}</AlertTitle>
              <AlertDescription>
                申请状态、复核人员或目标授权版本可能已经变化。
              </AlertDescription>
            </Alert>
          ) : null}

          <Field data-invalid={Boolean(reasonError)}>
            <FieldLabel htmlFor={`${domain}-${decision}-${request.id}`}>
              独立复核意见
            </FieldLabel>
            <Textarea
              id={`${domain}-${decision}-${request.id}`}
              name="reason"
              minLength={10}
              maxLength={1000}
              rows={4}
              placeholder="记录核验依据、风险判断与决定理由…"
              aria-invalid={Boolean(reasonError)}
              disabled={mutation.isPending}
            />
            <FieldError
              errors={reasonError ? [{ message: reasonError }] : []}
            />
          </Field>

          <AlertDialogFooter>
            <AlertDialogCancel disabled={mutation.isPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              type="submit"
              variant={approving ? "default" : "destructive"}
              disabled={mutation.isPending}
            >
              {mutation.isPending ? <Spinner /> : null}
              {mutation.isPending
                ? "正在记录…"
                : `确认${approving ? "批准" : "拒绝"}`}
            </AlertDialogAction>
          </AlertDialogFooter>
        </form>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function ReviewState({
  request,
  domain,
}: {
  request: GrantRevocationRequest
  domain: ReviewDomain
}) {
  const review = request.reviews.find((item) => item.domain === domain)
  return (
    <div className="flex items-start justify-between gap-3 rounded-md bg-muted/45 px-3 py-2 text-sm">
      <div className="flex flex-col gap-0.5">
        <span className="font-medium">{reviewDomainLabel(domain)}复核</span>
        <span className="text-xs text-muted-foreground">
          {review ? review.reason : "尚未作出决定"}
        </span>
      </div>
      <Badge
        variant={review?.decision === "reject" ? "destructive" : "outline"}
      >
        {review
          ? review.decision === "approve"
            ? "已批准"
            : "已拒绝"
          : "待处理"}
      </Badge>
    </div>
  )
}

function SummaryCard({
  icon: Icon,
  title,
  value,
  description,
  compact = false,
}: {
  icon: typeof BadgeCheckIcon
  title: string
  value: number | string
  description: string
  compact?: boolean
}) {
  return (
    <Card className="h-[102px] gap-0 py-0">
      <CardHeader className="grid h-full grid-cols-[auto_1fr] items-center gap-3 p-6">
        <span className="flex size-9 items-center justify-center rounded-lg bg-muted text-muted-foreground">
          <Icon className="size-5" />
        </span>
        <div className="min-w-0">
          <CardDescription>{title}</CardDescription>
          <CardTitle
            className={compact ? "truncate text-lg" : "text-2xl font-bold"}
          >
            {value}
          </CardTitle>
          <span className="sr-only">{description}</span>
        </div>
      </CardHeader>
    </Card>
  )
}

function MandateBadge({
  status,
}: {
  status: GrantAdministrationGrant["mandate_status"]
}) {
  const active = status === "active"
  return (
    <Badge variant={active ? "secondary" : "destructive"}>
      {active
        ? "任期有效"
        : status === "suspended"
          ? "任期暂停"
          : status === "expired"
            ? "任期过期"
            : "任期撤销"}
    </Badge>
  )
}

function RequestStatusBadge({
  status,
}: {
  status: GrantRevocationRequest["status"]
}) {
  const label = {
    pending: "待复核",
    rejected: "已拒绝",
    applied: "已执行",
    conflicted: "版本冲突",
    expired: "已过期",
  }[status]
  return (
    <Badge
      variant={
        status === "pending"
          ? "secondary"
          : status === "applied"
            ? "outline"
            : "destructive"
      }
    >
      {label}
    </Badge>
  )
}

function reviewDomainLabel(domain: ReviewDomain) {
  return domain === "governance" ? "治理" : "安全"
}

function mutationErrorTitle(error: Error) {
  return error instanceof ApiProblemError ? error.message : "治理操作失败"
}

function GovernanceFrame({ children }: { children: React.ReactNode }) {
  return <StaffPageFrame className="gap-6">{children}</StaffPageFrame>
}

function GovernanceSkeleton() {
  return (
    <GovernanceFrame>
      <div
        className="flex flex-col gap-2"
        aria-label="正在加载权限与任期"
        aria-busy="true"
      >
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-4 w-96 max-w-full" />
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Skeleton className="h-[102px] w-full" />
        <Skeleton className="h-[102px] w-full" />
        <Skeleton className="h-[102px] w-full" />
        <Skeleton className="h-[102px] w-full" />
      </div>
      <Skeleton className="h-96 w-full" />
    </GovernanceFrame>
  )
}
