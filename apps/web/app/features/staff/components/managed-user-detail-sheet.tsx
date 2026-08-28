import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  ActivityIcon,
  CircleAlertIcon,
  Clock3Icon,
  CrownIcon,
  DatabaseZapIcon,
  ExternalLinkIcon,
  NetworkIcon,
  RefreshCwIcon,
  ShieldAlertIcon,
  UserRoundIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
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
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog"
import { Separator } from "~/components/ui/separator"
import { Skeleton } from "~/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "~/components/ui/tabs"
import {
  managedUserDetailQueryOptions,
  type ManagedUserDetail,
} from "~/features/staff/api/user-administration.queries"
import { AccountRestrictionControls } from "~/features/staff/components/account-restriction-controls"
import { ManagedUserAccountActions } from "~/features/staff/components/managed-user-account-actions"
import { ManagedUserDataAdjustment } from "~/features/staff/components/managed-user-data-adjustment"
import { ManagedUserNetworkHistoryCard } from "~/features/staff/components/managed-user-network-history-card"
import { ManagedUserStateBadges } from "~/features/staff/components/managed-user-table"
import { ManualDownloadRestrictionControls } from "~/features/staff/components/manual-download-restriction-controls"
import { VIPControls } from "~/features/staff/components/vip-controls"
import { accountRestrictionReasonLabel } from "~/features/staff/model/account-restriction-form"
import { managedTrackerActivityQueryOptions } from "~/features/user/api/tracker-activity.queries"
import { UserTrackerActivityCard } from "~/features/user/components/user-tracker-activity-card"
import { UserAvatar } from "~/shared/components/user-avatar"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function ManagedUserDialog({
  open,
  onOpenChange,
  userId,
  csrfToken,
  currentStaffUserId,
  canRestrict,
  canRevoke,
  canDownloadRestrict,
  canDownloadRevoke,
  canManageVIP,
  canAssignAssessment,
  canAdjustData,
  canReadNetworkHistory,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId?: string
  csrfToken: string
  currentStaffUserId: string
  canRestrict: boolean
  canRevoke: boolean
  canDownloadRestrict: boolean
  canDownloadRevoke: boolean
  canManageVIP: boolean
  canAssignAssessment: boolean
  canAdjustData: boolean
  canReadNetworkHistory: boolean
}) {
  const [activeTab, setActiveTab] = React.useState("profile")
  const detail = useQuery({
    ...managedUserDetailQueryOptions(userId ?? ""),
    enabled: open && Boolean(userId),
  })
  const trackerActivity = useQuery({
    ...managedTrackerActivityQueryOptions(userId ?? ""),
    enabled: open && activeTab === "activity" && Boolean(detail.data),
  })

  React.useEffect(() => {
    if (open) setActiveTab("profile")
  }, [open, userId])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="h-[min(780px,calc(100vh-2rem))] grid-rows-[auto_minmax(0,1fr)] gap-0 overflow-hidden p-0 sm:max-w-5xl">
        <DialogHeader className="border-b px-5 py-4 pr-14">
          <DialogTitle>用户设置</DialogTitle>
          <DialogDescription>
            分区查看和管理账户资料；密码、Passkey 与 API Key 不会显示。
          </DialogDescription>
        </DialogHeader>

        {detail.isPending ? (
          <div className="overflow-hidden p-5">
            <DetailSkeleton />
          </div>
        ) : detail.isError || !detail.data ? (
          <div className="p-5">
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>账户详情暂时无法读取</AlertTitle>
              <AlertDescription>
                目标可能已经不存在，或后台会话已失效。
              </AlertDescription>
            </Alert>
            <Button
              variant="outline"
              size="sm"
              className="mt-3"
              onClick={() => void detail.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </div>
        ) : (
          <div className="flex min-h-0 flex-col">
            <UserDialogHeader detail={detail.data} />

            <Tabs
              key={detail.data.id}
              value={activeTab}
              onValueChange={setActiveTab}
              className="min-h-0 flex-1 gap-0"
            >
              <div className="overflow-x-auto border-b px-5">
                <TabsList
                  variant="line"
                  className="h-11 w-max min-w-full justify-start"
                  aria-label="用户设置分区"
                >
                  <TabsTrigger value="profile">
                    <UserRoundIcon />
                    基础信息
                  </TabsTrigger>
                  <TabsTrigger value="data">
                    <DatabaseZapIcon />
                    数据设置
                  </TabsTrigger>
                  {canReadNetworkHistory ? (
                    <TabsTrigger value="network">
                      <NetworkIcon />
                      IP 历史
                    </TabsTrigger>
                  ) : null}
                  <TabsTrigger value="activity">
                    <ActivityIcon />
                    BT 在线
                  </TabsTrigger>
                  <TabsTrigger value="benefits">
                    <CrownIcon />
                    VIP 与考核
                  </TabsTrigger>
                  <TabsTrigger value="restrictions">
                    <ShieldAlertIcon />
                    访问限制
                  </TabsTrigger>
                </TabsList>
              </div>

              <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
                <TabsContent value="profile">
                  {activeTab === "profile" ? (
                    <ProfileTab detail={detail.data} />
                  ) : null}
                </TabsContent>

                <TabsContent value="data">
                  {activeTab === "data" ? (
                    <DataTab
                      detail={detail.data}
                      csrfToken={csrfToken}
                      currentStaffUserId={currentStaffUserId}
                      canAdjustData={canAdjustData}
                    />
                  ) : null}
                </TabsContent>

                {canReadNetworkHistory ? (
                  <TabsContent value="network">
                    {activeTab === "network" ? (
                      <ManagedUserNetworkHistoryCard userId={detail.data.id} />
                    ) : null}
                  </TabsContent>
                ) : null}

                <TabsContent value="activity">
                  {activeTab === "activity" ? (
                    <UserTrackerActivityCard
                      activity={trackerActivity.data}
                      loading={trackerActivity.isPending}
                      error={trackerActivity.isError}
                      visibility="admin"
                    />
                  ) : null}
                </TabsContent>

                <TabsContent value="benefits">
                  {activeTab === "benefits" ? (
                    <div className="flex flex-col gap-5">
                      <VIPControls
                        detail={detail.data}
                        csrfToken={csrfToken}
                        currentStaffUserId={currentStaffUserId}
                        canManage={canManageVIP}
                      />
                      <Separator />
                      <ManagedUserAccountActions
                        detail={detail.data}
                        csrfToken={csrfToken}
                        currentStaffUserId={currentStaffUserId}
                        canReactivate={canRevoke}
                        canAssignAssessment={canAssignAssessment}
                      />
                    </div>
                  ) : null}
                </TabsContent>

                <TabsContent value="restrictions">
                  {activeTab === "restrictions" ? (
                    <div className="flex flex-col gap-5">
                      <CurrentRestrictionSummary detail={detail.data} />
                      <Separator />
                      <ManualDownloadRestrictionControls
                        detail={detail.data}
                        csrfToken={csrfToken}
                        currentStaffUserId={currentStaffUserId}
                        canRestrict={canDownloadRestrict}
                        canRevoke={canDownloadRevoke}
                      />
                      <Separator />
                      <AccountRestrictionControls
                        detail={detail.data}
                        csrfToken={csrfToken}
                        currentStaffUserId={currentStaffUserId}
                        canRestrict={canRestrict}
                        canRevoke={canRevoke}
                        onRefresh={() => void detail.refetch()}
                      />
                    </div>
                  ) : null}
                </TabsContent>
              </div>
            </Tabs>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

function UserDialogHeader({ detail }: { detail: ManagedUserDetail }) {
  return (
    <div className="flex flex-col gap-3 border-b px-5 py-4 sm:flex-row sm:items-center">
      <UserAvatar
        username={detail.username}
        displayName={detail.display_name}
        size="lg"
      />
      <div className="min-w-0 flex-1">
        <div className="truncate font-heading text-lg font-semibold">
          {detail.display_name}
        </div>
        <div className="truncate text-sm text-muted-foreground">
          @{detail.username} · {detail.email || "邮箱不可用"}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2 sm:justify-end">
        <ManagedUserStateBadges user={detail} />
        <Badge variant="outline">ID {detail.numeric_id}</Badge>
      </div>
    </div>
  )
}

function ProfileTab({ detail }: { detail: ManagedUserDetail }) {
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card className="gap-0 py-0">
        <CardHeader className="border-b px-4 py-3">
          <CardTitle>账户资料</CardTitle>
          <CardDescription>身份、权限状态与注册来源。</CardDescription>
        </CardHeader>
        <CardContent className="p-4">
          <dl className="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-2 text-sm">
            <DetailTerm>用户 ID</DetailTerm>
            <DetailValue className="font-mono tabular-nums">
              {detail.numeric_id}
            </DetailValue>
            <DetailTerm>PeerGo UUID</DetailTerm>
            <DetailValue className="font-mono text-xs break-all text-muted-foreground">
              {detail.id}
            </DetailValue>
            <DetailTerm>角色</DetailTerm>
            <DetailValue>
              {detail.role_names.length
                ? detail.role_names.join("、")
                : "未分配（注册待恢复）"}
            </DetailValue>
            <DetailTerm>邮箱验证</DetailTerm>
            <DetailValue>
              {detail.email_verified ? "已验证" : "未验证"}
            </DetailValue>
            <DetailTerm>下载权限</DetailTerm>
            <DetailValue>
              {detail.download_restricted ? "下载受限" : "正常"}
            </DetailValue>
            <DetailTerm>VIP 状态</DetailTerm>
            <DetailValue>{vipStatusLabel(detail)}</DetailValue>
            <DetailTerm>注册事务</DetailTerm>
            <DetailValue>
              {registrationStateLabel(
                detail.registration_mode,
                detail.registration_state
              )}
            </DetailValue>
          </dl>
          <Button
            render={<Link to={`/user/${detail.username}`} />}
            nativeButton={false}
            variant="outline"
            size="sm"
            className="mt-4"
          >
            <ExternalLinkIcon data-icon="inline-start" />
            查看公开主页
          </Button>
        </CardContent>
      </Card>

      <Card className="gap-0 py-0">
        <CardHeader className="border-b px-4 py-3">
          <CardTitle>邀请与发布</CardTitle>
          <CardDescription>邀请链路、种子贡献与账户时间。</CardDescription>
        </CardHeader>
        <CardContent className="p-4">
          <dl className="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-2 text-sm">
            <DetailTerm>可用邀请</DetailTerm>
            <DetailValue className="tabular-nums">
              {formatInteger(detail.remaining_invites)} 个
            </DetailValue>
            <DetailTerm>直属邀请</DetailTerm>
            <DetailValue className="tabular-nums">
              {formatInteger(detail.direct_invite_count)} 人
            </DetailValue>
            <DetailTerm>邀请人</DetailTerm>
            <DetailValue>
              {detail.inviter_username && detail.inviter_numeric_id ? (
                <Link
                  to={`/user/${detail.inviter_username}`}
                  className="text-primary underline-offset-4 hover:underline"
                >
                  @{detail.inviter_username} · ID {detail.inviter_numeric_id}
                </Link>
              ) : (
                "无（开放注册或邀请根节点）"
              )}
            </DetailValue>
            <DetailTerm>发布种子</DetailTerm>
            <DetailValue className="tabular-nums">
              已发布 {formatInteger(detail.published_torrent_count)} · 待审核{" "}
              {formatInteger(detail.pending_review_torrent_count)} · 累计提交{" "}
              {formatInteger(detail.submitted_torrent_count)}
            </DetailValue>
            <DetailTerm>最后活跃</DetailTerm>
            <DetailValue>
              {detail.last_active_at
                ? formatDateTime(detail.last_active_at)
                : "从未活跃"}
            </DetailValue>
            <DetailTerm>加入时间</DetailTerm>
            <DetailValue>{formatDateTime(detail.created_at)}</DetailValue>
            <DetailTerm>最近变更</DetailTerm>
            <DetailValue>{formatDateTime(detail.updated_at)}</DetailValue>
          </dl>
        </CardContent>
      </Card>
    </div>
  )
}

function DataTab({
  detail,
  csrfToken,
  currentStaffUserId,
  canAdjustData,
}: {
  detail: ManagedUserDetail
  csrfToken: string
  currentStaffUserId: string
  canAdjustData: boolean
}) {
  const metrics = [
    ["实际上传", formatBytes(detail.uploaded_bytes)],
    ["实际下载", formatBytes(detail.downloaded_bytes)],
    [
      "分享率",
      formatShareRatio(detail.uploaded_bytes, detail.downloaded_bytes),
    ],
    ["魔力值", formatInteger(detail.magic_balance)],
    [
      "等级 / 经验",
      `Lv.${detail.level} · ${formatExperience(detail.experience)}`,
    ],
    ["捐赠金额", `¥${formatDonation(detail.donation_amount)}`],
  ]

  return (
    <div className="flex flex-col gap-4">
      <Card className="gap-0 py-0">
        <CardHeader className="border-b px-4 py-3">
          <CardTitle>当前账户数据</CardTitle>
          <CardDescription>
            展示实时投影；人工调整会走对应账本并留下审计记录。
          </CardDescription>
        </CardHeader>
        <CardContent className="grid grid-cols-2 gap-x-6 gap-y-4 p-4 md:grid-cols-3">
          {metrics.map(([label, value]) => (
            <div key={label} className="min-w-0">
              <div className="text-xs text-muted-foreground">{label}</div>
              <div className="mt-1 truncate font-medium tabular-nums">
                {value}
              </div>
            </div>
          ))}
        </CardContent>
      </Card>

      {canAdjustData ? (
        <ManagedUserDataAdjustment
          detail={detail}
          csrfToken={csrfToken}
          disabled={detail.id === currentStaffUserId}
        />
      ) : (
        <Alert>
          <DatabaseZapIcon />
          <AlertTitle>当前权限仅可查看</AlertTitle>
          <AlertDescription>
            需要“用户数据调整”权限后才能增减流量、魔力、经验、邀请或捐赠。
          </AlertDescription>
        </Alert>
      )}
    </div>
  )
}

function CurrentRestrictionSummary({ detail }: { detail: ManagedUserDetail }) {
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h2 className="font-heading font-medium">当前账户访问限制</h2>
          <p className="text-xs text-muted-foreground">
            仅显示当前生效且尚未解除的限制。
          </p>
        </div>
        <Badge variant="secondary" className="tabular-nums">
          {detail.active_restriction_count}
        </Badge>
      </div>

      {detail.active_restrictions.length === 0 ? (
        <Alert>
          <Clock3Icon />
          <AlertTitle>当前没有有效限制</AlertTitle>
          <AlertDescription>
            该用户目前可以正常登录和使用站点。
          </AlertDescription>
        </Alert>
      ) : (
        detail.active_restrictions.map((restriction) => (
          <Alert key={restriction.id} variant="destructive">
            <ShieldAlertIcon />
            <AlertTitle>账户访问已限制</AlertTitle>
            <AlertDescription className="flex flex-col gap-2">
              <span>{restriction.reason_summary}</span>
              <span className="text-xs">
                {restrictionReasonLabel(restriction.reason_code)} · 至{" "}
                <time dateTime={restriction.expires_at}>
                  {formatDateTime(restriction.expires_at)}
                </time>
              </span>
            </AlertDescription>
          </Alert>
        ))
      )}
    </section>
  )
}

function vipStatusLabel(user: {
  vip_enabled: boolean
  vip_active: boolean
  vip_until?: string | null
}) {
  if (!user.vip_enabled) {
    return "非 VIP"
  }
  if (!user.vip_active) {
    return user.vip_until
      ? `已于 ${formatDateTime(user.vip_until)} 到期`
      : "已失效"
  }
  return user.vip_until
    ? `有效至 ${formatDateTime(user.vip_until)}`
    : "永久 VIP"
}

function formatExperience(value: string) {
  const [integer, fraction = ""] = value.split(".")
  const visibleFraction = fraction.slice(0, 2).replace(/0+$/, "")
  return visibleFraction
    ? `${formatInteger(integer)}.${visibleFraction}`
    : formatInteger(integer)
}

function formatDonation(value: string) {
  const amount = Number(value)
  return Number.isFinite(amount)
    ? amount.toLocaleString("zh-CN", {
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      })
    : value
}

function formatShareRatio(uploaded: string, downloaded: string) {
  const uploadedBytes = BigInt(uploaded)
  const downloadedBytes = BigInt(downloaded)
  if (downloadedBytes === 0n) return uploadedBytes > 0n ? "∞" : "—"
  const hundredths = (uploadedBytes * 100n) / downloadedBytes
  return `${hundredths / 100n}.${(hundredths % 100n)
    .toString()
    .padStart(2, "0")}`
}

function registrationStateLabel(
  mode: "open" | "invite" | null | undefined,
  state: "reserved" | "credential_provisioned" | "completed" | null | undefined
) {
  if (!mode || !state) return "Rousi 迁移账户"
  const modeLabel = mode === "invite" ? "邀请注册" : "开放注册"
  const stateLabel = {
    reserved: "已预留",
    credential_provisioned: "待自动恢复",
    completed: "已完成",
  }[state]
  return `${modeLabel} · ${stateLabel}`
}

function restrictionReasonLabel(reasonCode: string) {
  return reasonCode === "manual_review" || reasonCode === "security_incident"
    ? accountRestrictionReasonLabel(reasonCode)
    : "其他管理原因"
}

function DetailTerm({ children }: { children: React.ReactNode }) {
  return <dt className="text-muted-foreground">{children}</dt>
}

function DetailValue({
  children,
  className,
}: {
  children: React.ReactNode
  className?: string
}) {
  return <dd className={className}>{children}</dd>
}

function DetailSkeleton() {
  return (
    <div
      className="flex flex-col gap-5"
      aria-label="正在加载账户详情"
      aria-busy="true"
    >
      <div className="flex items-center gap-3">
        <Skeleton className="size-10 rounded-full" />
        <div className="flex flex-1 flex-col gap-2">
          <Skeleton className="h-5 w-36" />
          <Skeleton className="h-4 w-24" />
        </div>
      </div>
      <Skeleton className="h-10 w-full" />
      <div className="grid gap-4 lg:grid-cols-2">
        <Skeleton className="h-72 w-full" />
        <Skeleton className="h-72 w-full" />
      </div>
    </div>
  )
}
