import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  Clock3Icon,
  ExternalLinkIcon,
  RefreshCwIcon,
  ShieldAlertIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Separator } from "~/components/ui/separator"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "~/components/ui/sheet"
import { Skeleton } from "~/components/ui/skeleton"
import { managedUserDetailQueryOptions } from "~/features/staff/api/user-administration.queries"
import { AccountRestrictionControls } from "~/features/staff/components/account-restriction-controls"
import { ManagedUserAccountActions } from "~/features/staff/components/managed-user-account-actions"
import { ManualDownloadRestrictionControls } from "~/features/staff/components/manual-download-restriction-controls"
import { VIPControls } from "~/features/staff/components/vip-controls"
import { ManagedUserStateBadges } from "~/features/staff/components/managed-user-table"
import { accountRestrictionReasonLabel } from "~/features/staff/model/account-restriction-form"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatInteger } from "~/shared/formatters/integer"
import { UserAvatar } from "~/shared/components/user-avatar"
import { managedTrackerActivityQueryOptions } from "~/features/user/api/tracker-activity.queries"
import { UserTrackerActivityCard } from "~/features/user/components/user-tracker-activity-card"

export function ManagedUserDetailSheet({
  userId,
  csrfToken,
  currentStaffUserId,
  canRestrict,
  canRevoke,
  canDownloadRestrict,
  canDownloadRevoke,
  canManageVIP,
  canAssignAssessment,
  onOpenChange,
}: {
  userId?: string
  csrfToken: string
  currentStaffUserId: string
  canRestrict: boolean
  canRevoke: boolean
  canDownloadRestrict: boolean
  canDownloadRevoke: boolean
  canManageVIP: boolean
  canAssignAssessment: boolean
  onOpenChange: (open: boolean) => void
}) {
  const detail = useQuery(managedUserDetailQueryOptions(userId ?? ""))
  const trackerActivity = useQuery({
    ...managedTrackerActivityQueryOptions(userId ?? ""),
    enabled: Boolean(userId && detail.data),
  })

  return (
    <Sheet open={Boolean(userId)} onOpenChange={onOpenChange}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-2xl">
        <SheetHeader className="border-b pr-12">
          <SheetTitle>账户详情</SheetTitle>
          <SheetDescription>
            显示账户标识、运营数据和联系邮箱；密码与凭据不会显示。
          </SheetDescription>
        </SheetHeader>

        {detail.isPending ? (
          <DetailSkeleton />
        ) : detail.isError || !detail.data ? (
          <div className="p-4">
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
          <div className="flex flex-col gap-5 p-4 pt-0">
            <div className="flex items-center gap-3">
              <UserAvatar
                username={detail.data.username}
                displayName={detail.data.display_name}
                size="lg"
              />
              <div className="min-w-0 flex-1">
                <div className="truncate font-heading text-lg font-semibold">
                  {detail.data.display_name}
                </div>
                <div className="truncate text-sm text-muted-foreground">
                  @{detail.data.username} · {detail.data.email || "邮箱不可用"}
                </div>
              </div>
              <ManagedUserStateBadges user={detail.data} />
            </div>

            <Button
              render={<Link to={`/user/${detail.data.username}`} />}
              nativeButton={false}
              variant="outline"
              size="sm"
              className="self-start"
            >
              <ExternalLinkIcon data-icon="inline-start" />
              查看用户公开主页
            </Button>

            <dl className="grid grid-cols-[6rem_1fr] gap-x-3 gap-y-2 text-sm">
              <DetailTerm>用户 ID</DetailTerm>
              <DetailValue className="font-mono tabular-nums">
                {detail.data.numeric_id}
              </DetailValue>
              <DetailTerm>PeerGo UUID</DetailTerm>
              <DetailValue className="font-mono text-xs break-all text-muted-foreground">
                {detail.data.id}
              </DetailValue>
              <DetailTerm>角色</DetailTerm>
              <DetailValue>
                {detail.data.role_names.length
                  ? detail.data.role_names.join("、")
                  : "未分配（注册待恢复）"}
              </DetailValue>
              <DetailTerm>邮箱验证</DetailTerm>
              <DetailValue>
                {detail.data.email_verified ? "已验证" : "未验证"}
              </DetailValue>
              <DetailTerm>下载权限</DetailTerm>
              <DetailValue>
                {detail.data.download_restricted ? "下载受限" : "正常"}
              </DetailValue>
              <DetailTerm>VIP 状态</DetailTerm>
              <DetailValue>{vipStatusLabel(detail.data)}</DetailValue>
              <DetailTerm>实际上传</DetailTerm>
              <DetailValue className="font-medium text-success-foreground tabular-nums">
                {formatBytes(detail.data.uploaded_bytes)}
              </DetailValue>
              <DetailTerm>实际下载</DetailTerm>
              <DetailValue className="font-medium text-info tabular-nums">
                {formatBytes(detail.data.downloaded_bytes)}
              </DetailValue>
              <DetailTerm>分享率</DetailTerm>
              <DetailValue className="tabular-nums">
                {formatShareRatio(
                  detail.data.uploaded_bytes,
                  detail.data.downloaded_bytes
                )}
              </DetailValue>
              <DetailTerm>魔力值</DetailTerm>
              <DetailValue>
                {formatInteger(detail.data.magic_balance)}
              </DetailValue>
              <DetailTerm>等级</DetailTerm>
              <DetailValue>Lv.{detail.data.level}</DetailValue>
              <DetailTerm>经验值</DetailTerm>
              <DetailValue className="tabular-nums">
                {formatExperience(detail.data.experience)}
              </DetailValue>
              <DetailTerm>可用邀请</DetailTerm>
              <DetailValue className="tabular-nums">
                {formatInteger(detail.data.remaining_invites)} 个
              </DetailValue>
              <DetailTerm>直属邀请</DetailTerm>
              <DetailValue className="tabular-nums">
                {formatInteger(detail.data.direct_invite_count)} 人
              </DetailValue>
              <DetailTerm>邀请人</DetailTerm>
              <DetailValue>
                {detail.data.inviter_username &&
                detail.data.inviter_numeric_id ? (
                  <Link
                    to={`/user/${detail.data.inviter_username}`}
                    className="text-primary underline-offset-4 hover:underline"
                  >
                    @{detail.data.inviter_username} · ID{" "}
                    {detail.data.inviter_numeric_id}
                  </Link>
                ) : (
                  "无（开放注册或邀请根节点）"
                )}
              </DetailValue>
              <DetailTerm>发布种子</DetailTerm>
              <DetailValue className="tabular-nums">
                已发布 {formatInteger(detail.data.published_torrent_count)} ·
                待审核 {formatInteger(detail.data.pending_review_torrent_count)}{" "}
                · 累计提交 {formatInteger(detail.data.submitted_torrent_count)}
              </DetailValue>
              <DetailTerm>注册事务</DetailTerm>
              <DetailValue>
                {registrationStateLabel(
                  detail.data.registration_mode,
                  detail.data.registration_state
                )}
              </DetailValue>
              <DetailTerm>最后活跃</DetailTerm>
              <DetailValue>
                {detail.data.last_active_at
                  ? formatDateTime(detail.data.last_active_at)
                  : "从未活跃"}
              </DetailValue>
              <DetailTerm>加入时间</DetailTerm>
              <DetailValue>
                {formatDateTime(detail.data.created_at)}
              </DetailValue>
              <DetailTerm>最近变更</DetailTerm>
              <DetailValue>
                {formatDateTime(detail.data.updated_at)}
              </DetailValue>
            </dl>

            <Separator />

            <UserTrackerActivityCard
              activity={trackerActivity.data}
              loading={trackerActivity.isPending}
              error={trackerActivity.isError}
              visibility="admin"
            />

            <Separator />

            <section className="flex flex-col gap-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <h2 className="font-heading font-medium">当前账户访问限制</h2>
                  <p className="text-xs text-muted-foreground">
                    仅显示当前生效且尚未解除的限制。
                  </p>
                </div>
                <Badge variant="secondary" className="tabular-nums">
                  {detail.data.active_restriction_count}
                </Badge>
              </div>

              {detail.data.active_restrictions.length === 0 ? (
                <Alert>
                  <Clock3Icon />
                  <AlertTitle>当前没有有效限制</AlertTitle>
                  <AlertDescription>
                    该用户目前可以正常登录和使用站点。
                  </AlertDescription>
                </Alert>
              ) : (
                detail.data.active_restrictions.map((restriction) => (
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

            <Separator />

            <VIPControls
              detail={detail.data}
              csrfToken={csrfToken}
              currentStaffUserId={currentStaffUserId}
              canManage={canManageVIP}
            />

            <Separator />

            <ManualDownloadRestrictionControls
              detail={detail.data}
              csrfToken={csrfToken}
              currentStaffUserId={currentStaffUserId}
              canRestrict={canDownloadRestrict}
              canRevoke={canDownloadRevoke}
            />

            <Separator />

            <ManagedUserAccountActions
              detail={detail.data}
              csrfToken={csrfToken}
              currentStaffUserId={currentStaffUserId}
              canReactivate={canRevoke}
              canAssignAssessment={canAssignAssessment}
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
        )}
      </SheetContent>
    </Sheet>
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

function formatShareRatio(uploaded: string, downloaded: string) {
  const uploadedBytes = BigInt(uploaded)
  const downloadedBytes = BigInt(downloaded)
  if (downloadedBytes === 0n) return uploadedBytes > 0n ? "∞" : "—"
  const hundredths = (uploadedBytes * 100n) / downloadedBytes
  return `${hundredths / 100n}.${(hundredths % 100n).toString().padStart(2, "0")}`
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
      className="flex flex-col gap-5 p-4 pt-0"
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
      <Skeleton className="h-28 w-full" />
      <Skeleton className="h-36 w-full" />
    </div>
  )
}
