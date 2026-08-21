import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CheckIcon,
  CircleAlertIcon,
  ClipboardIcon,
  Clock3Icon,
  CoinsIcon,
  GitForkIcon,
  KeyRoundIcon,
  LogInIcon,
  RefreshCwIcon,
  ShieldXIcon,
  TicketIcon,
  Trash2Icon,
  UserRoundCheckIcon,
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
} from "~/components/ui/alert-dialog"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
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
import { Input } from "~/components/ui/input"
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
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type InvitationIssueResult,
  type MemberInvitation,
  invitationOverviewQueryOptions,
  useIssueInvitation,
  useRevokeInvitation,
} from "~/features/invitation/api/invitations.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

const pageSize = 20

export function InvitationsPage() {
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const canRead = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "invitation.read.self"
    )
  )
  const [offset, setOffset] = React.useState(0)
  const overview = useQuery({
    ...invitationOverviewQueryOptions(session.data?.user.id, pageSize, offset),
    enabled: Boolean(session.data && capabilities.data && canRead),
  })
  const issue = useIssueInvitation()
  const revoke = useRevokeInvitation()
  const [issued, setIssued] = React.useState<InvitationIssueResult>()
  const [revokeTarget, setRevokeTarget] = React.useState<MemberInvitation>()
  const [copied, setCopied] = React.useState<"token" | "link">()
  const [copyError, setCopyError] = React.useState("")

  async function handleIssue() {
    if (!session.data) return
    const result = await issue.mutateAsync(session.data.csrf_token)
    setCopied(undefined)
    setCopyError("")
    setIssued(result)
  }

  async function handleRevoke() {
    if (!session.data || !revokeTarget) return
    await revoke.mutateAsync({
      csrfToken: session.data.csrf_token,
      invitationId: revokeTarget.id,
    })
    setRevokeTarget(undefined)
  }

  async function copyValue(kind: "token" | "link") {
    if (!issued) return
    const value =
      kind === "token"
        ? issued.token
        : `${window.location.origin}/register?invite=${encodeURIComponent(issued.token)}`
    try {
      await navigator.clipboard.writeText(value)
      setCopyError("")
      setCopied(kind)
    } catch {
      setCopyError("浏览器没有允许自动复制，请手动选中上方内容复制。")
    }
  }

  return (
    <PageLayout>
      <PageHeader
        title="邀请"
        description="签发一次性邀请码，并查看自己已经使用的邀请名额。"
      />

      {(session.isPending ||
        (session.data && capabilities.isPending) ||
        (session.data &&
          capabilities.data &&
          canRead &&
          overview.isPending)) && <InvitationSkeleton />}

      {session.isError && (
        <ErrorAlert
          title="会话状态暂时无法读取"
          description={requestErrorDescription(
            session.error,
            "会话请求未能完成，请稍后刷新页面。"
          )}
          onRetry={() => void session.refetch()}
        />
      )}

      {!session.isPending && !session.isError && !session.data && (
        <Card>
          <CardContent>
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <LogInIcon />
                </EmptyMedia>
                <EmptyTitle>登录后管理邀请</EmptyTitle>
                <EmptyDescription>
                  邀请名额和邀请记录只对本人可见。
                </EmptyDescription>
              </EmptyHeader>
              <Link to="/login" className={buttonVariants()}>
                前往登录
              </Link>
            </Empty>
          </CardContent>
        </Card>
      )}

      {session.data && capabilities.isError && (
        <ErrorAlert
          title="暂时无法读取邀请权限"
          description={requestErrorDescription(
            capabilities.error,
            "权限请求未能完成，请稍后再试。"
          )}
          onRetry={() => void capabilities.refetch()}
        />
      )}

      {session.data && capabilities.data && !canRead && (
        <Alert>
          <ShieldXIcon />
          <AlertTitle>当前账户不能使用邀请功能</AlertTitle>
          <AlertDescription>如有疑问，请联系站点管理人员。</AlertDescription>
        </Alert>
      )}

      {session.data && capabilities.data && canRead && overview.isError && (
        <ErrorAlert
          title="邀请记录暂时无法读取"
          description={requestErrorDescription(
            overview.error,
            "邀请请求未能完成，请稍后重试。"
          )}
          onRetry={() => void overview.refetch()}
        />
      )}

      {session.data && overview.data && (
        <>
          <InvitationSummary
            eligibility={overview.data.eligibility}
            issuePending={issue.isPending}
            issueError={issue.error}
            onIssue={() => void handleIssue()}
          />
          <InvitationNetwork network={overview.data.network} />
          <InvitationHistory
            items={overview.data.items}
            total={overview.data.total}
            offset={offset}
            revokePending={revoke.isPending}
            onRevoke={setRevokeTarget}
            onPrevious={() =>
              setOffset((value) => Math.max(0, value - pageSize))
            }
            onNext={() => setOffset((value) => value + pageSize)}
          />
        </>
      )}

      <Dialog
        open={Boolean(issued)}
        onOpenChange={(open) => {
          if (!open) {
            setIssued(undefined)
            setCopyError("")
            issue.reset()
          }
        }}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>邀请码已生成</DialogTitle>
            <DialogDescription>
              明文只显示这一次。关闭窗口后只能撤销并重新生成，不能找回。
            </DialogDescription>
          </DialogHeader>
          {issued ? (
            <div className="grid gap-4">
              <CopyField
                label="邀请凭证"
                value={issued.token}
                copied={copied === "token"}
                onCopy={() => void copyValue("token")}
              />
              <CopyField
                label="邀请注册链接"
                value={`${typeof window === "undefined" ? "" : window.location.origin}/register?invite=${encodeURIComponent(issued.token)}`}
                copied={copied === "link"}
                onCopy={() => void copyValue("link")}
              />
              <p className="text-xs text-muted-foreground">
                有效至 {formatDateTime(issued.invitation.expires_at)}
                ，仅可完成一次注册。
              </p>
              {copyError ? (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>复制失败</AlertTitle>
                  <AlertDescription>{copyError}</AlertDescription>
                </Alert>
              ) : null}
            </div>
          ) : null}
          <DialogFooter showCloseButton />
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={Boolean(revokeTarget)}
        onOpenChange={(open) => {
          if (!open && !revoke.isPending) setRevokeTarget(undefined)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <Trash2Icon />
            </AlertDialogMedia>
            <AlertDialogTitle>撤销这个邀请码？</AlertDialogTitle>
            <AlertDialogDescription>
              撤销后邀请码立即失效并释放一个名额，此操作不能恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={revoke.isPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={revoke.isPending}
              onClick={() => void handleRevoke()}
            >
              {revoke.isPending ? <Spinner data-icon="inline-start" /> : null}
              确认撤销
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </PageLayout>
  )
}

function InvitationSummary({
  eligibility,
  issuePending,
  issueError,
  onIssue,
}: {
  eligibility: import("~/features/invitation/api/invitations.queries").InvitationOverview["eligibility"]
  issuePending: boolean
  issueError: Error | null
  onIssue: () => void
}) {
  return (
    <div className="grid gap-4">
      <div className="grid gap-3 sm:grid-cols-3">
        <SummaryCard
          icon={TicketIcon}
          label="可用名额"
          value={`${eligibility.remaining_invites} / ${eligibility.max_invites_per_member}`}
        />
        <SummaryCard
          icon={Clock3Icon}
          label="邀请码有效期"
          value={`${eligibility.invite_valid_days} 天`}
        />
        <SummaryCard
          icon={UserRoundCheckIcon}
          label="当前资格"
          value={eligibility.eligible ? "可以签发" : "暂不可签发"}
        />
      </div>

      <Card>
        <CardHeader>
          <div>
            <CardTitle>生成邀请码</CardTitle>
            <CardDescription>
              需要邮箱已验证、账户正常、注册满{" "}
              {eligibility.minimum_account_age_days} 天并达到 Lv.
              {eligibility.minimum_level}。
            </CardDescription>
          </div>
          <CardAction>
            <Button
              onClick={onIssue}
              disabled={!eligibility.eligible || issuePending}
            >
              {issuePending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <KeyRoundIcon data-icon="inline-start" />
              )}
              {issuePending ? "生成中…" : "生成邀请码"}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="grid gap-3">
          {!eligibility.eligible ? (
            <Alert>
              <CircleAlertIcon />
              <AlertTitle>{blockerTitle(eligibility.blocker)}</AlertTitle>
              <AlertDescription>
                {blockerDescription(eligibility.blocker, eligibility)}
              </AlertDescription>
            </Alert>
          ) : null}
          {issueError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>邀请码生成失败</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(issueError, "请刷新页面后重试。")}
              </AlertDescription>
            </Alert>
          ) : null}
        </CardContent>
      </Card>
    </div>
  )
}

function InvitationNetwork({
  network,
}: {
  network: import("~/features/invitation/api/invitations.queries").InvitationOverview["network"]
}) {
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>后宫与邀请关系</CardTitle>
          <CardDescription>
            展示直属成员、邀请链规模和从 Rousi 继承的历史奖励。
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <NetworkMetric
            icon={UsersRoundIcon}
            label="直属成员"
            value={formatInteger(network.direct_count)}
          />
          <NetworkMetric
            icon={GitForkIcon}
            label="后宫总人数"
            value={formatInteger(network.total_descendants)}
          />
          <NetworkMetric
            icon={CoinsIcon}
            label="历史后宫奖励"
            value={formatInteger(network.harem_reward.amount) + " 魔力值"}
          />
          <NetworkMetric
            icon={TicketIcon}
            label="历史邀请奖励"
            value={formatInteger(network.invitation_reward.amount) + " 魔力值"}
          />
        </div>

        {(network.harem_reward.source_rows > 0 ||
          network.invitation_reward.source_rows > 0) && (
          <Alert>
            <CoinsIcon />
            <AlertTitle>旧站奖励已计入期初魔力值</AlertTitle>
            <AlertDescription>
              这里保留的是 Rousi 历史记录，不会再次入账。后宫奖励共{" "}
              {formatInteger(network.harem_reward.source_rows)} 笔
              {network.harem_reward.last_rewarded_at
                ? "，最后结算于 " +
                  formatDateTime(network.harem_reward.last_rewarded_at)
                : ""}
              。
            </AlertDescription>
          </Alert>
        )}

        {network.direct_members.length ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>直属成员</TableHead>
                <TableHead>用户 ID</TableHead>
                <TableHead>来源</TableHead>
                <TableHead className="text-right">建立时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {network.direct_members.map((member) => (
                <TableRow key={member.numeric_id}>
                  <TableCell>
                    <div className="flex flex-col gap-0.5">
                      <span>{member.display_name}</span>
                      <span className="text-xs text-muted-foreground">
                        @{member.username}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {formatInteger(member.numeric_id)}
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {member.source === "legacy_import"
                        ? "Rousi 继承"
                        : "PeerGo 邀请"}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right text-muted-foreground">
                    {formatDateTime(member.established_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <Empty className="min-h-36">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <UsersRoundIcon />
              </EmptyMedia>
              <EmptyTitle>还没有直属成员</EmptyTitle>
              <EmptyDescription>
                通过你的邀请码完成注册后，邀请关系会显示在这里。
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
      </CardContent>
    </Card>
  )
}

function NetworkMetric({
  icon: Icon,
  label,
  value,
}: {
  icon: typeof UsersRoundIcon
  label: string
  value: string
}) {
  return (
    <Card size="sm">
      <CardHeader>
        <CardDescription className="flex items-center gap-2">
          <Icon />
          {label}
        </CardDescription>
        <CardTitle className="text-lg tabular-nums">{value}</CardTitle>
      </CardHeader>
    </Card>
  )
}

function InvitationHistory({
  items,
  total,
  offset,
  revokePending,
  onRevoke,
  onPrevious,
  onNext,
}: {
  items: MemberInvitation[]
  total: number
  offset: number
  revokePending: boolean
  onRevoke: (item: MemberInvitation) => void
  onPrevious: () => void
  onNext: () => void
}) {
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>邀请记录</CardTitle>
          <CardDescription>
            共 {total.toLocaleString("zh-CN")} 条记录
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent className="px-0">
        {items.length ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">状态</TableHead>
                <TableHead>受邀用户</TableHead>
                <TableHead>生成时间</TableHead>
                <TableHead>有效期至</TableHead>
                <TableHead className="pr-6 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="pl-6">
                    <InvitationStatusBadge status={item.status} />
                  </TableCell>
                  <TableCell>{item.invitee_username ?? "—"}</TableCell>
                  <TableCell className="text-muted-foreground">
                    {formatDateTime(item.created_at)}
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {formatDateTime(item.expires_at)}
                  </TableCell>
                  <TableCell className="pr-6 text-right">
                    {item.status === "available" ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={revokePending}
                        onClick={() => onRevoke(item)}
                      >
                        撤销
                      </Button>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <Empty className="min-h-44 rounded-none border-0">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <TicketIcon />
              </EmptyMedia>
              <EmptyTitle>还没有邀请记录</EmptyTitle>
              <EmptyDescription>
                成功生成邀请码后，状态和使用结果会显示在这里。
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        {total > pageSize ? (
          <div className="flex items-center justify-end gap-2 border-t px-6 pt-4">
            <Button
              size="sm"
              variant="outline"
              onClick={onPrevious}
              disabled={offset === 0}
            >
              上一页
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={onNext}
              disabled={offset + pageSize >= total}
            >
              下一页
            </Button>
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

function SummaryCard({
  icon: Icon,
  label,
  value,
}: {
  icon: typeof TicketIcon
  label: string
  value: string
}) {
  return (
    <Card className="py-4">
      <CardContent className="flex items-center gap-3">
        <div className="flex size-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Icon className="size-4" />
        </div>
        <div>
          <p className="text-xs text-muted-foreground">{label}</p>
          <p className="text-lg font-semibold tabular-nums">{value}</p>
        </div>
      </CardContent>
    </Card>
  )
}

function CopyField({
  label,
  value,
  copied,
  onCopy,
}: {
  label: string
  value: string
  copied: boolean
  onCopy: () => void
}) {
  return (
    <div className="grid gap-1.5">
      <label className="text-sm font-medium">{label}</label>
      <div className="flex gap-2">
        <Input value={value} readOnly className="font-mono text-xs" />
        <Button variant="outline" size="icon" onClick={onCopy}>
          {copied ? <CheckIcon /> : <ClipboardIcon />}
          <span className="sr-only">复制{label}</span>
        </Button>
      </div>
    </div>
  )
}

function InvitationStatusBadge({ status }: { status: string }) {
  const labels: Record<string, string> = {
    available: "可使用",
    claimed: "注册处理中",
    used: "已使用",
    expired: "已过期",
    revoked: "已撤销",
  }
  return (
    <Badge
      variant={
        status === "available"
          ? "default"
          : status === "used"
            ? "secondary"
            : "outline"
      }
    >
      {labels[status] ?? status}
    </Badge>
  )
}

function blockerTitle(blocker: string) {
  const titles: Record<string, string> = {
    disabled: "成员邀请暂未开放",
    account_unavailable: "账户状态不满足要求",
    email_unverified: "请先验证邮箱",
    account_age: "注册时间尚未达标",
    level: "用户等级尚未达标",
    quota_exhausted: "邀请名额已用完",
  }
  return titles[blocker] ?? "暂时不能签发邀请码"
}

function blockerDescription(
  blocker: string,
  eligibility: import("~/features/invitation/api/invitations.queries").InvitationOverview["eligibility"]
) {
  if (blocker === "account_age") {
    return `当前已注册 ${eligibility.current_account_age_days} 天，需要满 ${eligibility.minimum_account_age_days} 天。`
  }
  if (blocker === "level") {
    return `当前 Lv.${eligibility.current_level}，需要达到 Lv.${eligibility.minimum_level}。`
  }
  if (blocker === "quota_exhausted") {
    return "已成功邀请和当前有效邀请码已经占满名额；过期或撤销未使用的邀请码会释放名额。"
  }
  if (blocker === "email_unverified") {
    return "完成登录邮箱验证后即可重新检查邀请资格。"
  }
  if (blocker === "disabled") {
    return "管理员当前没有开放成员生成邀请码。"
  }
  return "请确认账户没有被封禁或限制后再试。"
}

function ErrorAlert({
  title,
  description,
  onRetry,
}: {
  title: string
  description: string
  onRetry: () => void
}) {
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{description}</AlertDescription>
      <AlertAction>
        <Button variant="outline" size="sm" onClick={onRetry}>
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </AlertAction>
    </Alert>
  )
}

function InvitationSkeleton() {
  return (
    <div className="grid gap-4" aria-busy="true">
      <div className="grid gap-3 sm:grid-cols-3">
        {Array.from({ length: 3 }).map((_, index) => (
          <Skeleton key={index} className="h-20" />
        ))}
      </div>
      <Skeleton className="h-40" />
      <Skeleton className="h-56" />
    </div>
  )
}
