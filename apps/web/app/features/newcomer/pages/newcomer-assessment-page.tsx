import { Link } from "react-router"
import {
  ArrowUpFromLineIcon,
  CircleAlertIcon,
  Clock3Icon,
  LogInIcon,
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
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from "~/components/ui/progress"
import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import { useMyNewcomerAssessment } from "~/features/newcomer/api/newcomer.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"

export function NewcomerAssessmentPage() {
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const canRead = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "newcomer.assessment.read.self"
    )
  )
  const assessment = useMyNewcomerAssessment(
    session.data?.user.id,
    Boolean(capabilities.data && canRead)
  )

  return (
    <PageLayout>
      <PageHeader
        title="新人考核"
        description="查看本次考核期限、有效上传和做种时长进度。"
      />

      {(session.isPending ||
        (session.data && capabilities.isPending) ||
        (session.data && canRead && assessment.isPending)) && (
        <AssessmentSkeleton />
      )}

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
                <EmptyTitle>登录后查看新人考核</EmptyTitle>
                <EmptyDescription>考核进度只对本人可见。</EmptyDescription>
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
          title="暂时无法读取查看权限"
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
          <AlertTitle>当前账户不能查看新人考核</AlertTitle>
          <AlertDescription>如有疑问，请联系站点管理人员。</AlertDescription>
        </Alert>
      )}

      {session.data && canRead && assessment.isError && (
        <ErrorAlert
          title="新人考核暂时无法读取"
          description={requestErrorDescription(
            assessment.error,
            "新人考核请求未能完成，请稍后再试。"
          )}
          onRetry={() => void assessment.refetch()}
        />
      )}

      {session.data &&
        canRead &&
        assessment.data &&
        (assessment.data.assessment ? (
          <AssessmentContent assessment={assessment.data.assessment} />
        ) : (
          <Card>
            <CardContent>
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <ShieldCheckIcon />
                  </EmptyMedia>
                  <EmptyTitle>当前没有新人考核</EmptyTitle>
                  <EmptyDescription>
                    旧站迁移用户与规则启用前已经完成注册的用户不会被追溯考核。
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            </CardContent>
          </Card>
        ))}
    </PageLayout>
  )
}

function AssessmentContent({
  assessment,
}: {
  assessment: NonNullable<
    import("~/features/newcomer/api/newcomer.queries").MyNewcomerAssessmentStatus["assessment"]
  >
}) {
  const uploadProgress = progressPercent(
    assessment.current_credited_upload_bytes,
    assessment.minimum_credited_upload_bytes
  )
  const seedingProgress = progressPercent(
    assessment.current_seeding_active_seconds,
    assessment.minimum_seeding_active_seconds
  )
  const resolved =
    assessment.status === "passed" || assessment.status === "exempted"
  const requiredTaskCount =
    Number(BigInt(assessment.minimum_credited_upload_bytes) > 0n) +
    Number(assessment.minimum_seeding_active_seconds > 0)

  return (
    <>
      <Alert
        variant={
          assessment.status === "download_restricted"
            ? "destructive"
            : "default"
        }
      >
        {resolved ? (
          <ShieldCheckIcon />
        ) : assessment.status === "download_restricted" ? (
          <ShieldXIcon />
        ) : (
          <Clock3Icon />
        )}
        <AlertTitle>{assessmentStatusTitle(assessment.status)}</AlertTitle>
        <AlertDescription>
          {assessmentStatusDescription(assessment.status, requiredTaskCount)}
        </AlertDescription>
      </Alert>

      <Card>
        <CardHeader>
          <CardTitle>本次考核</CardTitle>
          <CardDescription>
            {requiredTaskCount}{" "}
            项任务均达标即可提前通过；受限后继续贡献也会自动恢复。
          </CardDescription>
          <CardAction>
            <Badge variant={resolved ? "secondary" : "outline"}>
              {assessmentStatusLabel(assessment.status)}
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-7">
          {BigInt(assessment.minimum_credited_upload_bytes) > 0n ? (
            <Progress value={uploadProgress}>
              <ProgressLabel className="flex items-center gap-2">
                <ArrowUpFromLineIcon />
                有效上传
              </ProgressLabel>
              <ProgressValue>
                {() =>
                  `${formatBytes(assessment.current_credited_upload_bytes)} / ${formatBytes(assessment.minimum_credited_upload_bytes)}`
                }
              </ProgressValue>
            </Progress>
          ) : null}
          {assessment.minimum_seeding_active_seconds > 0 ? (
            <Progress value={seedingProgress}>
              <ProgressLabel className="flex items-center gap-2">
                <Clock3Icon />
                做种时长
              </ProgressLabel>
              <ProgressValue>
                {() =>
                  `${formatDuration(assessment.current_seeding_active_seconds)} / ${formatDuration(assessment.minimum_seeding_active_seconds)}`
                }
              </ProgressValue>
            </Progress>
          ) : null}
          <dl className="grid gap-4 text-sm sm:grid-cols-3">
            <AssessmentFact
              label="开始时间"
              value={formatDateTime(assessment.started_at)}
            />
            <AssessmentFact
              label="截止时间"
              value={formatDateTime(assessment.deadline_at)}
            />
            <AssessmentFact
              label="最近统计"
              value={formatDateTime(assessment.updated_at)}
            />
          </dl>
        </CardContent>
      </Card>

      <Alert>
        <CircleAlertIcon />
        <AlertTitle>考核不会封禁账号</AlertTitle>
        <AlertDescription>
          未按期完成只会限制新下载，不影响登录、做种和继续完成考核；其他来源的下载限制独立保留。
        </AlertDescription>
      </Alert>
    </>
  )
}

function AssessmentFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium tabular-nums">{value}</dd>
    </div>
  )
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

function AssessmentSkeleton() {
  return (
    <Card aria-busy="true">
      <CardHeader>
        <Skeleton className="h-6 w-32" />
        <Skeleton className="h-4 w-72 max-w-full" />
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
      </CardContent>
    </Card>
  )
}

function progressPercent(current: string | number, target: string | number) {
  const currentValue = BigInt(current)
  const targetValue = BigInt(target)
  if (targetValue === 0n) return 100
  const basisPoints = (currentValue * 10_000n) / targetValue
  return Math.min(100, Number(basisPoints) / 100)
}

function formatDuration(seconds: number) {
  const hours = Math.floor(seconds / 3600)
  const days = Math.floor(hours / 24)
  const remainingHours = hours % 24
  if (days > 0) return `${days} 天 ${remainingHours} 小时`
  if (hours > 0) return `${hours} 小时`
  return `${Math.floor(seconds / 60)} 分钟`
}

function assessmentStatusLabel(status: string) {
  if (status === "download_restricted") return "下载受限"
  if (status === "passed") return "已通过"
  if (status === "exempted") return "已豁免"
  return "进行中"
}

function assessmentStatusTitle(status: string) {
  if (status === "download_restricted") return "考核到期，当前限制新下载"
  if (status === "passed") return "新人考核已经通过"
  if (status === "exempted") return "本次新人考核已由管理员豁免"
  return "新人考核进行中"
}

function assessmentStatusDescription(
  status: string,
  requiredTaskCount: number
) {
  if (status === "download_restricted")
    return `继续完成剩余任务，${requiredTaskCount} 项目标达标后会自动恢复下载。`
  if (status === "passed")
    return `${requiredTaskCount} 项要求已经达到，本考核不会再限制下载。`
  if (status === "exempted") return "本考核已经结束；其他独立下载限制不受影响。"
  return `请在截止时间前完成本期 ${requiredTaskCount} 项新人任务。`
}
