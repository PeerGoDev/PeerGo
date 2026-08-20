import * as React from "react"
import { Link, useNavigate } from "react-router"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  Clock3Icon,
  LogInIcon,
  LogOutIcon,
  MonitorSmartphoneIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  ShieldXIcon,
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
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
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
import {
  useAccountSecurity,
  useRevokeOtherWebSessions,
  useRevokeUserWebSession,
  useUserWebSessions,
  type UserWebSession,
} from "~/features/auth/api/session-security.queries"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { AccountSettingsLayout } from "~/features/auth/components/account-settings-layout"
import { TwoFactorCard } from "~/features/auth/components/two-factor-card"
import { formatDateTime } from "~/shared/formatters/date-time"

export function AccountSecurityPage() {
  const navigate = useNavigate()
  const session = useWebSession()
  const userId = session.data?.user.id
  const overview = useAccountSecurity(userId)
  const sessions = useUserWebSessions(userId)
  const revokeSession = useRevokeUserWebSession(userId)
  const revokeOthers = useRevokeOtherWebSessions(userId)

  if (session.isPending) {
    return <AccountSecuritySkeleton />
  }
  if (session.isError) {
    return (
      <AccountSecurityFrame>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>暂时无法读取登录状态</AlertTitle>
          <AlertDescription>请刷新页面后重试。</AlertDescription>
        </Alert>
      </AccountSecurityFrame>
    )
  }
  if (!session.data) {
    return (
      <AccountSecurityFrame>
        <Card className="rounded-lg border ring-0">
          <CardHeader>
            <CardTitle>需要登录</CardTitle>
            <CardDescription>
              登录后可以查看安全状态和仍然有效的浏览器登录。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Alert>
              <ShieldCheckIcon />
              <AlertTitle>没有可用会话</AlertTitle>
              <AlertDescription>
                登录后只能查看和撤销自己的设备会话。
              </AlertDescription>
            </Alert>
          </CardContent>
          <CardFooter>
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          </CardFooter>
        </Card>
      </AccountSecurityFrame>
    )
  }

  const activeSession = session.data
  const otherSessionCount =
    sessions.data?.items.filter((item) => !item.current).length ?? 0

  async function handleRevoke(item: UserWebSession) {
    await revokeSession.mutateAsync({
      sessionId: item.id,
      current: item.current,
      csrfToken: activeSession.csrf_token,
    })
    if (item.current) {
      navigate("/login")
    }
  }

  return (
    <AccountSecurityFrame>
      {overview.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>安全状态暂时无法读取</AlertTitle>
          <AlertDescription>设备会话仍可单独重试读取。</AlertDescription>
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
      ) : null}

      {overview.isPending ? (
        <SecurityStatusSkeleton />
      ) : overview.data ? (
        <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
          <CardHeader className="px-6 pt-6 pb-4">
            <CardTitle>
              <h2 className="text-2xl leading-none font-semibold">修改密码</h2>
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4 px-6 pb-6">
            <div className="flex flex-col gap-1 text-sm">
              <p className="font-medium">当前账户已设置密码</p>
              <p className="text-xs text-muted-foreground">
                最近变更：
                <time dateTime={overview.data.password_changed_at}>
                  {formatDateTime(overview.data.password_changed_at)}
                </time>
              </p>
            </div>
            <Link
              to="/forgot-password"
              className={buttonVariants({ className: "w-[88px] self-start" })}
            >
              修改密码
            </Link>
          </CardContent>
        </Card>
      ) : null}

      {overview.data ? (
        <TwoFactorCard
          status={overview.data.two_factor}
          userId={activeSession.user.id}
          csrfToken={activeSession.csrf_token}
        />
      ) : null}

      <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
        <CardHeader className="border-b px-6 py-5">
          <CardTitle>
            <h2 className="text-2xl leading-none font-semibold">设备会话</h2>
          </CardTitle>
          <CardDescription>查看并撤销仍然有效的浏览器登录。</CardDescription>
          <CardAction>
            <RevokeOthersDialog
              count={otherSessionCount}
              pending={revokeOthers.isPending}
              onConfirm={async () => {
                await revokeOthers.mutateAsync(activeSession.csrf_token)
              }}
            />
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {revokeSession.isError || revokeOthers.isError ? (
            <div className="px-4 pb-4">
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>撤销失败</AlertTitle>
                <AlertDescription>
                  当前会话可能已经变化，请刷新后重试。
                </AlertDescription>
              </Alert>
            </div>
          ) : null}
          {revokeOthers.isSuccess ? (
            <div className="px-4 pb-4">
              <Alert>
                <CircleCheckIcon />
                <AlertTitle>其他会话已处理</AlertTitle>
                <AlertDescription>
                  当前浏览器保持登录，其他有效浏览器登录已经退出。
                </AlertDescription>
              </Alert>
            </div>
          ) : null}

          {sessions.isPending ? (
            <SessionTableSkeleton />
          ) : sessions.isError ? (
            <div className="px-4">
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>设备会话暂时无法读取</AlertTitle>
                <AlertDescription>请稍后重试。</AlertDescription>
                <AlertAction>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void sessions.refetch()}
                  >
                    <RefreshCwIcon data-icon="inline-start" />
                    重试
                  </Button>
                </AlertAction>
              </Alert>
            </div>
          ) : sessions.data ? (
            <SessionTable
              items={sessions.data.items}
              pendingId={revokeSession.variables?.sessionId}
              onRevoke={handleRevoke}
            />
          ) : null}
        </CardContent>
        <CardFooter className="gap-2 px-4 py-3 text-xs text-muted-foreground">
          <Clock3Icon className="size-3.5" />
          最近活动时间每五分钟更新一次，仅用于帮助识别会话。
        </CardFooter>
      </Card>
    </AccountSecurityFrame>
  )
}

function AccountSecurityFrame({ children }: { children: React.ReactNode }) {
  return (
    <AccountSettingsLayout
      active="security"
      title="账户安全"
      description="管理登录保护、恢复码与仍然有效的浏览器会话。"
      contentClassName="gap-6"
    >
      {children}
    </AccountSettingsLayout>
  )
}

function SessionTable({
  items,
  pendingId,
  onRevoke,
}: {
  items: UserWebSession[]
  pendingId?: string
  onRevoke: (item: UserWebSession) => Promise<void>
}) {
  return (
    <Table>
      <TableHeader className="bg-secondary/70">
        <TableRow>
          <TableHead className="pl-4">会话</TableHead>
          <TableHead className="hidden lg:table-cell">最近活动</TableHead>
          <TableHead className="hidden xl:table-cell">有效至</TableHead>
          <TableHead className="pr-4 text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((item) => (
          <TableRow key={item.id} className="h-16 hover:bg-accent/70">
            <TableCell className="pl-4">
              <div className="flex items-center gap-3">
                <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                  <MonitorSmartphoneIcon className="size-4" />
                </span>
                <div className="flex flex-col gap-0.5">
                  <div className="flex items-center gap-2 font-medium">
                    {item.current ? "当前浏览器" : "浏览器会话"}
                    {item.current ? (
                      <Badge variant="secondary">当前</Badge>
                    ) : null}
                  </div>
                  <span className="text-xs text-muted-foreground">
                    创建于 {formatDateTime(item.created_at)}
                  </span>
                  <span className="text-xs text-muted-foreground lg:hidden">
                    活动于 {formatDateTime(item.last_seen_at)}
                  </span>
                </div>
              </div>
            </TableCell>
            <TableCell className="hidden lg:table-cell">
              <time dateTime={item.last_seen_at}>
                {formatDateTime(item.last_seen_at)}
              </time>
            </TableCell>
            <TableCell className="hidden xl:table-cell">
              <time dateTime={item.expires_at}>
                {formatDateTime(item.expires_at)}
              </time>
            </TableCell>
            <TableCell className="pr-4 text-right">
              <RevokeSessionDialog
                item={item}
                pending={pendingId === item.id}
                onConfirm={() => onRevoke(item)}
              />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function RevokeSessionDialog({
  item,
  pending,
  onConfirm,
}: {
  item: UserWebSession
  pending: boolean
  onConfirm: () => Promise<void>
}) {
  const [open, setOpen] = React.useState(false)

  async function handleConfirm() {
    try {
      await onConfirm()
      setOpen(false)
    } catch {
      // Mutation state renders the stable error without leaking transport data.
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <AlertDialogTrigger
        render={
          <Button
            variant={item.current ? "outline" : "ghost"}
            size="sm"
            disabled={pending}
          />
        }
      >
        {pending ? <Spinner data-icon="inline-start" /> : <LogOutIcon />}
        {item.current ? "退出" : "撤销"}
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <LogOutIcon />
          </AlertDialogMedia>
          <AlertDialogTitle>
            {item.current ? "退出当前浏览器？" : "撤销这个会话？"}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {item.current
              ? "确认后当前页面会回到登录入口，这个浏览器需要重新登录。"
              : "确认后，该浏览器需要重新登录。"}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>取消</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={pending}
            onClick={() => void handleConfirm()}
          >
            {pending ? <Spinner data-icon="inline-start" /> : null}
            确认撤销
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function RevokeOthersDialog({
  count,
  pending,
  onConfirm,
}: {
  count: number
  pending: boolean
  onConfirm: () => Promise<void>
}) {
  const [open, setOpen] = React.useState(false)

  async function handleConfirm() {
    try {
      await onConfirm()
      setOpen(false)
    } catch {
      // The parent card owns the visible error state.
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <AlertDialogTrigger
        render={<Button variant="outline" size="sm" disabled={count === 0} />}
      >
        <ShieldXIcon data-icon="inline-start" />
        撤销其他会话
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <ShieldXIcon />
          </AlertDialogMedia>
          <AlertDialogTitle>撤销其他 {count} 个会话？</AlertDialogTitle>
          <AlertDialogDescription>
            当前浏览器会保持登录，其他浏览器需要重新登录。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>取消</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={pending}
            onClick={() => void handleConfirm()}
          >
            {pending ? <Spinner data-icon="inline-start" /> : null}
            全部撤销
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function AccountSecuritySkeleton() {
  return (
    <AccountSecurityFrame>
      <SecurityStatusSkeleton />
      <Skeleton className="h-36 w-full" />
      <Card
        className="rounded-lg border ring-0"
        aria-label="正在加载设备会话"
        aria-busy="true"
      >
        <CardHeader>
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-4 w-80 max-w-full" />
        </CardHeader>
        <CardContent>
          <SessionTableSkeleton />
        </CardContent>
      </Card>
    </AccountSecurityFrame>
  )
}

function SecurityStatusSkeleton() {
  return <Skeleton className="h-40 w-full" aria-busy="true" />
}

function SessionTableSkeleton() {
  return (
    <div className="flex flex-col gap-3 px-4" aria-busy="true">
      <Skeleton className="h-10 w-full" />
      <Skeleton className="h-14 w-full" />
      <Skeleton className="h-14 w-full" />
    </div>
  )
}
