import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CheckIcon,
  ChevronRightIcon,
  CircleAlertIcon,
  ClipboardIcon,
  CoinsIcon,
  GitForkIcon,
  GiftIcon,
  KeyRoundIcon,
  LockIcon,
  LogInIcon,
  RefreshCwIcon,
  ShieldXIcon,
  TicketIcon,
  Trash2Icon,
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
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
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
import { cn } from "~/lib/utils"

const pageSize = 20

export function InvitationsPage() {
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const canRead = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "invitation.read.self"
    )
  )
  const canRevoke = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "invitation.revoke.self"
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
  const [inviteeEmail, setInviteeEmail] = React.useState("")
  const [issuedEmail, setIssuedEmail] = React.useState("")
  const [revokeTarget, setRevokeTarget] = React.useState<MemberInvitation>()
  const [copied, setCopied] = React.useState<"token" | "link">()
  const [copyError, setCopyError] = React.useState("")
  const [activeTab, setActiveTab] = React.useState<
    "invites" | "tree" | "harem" | "chain"
  >("invites")

  async function handleIssue(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!session.data) return
    const normalizedEmail = inviteeEmail.trim().toLowerCase()
    const result = await issue.mutateAsync({
      csrfToken: session.data.csrf_token,
      email: normalizedEmail,
    })
    setCopied(undefined)
    setCopyError("")
    setIssuedEmail(normalizedEmail)
    setInviteeEmail("")
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
        description="管理可用邀请、邀请码历史、邀请树和上家邀请链。"
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
          <InvitationStats overview={overview.data} />
          <InvitationTabs active={activeTab} onChange={setActiveTab} />
          {activeTab === "invites" ? (
            <>
              <InvitationSummary
                eligibility={overview.data.eligibility}
                issuePending={issue.isPending}
                issueError={issue.error}
                inviteeEmail={inviteeEmail}
                onInviteeEmailChange={setInviteeEmail}
                onIssue={(event) => void handleIssue(event)}
              />
              <InvitationHistory
                items={overview.data.items}
                total={overview.data.total}
                offset={offset}
                canRevoke={canRevoke}
                revokePending={revoke.isPending}
                onRevoke={setRevokeTarget}
                onPrevious={() =>
                  setOffset((value) => Math.max(0, value - pageSize))
                }
                onNext={() => setOffset((value) => value + pageSize)}
              />
            </>
          ) : null}
          {activeTab === "tree" ? (
            <InvitationTree network={overview.data.network} />
          ) : null}
          {activeTab === "harem" ? (
            <InvitationHarem network={overview.data.network} />
          ) : null}
          {activeTab === "chain" ? (
            <InvitationChain
              currentUsername={session.data.user.username}
              network={overview.data.network}
            />
          ) : null}
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
              <Alert>
                <LockIcon />
                <AlertTitle>已绑定注册邮箱</AlertTitle>
                <AlertDescription>
                  仅{" "}
                  <span className="font-medium text-foreground">
                    {issuedEmail}
                  </span>{" "}
                  可以使用这个邀请码完成注册，其他邮箱会被拒绝。
                </AlertDescription>
              </Alert>
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
              撤销后邀请码立即失效，并返还一个可用邀请名额。此操作不能恢复。
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

type InvitationOverview =
  import("~/features/invitation/api/invitations.queries").InvitationOverview

function InvitationStats({ overview }: { overview: InvitationOverview }) {
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
      <SummaryCard
        icon={TicketIcon}
        label="剩余邀请"
        value={formatInteger(overview.eligibility.remaining_invites)}
      />
      <SummaryCard
        icon={UsersRoundIcon}
        label="总邀请人数"
        value={formatInteger(overview.network.direct_count)}
      />
      <SummaryCard
        icon={GitForkIcon}
        label="后宫总人数"
        value={formatInteger(overview.network.total_descendants)}
      />
      <SummaryCard
        icon={CoinsIcon}
        label="预计后宫奖励 / 小时"
        value={
          formatMilliMagic(
            overview.network.live_harem_reward.current_hourly_estimate_milli
          ) + " 魔力值"
        }
        tone="text-purple-600"
      />
      <SummaryCard
        icon={GiftIcon}
        label="PeerGo 已结算"
        value={
          formatInteger(overview.network.live_harem_reward.awarded_amount) +
          " 魔力值"
        }
        tone="text-emerald-600"
      />
    </div>
  )
}

function InvitationTabs({
  active,
  onChange,
}: {
  active: "invites" | "tree" | "harem" | "chain"
  onChange: (tab: "invites" | "tree" | "harem" | "chain") => void
}) {
  const tabs = [
    { id: "invites" as const, label: "邀请码", icon: KeyRoundIcon },
    { id: "tree" as const, label: "邀请树", icon: UsersRoundIcon },
    { id: "harem" as const, label: "后宫", icon: GiftIcon },
    { id: "chain" as const, label: "邀请链", icon: ChevronRightIcon },
  ]
  return (
    <div className="flex gap-1 overflow-x-auto border-b" role="tablist">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          type="button"
          role="tab"
          aria-selected={active === tab.id}
          onClick={() => onChange(tab.id)}
          className={cn(
            "-mb-px flex min-h-11 items-center gap-2 border-b-2 px-4 text-sm whitespace-nowrap transition-colors",
            active === tab.id
              ? "border-primary text-primary"
              : "border-transparent text-muted-foreground hover:text-foreground"
          )}
        >
          <tab.icon className="size-4" />
          {tab.label}
        </button>
      ))}
    </div>
  )
}

function InvitationSummary({
  eligibility,
  issuePending,
  issueError,
  inviteeEmail,
  onInviteeEmailChange,
  onIssue,
}: {
  eligibility: import("~/features/invitation/api/invitations.queries").InvitationOverview["eligibility"]
  issuePending: boolean
  issueError: Error | null
  inviteeEmail: string
  onInviteeEmailChange: (value: string) => void
  onIssue: (event: React.FormEvent<HTMLFormElement>) => void
}) {
  return (
    <div className="grid gap-4">
      <Card>
        <CardHeader>
          <div>
            <CardTitle>生成邀请码</CardTitle>
            <CardDescription>
              当前剩余 {formatInteger(eligibility.remaining_invites)} 个名额，
              邀请码有效期 {eligibility.invite_valid_days} 天。需要邮箱已验证、
              账户正常、注册满 {eligibility.minimum_account_age_days} 天并达到
              Lv.
              {eligibility.minimum_level}。
            </CardDescription>
          </div>
        </CardHeader>
        <CardContent className="grid gap-3">
          <form onSubmit={onIssue}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="invitee-email">被邀请人邮箱</FieldLabel>
                <div className="flex flex-col gap-2 sm:flex-row">
                  <Input
                    id="invitee-email"
                    name="inviteeEmail"
                    type="email"
                    autoComplete="off"
                    value={inviteeEmail}
                    onChange={(event) =>
                      onInviteeEmailChange(event.currentTarget.value)
                    }
                    placeholder="name@example.com"
                    maxLength={254}
                    required
                    disabled={!eligibility.eligible || issuePending}
                  />
                  <Button
                    type="submit"
                    className="sm:shrink-0"
                    disabled={
                      !eligibility.eligible ||
                      issuePending ||
                      !inviteeEmail.trim()
                    }
                  >
                    {issuePending ? (
                      <Spinner data-icon="inline-start" />
                    ) : (
                      <KeyRoundIcon data-icon="inline-start" />
                    )}
                    {issuePending ? "生成中…" : "绑定邮箱并生成"}
                  </Button>
                </div>
                <FieldDescription>
                  邮箱将在注册时强制匹配；Core
                  只保存不可还原的绑定摘要，不保存邮箱明文。
                </FieldDescription>
              </Field>
            </FieldGroup>
          </form>
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

function InvitationHarem({
  network,
}: {
  network: import("~/features/invitation/api/invitations.queries").InvitationOverview["network"]
}) {
  const live = network.live_harem_reward
  const policy = live.policy
  const rewardPercent = new Intl.NumberFormat("zh-CN", {
    maximumFractionDigits: 2,
  }).format(policy.reward_bps / 100)

  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>后宫奖励</CardTitle>
          <CardDescription>
            直属成员的合格做种奖励会按旧站规则持续为你产生魔力值。
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
            label="当前预计 / 小时"
            value={
              formatMilliMagic(live.current_hourly_estimate_milli) + " 魔力值"
            }
          />
          <NetworkMetric
            icon={CoinsIcon}
            label="PeerGo 已结算"
            value={formatInteger(live.awarded_amount) + " 魔力值"}
          />
          <NetworkMetric
            icon={TicketIcon}
            label="旧站已计入余额"
            value={formatInteger(network.harem_reward.amount) + " 魔力值"}
          />
        </div>

        <Alert>
          <CoinsIcon />
          <AlertTitle>
            {policy.enabled ? "后宫加成正在运行" : "后宫加成当前停用"}
          </AlertTitle>
          <AlertDescription>
            计入直属 {formatInteger(policy.depth)} 层成员做种奖励的{" "}
            {rewardPercent}%，成员至少做种{" "}
            {formatInteger(policy.minimum_seed_count)} 个且最近{" "}
            {formatInteger(policy.activity_days)} 天有活动；每小时最高{" "}
            {formatInteger(policy.hourly_cap)} 魔力值。权益仍按小时计算，每{" "}
            {formatInteger(policy.settlement_hours)}{" "}
            小时合并入账一次，减少数据库流水。
            {live.last_settled_at
              ? " 最近一次结算：" + formatDateTime(live.last_settled_at) + "。"
              : ""}
          </AlertDescription>
        </Alert>

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

        <InvitationMembersTable members={network.direct_members} showRewards />
      </CardContent>
    </Card>
  )
}

function InvitationTree({
  network,
}: {
  network: InvitationOverview["network"]
}) {
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>邀请树</CardTitle>
          <CardDescription>
            当前直属成员 {formatInteger(network.direct_count)} 人，全部后代共{" "}
            {formatInteger(network.total_descendants)} 人。
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        <InvitationMembersTable members={network.direct_members} />
      </CardContent>
    </Card>
  )
}

function InvitationChain({
  currentUsername,
  network,
}: {
  currentUsername: string
  network: InvitationOverview["network"]
}) {
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>邀请链（上家）</CardTitle>
          <CardDescription>
            从你开始，依次展示直属上家直到邀请根节点。
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        {network.ancestor_members.length ? (
          <div className="flex flex-wrap items-center gap-2">
            <Badge className="px-3 py-2 text-sm">{currentUsername}（你）</Badge>
            {network.ancestor_members.map((member) => (
              <React.Fragment key={member.numeric_id}>
                <ChevronRightIcon className="size-4 text-muted-foreground" />
                <div className="rounded-lg border px-3 py-2">
                  <p className="text-sm font-medium">{member.display_name}</p>
                  <p className="text-xs text-muted-foreground">
                    @{member.username} ·{" "}
                    {member.source === "legacy_import"
                      ? "Rousi 继承"
                      : "PeerGo 邀请"}
                  </p>
                </div>
              </React.Fragment>
            ))}
          </div>
        ) : (
          <Empty className="min-h-40">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <GitForkIcon />
              </EmptyMedia>
              <EmptyTitle>没有上家邀请链</EmptyTitle>
              <EmptyDescription>
                该账户由开放注册或系统邀请创建。
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
      </CardContent>
    </Card>
  )
}

function InvitationMembersTable({
  members,
  showRewards = false,
}: {
  members: InvitationOverview["network"]["direct_members"]
  showRewards?: boolean
}) {
  if (!members.length) {
    return (
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
    )
  }
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>直属成员</TableHead>
            <TableHead>用户 ID</TableHead>
            {showRewards ? (
              <>
                <TableHead>计入状态</TableHead>
                <TableHead className="text-right">做种数</TableHead>
                <TableHead className="text-right">做种奖励 / 小时</TableHead>
                <TableHead className="text-right">预计贡献 / 小时</TableHead>
                <TableHead className="text-right">最近活动</TableHead>
              </>
            ) : (
              <TableHead>来源</TableHead>
            )}
            <TableHead className="text-right">建立时间</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {members.map((member) => (
            <TableRow key={member.numeric_id}>
              <TableCell>
                <div className="flex flex-col gap-0.5">
                  <span>{member.display_name}</span>
                  <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <span>@{member.username}</span>
                    {showRewards ? (
                      <Badge variant="outline" className="text-[10px]">
                        {member.source === "legacy_import"
                          ? "Rousi 继承"
                          : "PeerGo 邀请"}
                      </Badge>
                    ) : null}
                  </div>
                </div>
              </TableCell>
              <TableCell className="tabular-nums">
                {formatInteger(member.numeric_id)}
              </TableCell>
              {showRewards ? (
                <>
                  <TableCell>
                    <Badge
                      variant={member.harem_eligible ? "secondary" : "outline"}
                    >
                      {member.harem_eligible ? "正在贡献" : "暂未计入"}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(member.current_seeding_count)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(member.current_seeding_reward)}
                  </TableCell>
                  <TableCell className="text-right font-medium text-purple-600 tabular-nums">
                    {formatMilliMagic(member.current_contribution_milli)}
                  </TableCell>
                  <TableCell className="text-right whitespace-nowrap text-muted-foreground">
                    {member.last_active_at
                      ? formatDateTime(member.last_active_at)
                      : "暂无记录"}
                  </TableCell>
                </>
              ) : (
                <TableCell>
                  <Badge variant="outline">
                    {member.source === "legacy_import"
                      ? "Rousi 继承"
                      : "PeerGo 邀请"}
                  </Badge>
                </TableCell>
              )}
              <TableCell className="text-right whitespace-nowrap text-muted-foreground">
                {formatDateTime(member.established_at)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function formatMilliMagic(value: string) {
  const milli = BigInt(value)
  const whole = milli / 1_000n
  const fraction = (milli % 1_000n)
    .toString()
    .padStart(3, "0")
    .replace(/0+$/, "")
  return fraction
    ? whole.toLocaleString("zh-CN") + "." + fraction
    : whole.toLocaleString("zh-CN")
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
  canRevoke,
  revokePending,
  onRevoke,
  onPrevious,
  onNext,
}: {
  items: MemberInvitation[]
  total: number
  offset: number
  canRevoke: boolean
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
            共 {total.toLocaleString("zh-CN")} 条记录。签发者可以撤回尚未领取的
            PeerGo 邀请并立即返还名额；注册处理中、已使用及历史邀请不可撤回。
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent className="px-0">
        {items.length ? (
          <div className="grid gap-3 px-6 pb-2">
            {items.map((item) => (
              <div
                key={item.id}
                className={cn(
                  "flex flex-col gap-3 rounded-lg border p-4 sm:flex-row sm:items-center sm:justify-between",
                  item.status === "used" && "bg-muted/40"
                )}
              >
                <div className="grid gap-1.5">
                  <div className="flex flex-wrap items-center gap-2">
                    <InvitationStatusBadge status={item.status} />
                    {item.invitee_username ? (
                      <span className="text-sm font-medium">
                        已被 {item.invitee_username} 使用
                      </span>
                    ) : null}
                    <Badge variant="outline">
                      {item.source === "legacy_import"
                        ? "Rousi 继承"
                        : "PeerGo"}
                    </Badge>
                    <Badge variant="outline">
                      {item.email_bound
                        ? "已绑定邮箱"
                        : "旧邀请码 · 未绑定邮箱"}
                    </Badge>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    创建于 {formatDateTime(item.created_at)} · 有效期至{" "}
                    {formatDateTime(item.expires_at)}
                  </p>
                </div>
                {item.status === "available" && canRevoke ? (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={revokePending}
                    onClick={() => onRevoke(item)}
                  >
                    撤销并返还名额
                  </Button>
                ) : null}
              </div>
            ))}
          </div>
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
  tone,
}: {
  icon: typeof TicketIcon
  label: string
  value: string
  tone?: string
}) {
  return (
    <Card className="py-4">
      <CardContent className="flex items-center gap-3">
        <div className="flex size-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Icon className="size-4" />
        </div>
        <div>
          <p className="text-xs text-muted-foreground">{label}</p>
          <p className={cn("text-lg font-semibold tabular-nums", tone)}>
            {value}
          </p>
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
    return "当前没有剩余邀请名额。生成邀请码会立即消耗一个名额；仅撤销未使用的邀请码会返还，过期不会自动返还。"
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
