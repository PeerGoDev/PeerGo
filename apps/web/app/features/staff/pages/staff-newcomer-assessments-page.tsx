import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  GraduationCapIcon,
  RefreshCwIcon,
  SearchIcon,
  ShieldCheckIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Field, FieldLabel } from "~/components/ui/field"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "~/components/ui/input-group"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import {
  type NewcomerAssessment,
  type NewcomerAssessmentFilter,
  newcomerAssessmentListQueryOptions,
} from "~/features/staff/api/newcomer-administration.queries"
import { NewcomerAssessmentExemptionDialog } from "~/features/staff/components/newcomer-assessment-exemption-dialog"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

const filters = [
  { value: "active", label: "进行中" },
  { value: "download_restricted", label: "下载受限" },
  { value: "resolved", label: "已结束" },
  { value: "all", label: "全部" },
] satisfies Array<{ value: NewcomerAssessmentFilter; label: string }>

export function StaffNewcomerAssessmentsPage() {
  return (
    <StaffAccessGate
      requiredAction="newcomer.assessment.read"
      pageHeader={{
        title: "新人考核",
        description: "查看新注册用户的上传、做种进度与下载限制状态。",
      }}
    >
      {({ session, capabilities }) => (
        <AssessmentContent
          csrfToken={session.csrf_token}
          canExempt={hasCapability(capabilities, "newcomer.assessment.exempt")}
        />
      )}
    </StaffAccessGate>
  )
}

function AssessmentContent({
  csrfToken,
  canExempt,
}: {
  csrfToken: string
  canExempt: boolean
}) {
  const [filter, setFilter] = React.useState<NewcomerAssessmentFilter>("active")
  const [queryDraft, setQueryDraft] = React.useState("")
  const [query, setQuery] = React.useState("")
  const [selected, setSelected] = React.useState<NewcomerAssessment | null>(
    null
  )
  const assessments = useQuery(
    newcomerAssessmentListQueryOptions(filter, query)
  )

  if (assessments.isPending) {
    return (
      <StaffPageFrame>
        <Skeleton className="h-36 w-full" />
        <Skeleton className="h-96 w-full" />
      </StaffPageFrame>
    )
  }
  if (assessments.isError || !assessments.data) {
    return (
      <StaffPageFrame>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>新人考核暂时无法读取</AlertTitle>
          <AlertDescription>
            请检查后台登录状态或 Core policy worker 后重试。
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void assessments.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      </StaffPageFrame>
    )
  }

  return (
    <StaffPageFrame>
      {!canExempt ? (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>当前权限仅可查看</AlertTitle>
          <AlertDescription>
            可以查看考核进度，但不能人工豁免考核。
          </AlertDescription>
        </Alert>
      ) : null}
      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-3">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <CardTitle className="flex items-center gap-2 text-xl">
              <GraduationCapIcon />
              新人考核
              <span className="text-sm font-normal text-muted-foreground">
                ({assessments.data.total} 条记录)
              </span>
            </CardTitle>
            <div className="grid gap-2 sm:grid-cols-[minmax(240px,320px)_140px_32px]">
              <form
                onSubmit={(event) => {
                  event.preventDefault()
                  setQuery(queryDraft.trim())
                }}
              >
                <Field>
                  <FieldLabel htmlFor="newcomer-search" className="sr-only">
                    搜索用户名或显示名
                  </FieldLabel>
                  <InputGroup className="h-8">
                    <InputGroupInput
                      id="newcomer-search"
                      value={queryDraft}
                      maxLength={120}
                      placeholder="搜索用户名或显示名..."
                      onChange={(event) => setQueryDraft(event.target.value)}
                    />
                    <InputGroupAddon align="inline-end">
                      <InputGroupButton type="submit" size="icon-xs">
                        <SearchIcon />
                        <span className="sr-only">搜索</span>
                      </InputGroupButton>
                    </InputGroupAddon>
                  </InputGroup>
                </Field>
              </form>
              <Select
                items={filters}
                value={filter}
                onValueChange={(value) =>
                  setFilter(value as NewcomerAssessmentFilter)
                }
              >
                <SelectTrigger className="h-8 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {filters.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Button
                variant="outline"
                size="icon-sm"
                aria-label="刷新新人考核"
                disabled={assessments.isFetching}
                onClick={() => void assessments.refetch()}
              >
                <RefreshCwIcon />
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent className="px-6 pb-6">
          {assessments.data.items.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <GraduationCapIcon />
                </EmptyMedia>
                <EmptyTitle>没有匹配的新人考核</EmptyTitle>
                <EmptyDescription>
                  默认规则尚未启用，或当前筛选下没有记录。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="overflow-x-auto rounded-lg border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>用户</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>有效上传</TableHead>
                    <TableHead>做种时长</TableHead>
                    <TableHead>截止时间</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {assessments.data.items.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell>
                        <div className="flex flex-col gap-0.5">
                          <span className="font-medium">{item.username}</span>
                          <span className="text-xs text-muted-foreground">
                            ID {item.user_numeric_id} · {item.display_name}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant={statusVariant(item.status)}>
                          {statusLabel(item.status)}
                        </Badge>
                      </TableCell>
                      <TableCell className="tabular-nums">
                        {BigInt(item.minimum_credited_upload_bytes) > 0n ? (
                          <>
                            {formatBytes(item.current_credited_upload_bytes)} /{" "}
                            {formatBytes(item.minimum_credited_upload_bytes)}
                          </>
                        ) : (
                          <span className="text-muted-foreground">不要求</span>
                        )}
                      </TableCell>
                      <TableCell className="tabular-nums">
                        {item.minimum_seeding_active_seconds > 0 ? (
                          <>
                            {formatHours(item.current_seeding_active_seconds)} /{" "}
                            {formatHours(item.minimum_seeding_active_seconds)}
                          </>
                        ) : (
                          <span className="text-muted-foreground">不要求</span>
                        )}
                      </TableCell>
                      <TableCell className="text-muted-foreground tabular-nums">
                        {formatCompactDateTime(item.deadline_at)}
                      </TableCell>
                      <TableCell className="text-right">
                        {canExempt &&
                        (item.status === "active" ||
                          item.status === "download_restricted") ? (
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => setSelected(item)}
                          >
                            豁免
                          </Button>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
      <NewcomerAssessmentExemptionDialog
        assessment={selected}
        csrfToken={csrfToken}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      />
    </StaffPageFrame>
  )
}

function statusLabel(status: string) {
  if (status === "download_restricted") return "下载受限"
  if (status === "passed") return "已通过"
  if (status === "exempted") return "已豁免"
  return "进行中"
}

function statusVariant(
  status: string
): "default" | "secondary" | "destructive" | "outline" {
  if (status === "download_restricted") return "destructive"
  if (status === "passed" || status === "exempted") return "secondary"
  return "outline"
}

function formatHours(seconds: number) {
  return `${Math.floor(seconds / 3600).toLocaleString("zh-CN")} 小时`
}
