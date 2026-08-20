import type { ComponentType, ReactNode, SVGProps } from "react"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  LockKeyholeIcon,
  LogInIcon,
  RefreshCwIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type StaffSession,
  useStaffCapabilities,
  useStaffSession,
} from "~/features/staff/api/staff-session.mutations"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffPageHeader } from "~/features/staff/components/staff-page-header"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"
import { cn } from "~/lib/utils"

type CapabilityList = components["schemas"]["CapabilityList"]
type CapabilityAction = components["schemas"]["CapabilityAction"]

/**
 * Static page identity that may be shown before staff elevation.
 *
 * Keep this limited to labels already present in the staff navigation. Query
 * results, counts, actions and permission-derived details must stay inside the
 * authenticated `children` branch below.
 */
export type StaffGatePageHeader = {
  title: string
  description?: string
  meta?: ReactNode
  icon?: ComponentType<SVGProps<SVGSVGElement>>
  variant?: "compact" | "hero"
  frameClassName?: string
  contentClassName?: string
  descriptionClassName?: string
}

export function StaffAccessGate({
  requiredAction,
  layout = "page",
  pageHeader,
  children,
}: {
  requiredAction?: CapabilityAction
  layout?: "page" | "embedded"
  pageHeader?: StaffGatePageHeader
  children: (context: {
    session: StaffSession
    capabilities: CapabilityList
  }) => ReactNode
}) {
  const Frame = layout === "embedded" ? EmbeddedGateFrame : GateFrame
  const webSession = useWebSession()
  const webCapabilities = useCapabilities(webSession.data?.user.id)
  const staffSession = useStaffSession()
  const staffCapabilities = useStaffCapabilities(staffSession.data?.user.id)
  const frameProps = layout === "page" ? { pageHeader } : undefined

  if (webSession.isPending) {
    return <StaffGateSkeleton layout={layout} pageHeader={pageHeader} />
  }
  if (webSession.isError) {
    return (
      <Frame {...frameProps}>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>普通会话状态暂时无法读取</AlertTitle>
          <AlertDescription>请刷新页面后重试。</AlertDescription>
        </Alert>
      </Frame>
    )
  }
  if (!webSession.data) {
    return (
      <Frame {...frameProps}>
        <Card>
          <CardHeader>
            <CardTitle>请先登录站点账户</CardTitle>
            <CardDescription>管理后台需要确认当前登录账号。</CardDescription>
          </CardHeader>
          <CardContent>
            <Alert>
              <LockKeyholeIcon />
              <AlertTitle>后台使用站点账号登录</AlertTitle>
              <AlertDescription>
                登录后，具有站点管理员角色的账号可直接进入后台。
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
      </Frame>
    )
  }

  if (staffSession.data) {
    if (staffCapabilities.isPending) {
      return <StaffGateSkeleton layout={layout} pageHeader={pageHeader} />
    }
    if (staffCapabilities.isError || !staffCapabilities.data) {
      return (
        <Frame {...frameProps}>
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>后台权限暂时无法读取</AlertTitle>
            <AlertDescription>
              后台登录仍然有效，请稍后刷新权限信息。
            </AlertDescription>
          </Alert>
        </Frame>
      )
    }
    if (
      requiredAction &&
      !hasCapability(staffCapabilities.data, requiredAction)
    ) {
      return (
        <Frame {...frameProps}>
          <Card>
            <CardHeader>
              <CardTitle>当前权限不能访问此页面</CardTitle>
              <CardDescription>
                当前管理员权限不包含此页面所需能力。
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Alert>
                <LockKeyholeIcon />
                <AlertTitle>缺少页面访问权限</AlertTitle>
                <AlertDescription>
                  如需调整，请通过权限与任期管理流程申请。
                </AlertDescription>
              </Alert>
            </CardContent>
          </Card>
        </Frame>
      )
    }
    return (
      <>
        {children({
          session: staffSession.data,
          capabilities: staffCapabilities.data,
        })}
      </>
    )
  }

  if (webCapabilities.isPending || staffSession.isPending) {
    return <StaffGateSkeleton layout={layout} pageHeader={pageHeader} />
  }
  if (webCapabilities.isError) {
    return (
      <Frame {...frameProps}>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>后台权限暂时无法读取</AlertTitle>
          <AlertDescription>
            无法确认当前账号是否允许发起后台验证，请稍后重试。
          </AlertDescription>
        </Alert>
      </Frame>
    )
  }

  const canAccessAdmin = hasCapability(
    webCapabilities.data,
    "staff.session.create.self"
  )
  if (!canAccessAdmin) {
    return (
      <Frame {...frameProps}>
        <Card>
          <CardHeader>
            <CardTitle>当前账号不是管理员</CardTitle>
            <CardDescription>普通成员账号不能访问管理后台。</CardDescription>
          </CardHeader>
          <CardContent>
            <Alert>
              <LockKeyholeIcon />
              <AlertTitle>没有站点管理权限</AlertTitle>
              <AlertDescription>
                请由服务器维护者使用管理员命令授予 site_admin 角色。
              </AlertDescription>
            </Alert>
          </CardContent>
        </Card>
      </Frame>
    )
  }

  return (
    <Frame {...frameProps}>
      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle>
            {pageHeader ? (
              <h2 className="text-base leading-snug font-medium">
                正在进入管理后台
              </h2>
            ) : (
              <h1 className="text-base leading-snug font-medium">
                正在进入管理后台
              </h1>
            )}
          </CardTitle>
          <CardDescription>
            当前账号已有管理员权限，但会话状态尚未同步。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Alert>
            <RefreshCwIcon />
            <AlertTitle>请重新读取管理员状态</AlertTitle>
            <AlertDescription>
              无需登记通行密钥；刷新成功后会直接显示后台内容。
            </AlertDescription>
          </Alert>
        </CardContent>
        <CardFooter className="justify-end">
          <Button
            onClick={() => void staffSession.refetch()}
            disabled={staffSession.isFetching}
          >
            {staffSession.isFetching ? (
              <Spinner />
            ) : (
              <RefreshCwIcon data-icon="inline-start" />
            )}
            {staffSession.isFetching ? "读取中…" : "重新读取"}
          </Button>
        </CardFooter>
      </Card>
    </Frame>
  )
}

function GateFrame({
  children,
  pageHeader,
}: {
  children: ReactNode
  pageHeader?: StaffGatePageHeader
}) {
  if (pageHeader) {
    return (
      <StaffPageFrame className={pageHeader.frameClassName}>
        <div
          className={cn(
            "flex min-h-0 w-full flex-1 flex-col gap-6",
            pageHeader.contentClassName
          )}
        >
          <StaffPageHeader
            title={pageHeader.title}
            description={pageHeader.description}
            meta={pageHeader.meta}
            icon={pageHeader.icon}
            variant={pageHeader.variant}
            descriptionClassName={pageHeader.descriptionClassName}
          />
          <section className="flex min-h-[320px] w-full flex-1 flex-col items-center justify-center gap-6 [&>*]:w-full [&>*]:max-w-xl">
            {children}
          </section>
        </div>
      </StaffPageFrame>
    )
  }

  return (
    <main className="mx-auto flex w-full max-w-[1248px] flex-1 flex-col items-center justify-center gap-6 p-4 lg:p-6 [&>*]:w-full">
      {children}
    </main>
  )
}

function EmbeddedGateFrame({ children }: { children: ReactNode }) {
  return (
    <section className="flex min-h-[320px] w-full flex-col items-center justify-center gap-6 py-6 [&>*]:w-full">
      {children}
    </section>
  )
}

function StaffGateSkeleton({
  layout = "page",
  pageHeader,
}: {
  layout?: "page" | "embedded"
  pageHeader?: StaffGatePageHeader
}) {
  const Frame = layout === "embedded" ? EmbeddedGateFrame : GateFrame
  const frameProps = layout === "page" ? { pageHeader } : undefined

  return (
    <Frame {...frameProps}>
      <Card
        aria-label="正在检查后台访问状态"
        aria-busy="true"
        className="max-w-xl"
      >
        <CardHeader>
          <CardTitle>
            <Skeleton className="h-6 w-36" />
          </CardTitle>
          <CardDescription>
            <Skeleton className="h-4 w-72 max-w-full" />
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-10 w-full" />
        </CardContent>
      </Card>
    </Frame>
  )
}
