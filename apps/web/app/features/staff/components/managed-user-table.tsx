import { Settings2Icon, ShieldAlertIcon, UsersRoundIcon } from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import type { ManagedUserSummary } from "~/features/staff/api/user-administration.queries"
import { managedUserStatusLabel } from "~/features/staff/model/user-administration"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"
import { UserAvatar } from "~/shared/components/user-avatar"

export function ManagedUserTable({
  users,
  hasFilters,
  onSelect,
}: {
  users: ManagedUserSummary[]
  hasFilters: boolean
  onSelect: (userId: string) => void
}) {
  if (users.length === 0) {
    return (
      <Empty className="min-h-72 border">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <UsersRoundIcon />
          </EmptyMedia>
          <EmptyTitle>{hasFilters ? "没有匹配账户" : "暂无账户"}</EmptyTitle>
          <EmptyDescription>
            {hasFilters
              ? "请调整用户 ID、UUID、用户名、显示名或状态筛选后重试。"
              : "账户会显示在这里；密码和登录凭据不会出现在列表中。"}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="overflow-hidden rounded-lg border">
      <p className="border-b bg-muted/30 px-3 py-2 text-xs text-muted-foreground md:hidden">
        表格可左右滑动，点击末列设置按钮管理账户
      </p>
      <Table className="min-w-[1120px] lg:min-w-[1480px]">
        <TableHeader className="bg-muted/50">
          <TableRow className="h-11">
            <TableHead className="w-[72px] px-3 text-right">ID</TableHead>
            <TableHead className="hidden w-[248px] lg:table-cell">
              UUID
            </TableHead>
            <TableHead className="w-[230px]">用户</TableHead>
            <TableHead className="w-[120px]">角色</TableHead>
            <TableHead className="w-[180px] text-right">上传 / 下载</TableHead>
            <TableHead className="w-[120px]">魔力值</TableHead>
            <TableHead className="w-16 text-center">等级</TableHead>
            <TableHead className="w-[130px]">最后活跃</TableHead>
            <TableHead className="w-[130px]">注册时间</TableHead>
            <TableHead className="w-[105px]">状态</TableHead>
            <TableHead className="px-2 text-center">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {users.map((user) => (
            <TableRow key={user.id} className="h-[53px] hover:bg-muted/30">
              <TableCell className="px-3 py-2 text-right font-mono text-xs text-muted-foreground tabular-nums">
                {user.numeric_id}
              </TableCell>
              <TableCell className="hidden px-2 py-2 lg:table-cell">
                <code className="font-mono text-[11px] whitespace-nowrap text-muted-foreground">
                  {user.id}
                </code>
              </TableCell>
              <TableCell className="px-2 py-2">
                <UserIdentity user={user} />
              </TableCell>
              <TableCell className="px-2 py-2">
                <RoleBadges roles={user.role_names} />
              </TableCell>
              <TableCell className="px-2 py-2 text-right">
                <TransferAmounts user={user} />
              </TableCell>
              <TableCell className="px-2 py-2 text-sm font-medium text-warning-foreground tabular-nums">
                {formatInteger(user.magic_balance)}
              </TableCell>
              <TableCell className="px-2 py-2 text-center">
                <Badge variant="outline" className="tabular-nums">
                  Lv.{user.level}
                </Badge>
              </TableCell>
              <TableCell className="px-2 py-2">
                {user.last_active_at ? (
                  <UserTime value={user.last_active_at} />
                ) : (
                  <span className="text-xs text-muted-foreground">
                    从未活跃
                  </span>
                )}
              </TableCell>
              <TableCell className="px-2 py-2">
                <UserTime value={user.created_at} />
              </TableCell>
              <TableCell className="px-2 py-2">
                <div className="flex flex-col items-start gap-1">
                  <ManagedUserStateBadges user={user} />
                  {user.active_restriction_count > 0 ? (
                    <RestrictionBadge count={user.active_restriction_count} />
                  ) : null}
                </div>
              </TableCell>
              <TableCell className="px-2 py-2 text-center">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="size-7"
                  onClick={() => onSelect(user.id)}
                  aria-label={`管理账户 ${user.username}`}
                  title="设置"
                >
                  <Settings2Icon />
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function TransferAmounts({ user }: { user: ManagedUserSummary }) {
  return (
    <span className="inline-flex items-center justify-end gap-1 text-sm font-medium whitespace-nowrap tabular-nums">
      <span className="text-success-foreground">
        {formatBytes(user.uploaded_bytes)}
      </span>
      <span className="text-muted-foreground">/</span>
      <span className="text-info">{formatBytes(user.downloaded_bytes)}</span>
    </span>
  )
}

function UserIdentity({ user }: { user: ManagedUserSummary }) {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <UserAvatar
        username={user.username}
        displayName={user.display_name}
        colorSeed={user.username}
        className="size-7"
        fallbackClassName="bg-muted text-xs text-muted-foreground"
      />
      <div className="flex min-w-0 flex-col">
        <span className="flex min-w-0 items-center gap-1">
          <span className="truncate font-medium text-primary">
            {user.username}
          </span>
          <VIPBadge user={user} />
        </span>
        <span className="truncate text-xs text-muted-foreground">
          {user.email || "邮箱不可用"}
        </span>
      </div>
    </div>
  )
}

function VIPBadge({ user }: { user: ManagedUserSummary }) {
  if (!user.vip_enabled) {
    return null
  }
  if (!user.vip_active) {
    return (
      <Badge variant="outline" className="shrink-0 px-1 py-0 text-[10px]">
        VIP 已过期
      </Badge>
    )
  }
  return (
    <Badge
      variant="outline"
      className="shrink-0 border-warning/40 bg-warning/10 px-1 py-0 text-[10px] text-warning-foreground"
      title={
        user.vip_until ? `VIP 至 ${formatDateTime(user.vip_until)}` : "永久 VIP"
      }
    >
      {user.vip_until ? "VIP" : "永久 VIP"}
    </Badge>
  )
}

export function ManagedUserStateBadges({ user }: { user: ManagedUserSummary }) {
  return (
    <div className="flex flex-wrap items-center gap-1">
      {user.banned ? (
        <Badge variant="destructive">已封禁</Badge>
      ) : (
        <ManagedUserStatusBadge status={user.status} />
      )}
      {user.download_restricted ? (
        <Badge
          variant="outline"
          className="border-warning/40 bg-warning/10 text-warning-foreground"
        >
          下载受限
        </Badge>
      ) : null}
      {!user.email_verified ? <Badge variant="secondary">未验证</Badge> : null}
    </div>
  )
}

function RoleBadges({ roles }: { roles: string[] }) {
  if (!roles.length) {
    return (
      <Badge
        variant="outline"
        className="border-warning/40 bg-warning/10 text-warning-foreground"
      >
        未分配
      </Badge>
    )
  }
  return (
    <div className="flex flex-wrap gap-1">
      {roles.slice(0, 2).map((role) => (
        <Badge key={role} variant="secondary" className="max-w-28 truncate">
          {role}
        </Badge>
      ))}
      {roles.length > 2 ? (
        <Badge variant="outline">+{roles.length - 2}</Badge>
      ) : null}
    </div>
  )
}

export function ManagedUserStatusBadge({
  status,
}: {
  status: ManagedUserSummary["status"]
}) {
  if (status === "active") {
    return (
      <Badge
        variant="outline"
        className="border-success/30 bg-success/10 text-success-foreground"
      >
        {managedUserStatusLabel(status)}
      </Badge>
    )
  }
  if (status === "pending") {
    return (
      <Badge
        variant="outline"
        className="border-warning/40 bg-warning/10 text-warning-foreground"
      >
        {managedUserStatusLabel(status)}
      </Badge>
    )
  }
  return <Badge variant="destructive">{managedUserStatusLabel(status)}</Badge>
}

function RestrictionBadge({ count }: { count: number }) {
  return count > 0 ? (
    <Badge variant="destructive">
      <ShieldAlertIcon data-icon="inline-start" />
      {count.toLocaleString("zh-CN")} 项有效限制
    </Badge>
  ) : (
    <span className="text-xs text-muted-foreground">允许访问</span>
  )
}

function UserTime({ value }: { value: string }) {
  return (
    <time dateTime={value} className="text-xs text-muted-foreground">
      {formatDateTime(value)}
    </time>
  )
}
