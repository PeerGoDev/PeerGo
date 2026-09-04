import { Link } from "react-router"
import {
  ChevronDownIcon,
  CircleAlertIcon,
  DatabaseIcon,
  LogInIcon,
  MessageCircleIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  UserRoundIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
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
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "~/components/ui/collapsible"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { AccountSettingsLayout } from "~/features/auth/components/account-settings-layout"
import {
  type CapabilityList,
  useCapabilities,
} from "~/features/authz/api/capabilities.queries"
import { formatDateTime } from "~/shared/formatters/date-time"
import { requestErrorDescription } from "~/shared/api/problem"

type Capability = CapabilityList["items"][number]

const capabilityGroups = [
  {
    key: "account",
    label: "账户与安全",
    description: "个人资料、邮箱、登录与安全设置",
    icon: ShieldCheckIcon,
    matches: (action: string) =>
      action.startsWith("account.") ||
      action.startsWith("session.") ||
      action.startsWith("authz."),
  },
  {
    key: "torrent",
    label: "种子与流量",
    description: "收藏、下载、上传、审核进度与流量记录",
    icon: DatabaseIcon,
    matches: (action: string) =>
      action.startsWith("torrent.") ||
      action.startsWith("traffic.") ||
      action.startsWith("hnr."),
  },
  {
    key: "community",
    label: "社区互动",
    description: "动态、评论、公告、成员资料与站内消息",
    icon: MessageCircleIcon,
    matches: (action: string) =>
      action.startsWith("social.") ||
      action.startsWith("comment.") ||
      action.startsWith("announcement.") ||
      action.startsWith("notification.") ||
      action.startsWith("user.profile."),
  },
  {
    key: "staff",
    label: "管理后台",
    description: "仅在获得站点管理职责时可用",
    icon: UserRoundIcon,
    matches: (action: string) => action.startsWith("staff."),
  },
] as const

const friendlyCapabilityNames: Readonly<Record<string, string>> = {
  "account.email.verify.self": "邮箱验证",
  "account.profile.update.self": "修改个人资料",
  "account.totp.manage.self": "两步验证与恢复码",
  "announcement.comment.create.self": "公告评论",
  "authz.capability.read.self": "查看功能权限",
  "comment.delete.self": "删除自己的评论",
  "comment.report.create.self": "举报不当评论",
  "comment.update.self": "编辑自己的评论",
  "hnr.read.self": "查看 H&R 记录",
  "user.downloadrestriction.read.self": "查看下载限制",
  "user.downloadrestriction.appeal.create.self": "提交下载限制申诉",
  "notification.archive.self": "整理站内消息",
  "notification.feedback.create.self": "联系管理员",
  "notification.read.self": "查看站内消息",
  "notification.read.state.write.self": "管理消息已读状态",
  "session.read.self": "查看登录状态",
  "session.revoke.self": "退出当前登录",
  "social.post.comment.create.self": "动态评论",
  "social.post.create.self": "发布动态",
  "social.post.delete.self": "删除自己的动态",
  "social.post.read": "浏览动态圈",
  "social.post.update.self": "编辑自己的动态",
  "staff.credential.enroll.self": "登记后台安全凭据",
  "staff.session.create.self": "进入管理后台",
  "torrent.bookmark.read.self": "查看种子收藏",
  "torrent.bookmark.write.self": "添加或取消收藏",
  "torrent.comment.create.self": "种子评论",
  "torrent.download": "下载种子文件",
  "torrent.submission.read.self": "查看上传审核进度",
  "torrent.submission.resubmit.self": "整改并重新送审",
  "torrent.submit": "上传种子",
  "traffic.read.self": "查看流量记录",
  "user.profile.read.member": "查看成员资料",
}

export function PermissionsPage() {
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)

  return (
    <AccountSettingsLayout
      active="permissions"
      title="我的权限"
      description="查看当前账户可使用的站点功能及有效期。"
    >
      {session.isPending && <PermissionsSkeleton />}

      {session.isError && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>会话状态暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              session.error,
              "会话请求未能完成，请稍后刷新页面。"
            )}
          </AlertDescription>
        </Alert>
      )}

      {!session.isPending && !session.isError && !session.data && (
        <Card className="gap-0 py-0">
          <CardHeader className="border-b py-4">
            <CardTitle>需要登录</CardTitle>
            <CardDescription>
              登录后可查看当前账户能够使用的功能。
            </CardDescription>
          </CardHeader>
          <CardContent className="px-0">
            <Alert>
              <ShieldCheckIcon />
              <AlertTitle>没有可用会话</AlertTitle>
              <AlertDescription>
                登录后只会显示当前账户的有效权限。
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
      )}

      {session.data && capabilities.isPending && <PermissionsSkeleton />}

      {session.data && capabilities.isError && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>权限暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              capabilities.error,
              "权限请求未能完成，请稍后再试。"
            )}
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void capabilities.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      )}

      {session.data && capabilities.data && (
        <CapabilityOverview capabilities={capabilities.data} />
      )}
    </AccountSettingsLayout>
  )
}

function CapabilityOverview({
  capabilities,
}: {
  capabilities: CapabilityList
}) {
  const grouped = capabilityGroups
    .map((group) => ({
      ...group,
      items: capabilities.items.filter((capability) =>
        group.matches(capability.action)
      ),
    }))
    .filter((group) => group.items.length > 0)
  const groupedActions = new Set(
    grouped.flatMap((group) => group.items.map((item) => item.action))
  )
  const otherItems = capabilities.items.filter(
    (capability) => !groupedActions.has(capability.action)
  )

  return (
    <Collapsible>
      <Card className="min-w-0 gap-0 py-0">
        <CardHeader className="px-6 pt-6 pb-5">
          <CardTitle>
            <h2 className="text-2xl leading-none font-semibold">可用功能</h2>
          </CardTitle>
          <CardDescription>当前账户可以正常使用以下站点功能。</CardDescription>
          <CardAction>
            <Badge variant="outline">{capabilities.items.length} 项</Badge>
          </CardAction>
        </CardHeader>
        <CardContent className="px-6 pb-5">
          {capabilities.items.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ShieldCheckIcon />
                </EmptyMedia>
                <EmptyTitle>暂无可用功能</EmptyTitle>
                <EmptyDescription>
                  当前账户暂时没有额外的站点权限。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="flex flex-col divide-y" aria-label="功能概览">
              {grouped.map((group) => (
                <CapabilityGroupRow key={group.key} group={group} />
              ))}
              {otherItems.length > 0 ? (
                <CapabilityGroupRow
                  group={{
                    label: "其他功能",
                    description: "由站点为当前账户开放的其他功能",
                    icon: ShieldCheckIcon,
                    items: otherItems,
                  }}
                />
              ) : null}
            </div>
          )}
        </CardContent>
        {capabilities.items.length > 0 ? (
          <CardFooter className="justify-center border-t bg-transparent px-6 py-3">
            <CollapsibleTrigger
              render={<Button type="button" variant="ghost" size="sm" />}
            >
              查看全部 {capabilities.items.length} 项功能
              <ChevronDownIcon data-icon="inline-end" />
            </CollapsibleTrigger>
          </CardFooter>
        ) : null}
        <CollapsibleContent>
          <div className="border-t px-6 py-5">
            <div className="hidden sm:block">
              <CapabilityTable items={capabilities.items} />
            </div>
            <CapabilityMobileList items={capabilities.items} />
          </div>
        </CollapsibleContent>
      </Card>
    </Collapsible>
  )
}

function CapabilityGroupRow({
  group,
}: {
  group: {
    label: string
    description: string
    icon: typeof ShieldCheckIcon
    items: Capability[]
  }
}) {
  return (
    <div className="flex items-center gap-3 py-4 first:pt-0 last:pb-0">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <group.icon className="size-4" />
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="font-medium">{group.label}</span>
        <span className="text-xs leading-5 text-muted-foreground">
          {group.description}
        </span>
      </span>
      <Badge variant="secondary">{group.items.length} 项</Badge>
    </div>
  )
}

function CapabilityTable({ items }: { items: Capability[] }) {
  return (
    <Table aria-label="全部可用功能" containerClassName="px-3">
      <TableHeader className="bg-secondary/70">
        <TableRow>
          <TableHead className="pl-4 text-muted-foreground">功能</TableHead>
          <TableHead className="text-muted-foreground">范围</TableHead>
          <TableHead className="pr-4 text-right text-muted-foreground">
            有效至
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((capability) => (
          <TableRow
            key={`${capability.action}:${capability.scope.type}:${capability.scope.id}`}
            className="h-14 hover:bg-accent/70"
          >
            <TableCell className="pl-4 font-medium">
              {friendlyCapabilityName(capability)}
            </TableCell>
            <TableCell>
              <Badge variant="secondary">
                {scopeLabel(capability.scope.type)}
              </Badge>
            </TableCell>
            <TableCell className="pr-4 text-right">
              <time dateTime={capability.expires_at}>
                {formatDateTime(capability.expires_at)}
              </time>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function CapabilityMobileList({ items }: { items: Capability[] }) {
  return (
    <div
      aria-label="移动端全部可用功能"
      className="flex flex-col divide-y sm:hidden"
    >
      {items.map((capability) => (
        <article
          key={`${capability.action}:${capability.scope.type}:${capability.scope.id}`}
          className="flex flex-col gap-2 py-4 first:pt-0 last:pb-0"
        >
          <div className="flex items-start justify-between gap-3">
            <h3 className="font-medium">
              {friendlyCapabilityName(capability)}
            </h3>
            <Badge variant="secondary">
              {scopeLabel(capability.scope.type)}
            </Badge>
          </div>
          <p className="text-xs text-muted-foreground">
            有效至：
            <time dateTime={capability.expires_at}>
              {formatDateTime(capability.expires_at)}
            </time>
          </p>
        </article>
      ))}
    </div>
  )
}

function friendlyCapabilityName(capability: Capability) {
  return friendlyCapabilityNames[capability.action] ?? capability.description
}

function PermissionsSkeleton() {
  return (
    <Card aria-label="正在加载权限" aria-busy="true">
      <CardHeader>
        <CardTitle>
          <Skeleton className="h-5 w-32" />
        </CardTitle>
        <CardDescription>
          <Skeleton className="h-4 w-48" />
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-14 w-full" />
        <Skeleton className="h-14 w-full" />
      </CardContent>
      <CardFooter>
        <Skeleton className="h-4 w-64 max-w-full" />
      </CardFooter>
    </Card>
  )
}

function scopeLabel(scope: "site" | "category") {
  return scope === "site" ? "全站" : "分类"
}
