import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { useSearchParams } from "react-router"
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  CircleAlertIcon,
  CrownIcon,
  DownloadIcon,
  MailWarningIcon,
  OctagonXIcon,
  RefreshCwIcon,
  SearchIcon,
  ShieldCheckIcon,
  UsersRoundIcon,
  XIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import { Field, FieldLabel } from "~/components/ui/field"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "~/components/ui/input-group"
import {
  Pagination,
  PaginationContent,
  PaginationItem,
} from "~/components/ui/pagination"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import {
  managedUserListQueryOptions,
  type ManagedUserPage,
} from "~/features/staff/api/user-administration.queries"
import { ManagedUserDetailSheet } from "~/features/staff/components/managed-user-detail-sheet"
import { ManagedUserTable } from "~/features/staff/components/managed-user-table"
import { AccountAccessAppealQueue } from "~/features/staff/components/account-access-appeal-queue"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import {
  type ManagedUserFilters,
  type ManagedUserStatusFilter,
  managedUserSearchParams,
  parseManagedUserFilters,
} from "~/features/staff/model/user-administration"

export function StaffUsersPage() {
  return (
    <StaffAccessGate
      requiredAction="user.account.read"
      pageHeader={{
        title: "用户管理",
        description: "统一查看用户角色、真实流量、魔力值、等级与账户状态。",
      }}
    >
      {({ session, capabilities }) => (
        <UsersContent
          csrfToken={session.csrf_token}
          currentStaffUserId={session.user.id}
          canRestrict={hasCapability(capabilities, "user.account.restrict")}
          canRevoke={hasCapability(
            capabilities,
            "user.account.restriction.revoke"
          )}
          canDownloadRestrict={hasCapability(
            capabilities,
            "user.downloadrestriction.restrict"
          )}
          canDownloadRevoke={hasCapability(
            capabilities,
            "user.downloadrestriction.revoke"
          )}
          canManageVIP={hasCapability(capabilities, "user.vip.manage")}
          canReadAppeals={hasCapability(
            capabilities,
            "user.account.appeal.read"
          )}
          canDecideAppeals={hasCapability(
            capabilities,
            "user.account.appeal.decide"
          )}
        />
      )}
    </StaffAccessGate>
  )
}

function UsersContent({
  csrfToken,
  currentStaffUserId,
  canRestrict,
  canRevoke,
  canDownloadRestrict,
  canDownloadRevoke,
  canManageVIP,
  canReadAppeals,
  canDecideAppeals,
}: {
  csrfToken: string
  currentStaffUserId: string
  canRestrict: boolean
  canRevoke: boolean
  canDownloadRestrict: boolean
  canDownloadRevoke: boolean
  canManageVIP: boolean
  canReadAppeals: boolean
  canDecideAppeals: boolean
}) {
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = React.useMemo(
    () => parseManagedUserFilters(searchParams),
    [searchParams]
  )
  const users = useQuery(managedUserListQueryOptions(filters))
  const [queryDraft, setQueryDraft] = React.useState(filters.query)
  const [selectedUserId, setSelectedUserId] = React.useState<string>()

  React.useEffect(() => {
    setQueryDraft(filters.query)
  }, [filters.query])

  React.useEffect(() => {
    if (!users.data) {
      return
    }
    const lastPage = Math.ceil(users.data.total / users.data.page_size)
    if (lastPage > 0 && filters.page > lastPage) {
      setSearchParams(managedUserSearchParams({ ...filters, page: lastPage }), {
        replace: true,
      })
    }
  }, [filters, setSearchParams, users.data])

  function updateFilters(update: Partial<ManagedUserFilters>) {
    setSearchParams(
      managedUserSearchParams({
        ...filters,
        ...update,
      })
    )
  }

  function handleAccountSearch(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const query = queryDraft.trim()
    updateFilters({ query, page: 1 })
  }

  if (users.isPending) {
    return <UsersSkeleton />
  }
  if (users.isError || !users.data) {
    return (
      <UsersFrame>
        <UsersHeader />
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>账户列表暂时无法读取</AlertTitle>
          <AlertDescription>
            暂时无法取得账户列表，请检查后台登录状态后重试。
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void users.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      </UsersFrame>
    )
  }

  const totalPages = Math.ceil(users.data.total / users.data.page_size)
  return (
    <UsersFrame>
      {!canRestrict &&
      !canRevoke &&
      !canDownloadRestrict &&
      !canDownloadRevoke &&
      !canManageVIP ? (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>当前权限仅可查看</AlertTitle>
          <AlertDescription>
            可以查看账户状态和当前限制，但不能创建或解除访问限制。
          </AlertDescription>
        </Alert>
      ) : null}

      <UserDirectorySummaryCards summary={users.data.summary} />

      {canReadAppeals ? (
        <AccountAccessAppealQueue
          csrfToken={csrfToken}
          canDecide={canDecideAppeals}
        />
      ) : null}

      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-2">
          <div className="flex flex-col gap-3 lg:grid lg:min-h-[52px] lg:grid-cols-[minmax(203px,1fr)_auto] lg:items-start lg:gap-3">
            <CardTitle className="flex min-h-12 items-start gap-2 text-2xl leading-none lg:pt-3.5">
              <UsersRoundIcon className="size-5" aria-hidden="true" />
              <h1>用户管理</h1>
              <span className="text-sm font-normal text-muted-foreground">
                ({users.data.total.toLocaleString("zh-CN")} 条记录)
              </span>
            </CardTitle>
            <div className="grid min-w-0 grid-cols-1 items-center gap-2 sm:grid-cols-[minmax(220px,320px)_142px_32px] lg:justify-self-end">
              <form className="w-full min-w-0" onSubmit={handleAccountSearch}>
                <Field>
                  <FieldLabel htmlFor="managed-user-search" className="sr-only">
                    搜索用户 ID、UUID、用户名或显示名
                  </FieldLabel>
                  <InputGroup className="h-8">
                    <InputGroupInput
                      id="managed-user-search"
                      value={queryDraft}
                      maxLength={64}
                      onChange={(event) => setQueryDraft(event.target.value)}
                      placeholder="搜索 ID / UUID / 用户名..."
                      className="h-8"
                    />
                    {queryDraft ? (
                      <InputGroupAddon align="inline-end">
                        <InputGroupButton
                          size="icon-xs"
                          aria-label="清空账户搜索"
                          onClick={() => setQueryDraft("")}
                        >
                          <XIcon />
                        </InputGroupButton>
                      </InputGroupAddon>
                    ) : null}
                    <InputGroupAddon align="inline-end">
                      <InputGroupButton
                        type="submit"
                        size="icon-xs"
                        aria-label="搜索账户"
                      >
                        <SearchIcon />
                      </InputGroupButton>
                    </InputGroupAddon>
                  </InputGroup>
                </Field>
              </form>

              <div className="flex items-center gap-2 sm:contents">
                <AccountStatusFilter
                  value={filters.status}
                  onChange={(status) => updateFilters({ status, page: 1 })}
                />
                <Button
                  variant="outline"
                  size="icon-sm"
                  className="size-8"
                  onClick={() => void users.refetch()}
                  disabled={users.isFetching}
                  aria-label={
                    users.isFetching ? "正在刷新用户列表" : "刷新用户列表"
                  }
                >
                  <RefreshCwIcon />
                </Button>
              </div>
            </div>
          </div>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 p-6 pt-0">
          <ManagedUserTable
            users={users.data.items}
            hasFilters={Boolean(filters.query) || filters.status !== "all"}
            onSelect={setSelectedUserId}
          />

          {totalPages > 1 ? (
            <ManagedUserPagination
              filters={filters}
              total={users.data.total}
              totalPages={totalPages}
              onPageChange={(page) => updateFilters({ page })}
            />
          ) : null}
        </CardContent>
      </Card>

      <ManagedUserDetailSheet
        userId={selectedUserId}
        csrfToken={csrfToken}
        currentStaffUserId={currentStaffUserId}
        canRestrict={canRestrict}
        canRevoke={canRevoke}
        canDownloadRestrict={canDownloadRestrict}
        canDownloadRevoke={canDownloadRevoke}
        canManageVIP={canManageVIP}
        onOpenChange={(open) => {
          if (!open) {
            setSelectedUserId(undefined)
          }
        }}
      />
    </UsersFrame>
  )
}

function AccountStatusFilter({
  value,
  onChange,
}: {
  value: ManagedUserStatusFilter
  onChange: (value: ManagedUserStatusFilter) => void
}) {
  return (
    <Select
      value={value}
      onValueChange={(next) => {
        if (next) {
          onChange(next as ManagedUserStatusFilter)
        }
      }}
    >
      <SelectTrigger
        size="xs"
        className="w-[142px]"
        aria-label="按账户状态筛选"
      >
        <SelectValue>{accountStatusFilterLabel(value)}</SelectValue>
      </SelectTrigger>
      <SelectContent align="end">
        <SelectGroup>
          <SelectItem value="all">全部用户</SelectItem>
          <SelectItem value="active">正常用户</SelectItem>
          <SelectItem value="pending">待激活</SelectItem>
          <SelectItem value="banned">已封禁</SelectItem>
          <SelectItem value="vip">VIP 用户</SelectItem>
          <SelectItem value="download_restricted">下载受限</SelectItem>
          <SelectItem value="unverified">邮箱未验证</SelectItem>
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

function accountStatusFilterLabel(value: ManagedUserStatusFilter) {
  switch (value) {
    case "all":
      return "全部用户"
    case "active":
      return "正常用户"
    case "pending":
      return "待激活"
    case "banned":
      return "已封禁"
    case "vip":
      return "VIP 用户"
    case "download_restricted":
      return "下载受限"
    case "unverified":
      return "邮箱未验证"
  }
}

function UserDirectorySummaryCards({
  summary,
}: {
  summary: ManagedUserPage["summary"]
}) {
  const cards = [
    {
      label: "总用户",
      value: summary.total,
      icon: UsersRoundIcon,
      className: "text-primary",
    },
    {
      label: "VIP",
      value: summary.vip,
      icon: CrownIcon,
      className: "text-warning-foreground",
    },
    {
      label: "已封禁",
      value: summary.banned,
      icon: OctagonXIcon,
      className: "text-destructive",
    },
    {
      label: "下载受限",
      value: summary.download_restricted,
      icon: DownloadIcon,
      className: "text-warning-foreground",
    },
    {
      label: "未验证",
      value: summary.unverified,
      icon: MailWarningIcon,
      className: "text-muted-foreground",
    },
  ]
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-5">
      {cards.map((card) => {
        const Icon = card.icon
        return (
          <Card key={card.label} size="sm" className="gap-1 py-3">
            <CardContent className="flex items-center justify-between gap-3 px-4">
              <div>
                <div
                  className={`text-xl font-semibold tabular-nums ${card.className}`}
                >
                  {card.value.toLocaleString("zh-CN")}
                </div>
                <div className="text-xs text-muted-foreground">
                  {card.label}
                </div>
              </div>
              <Icon className={`size-5 ${card.className}`} aria-hidden="true" />
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}

function ManagedUserPagination({
  filters,
  total,
  totalPages,
  onPageChange,
}: {
  filters: ManagedUserFilters
  total: number
  totalPages: number
  onPageChange: (page: number) => void
}) {
  return (
    <Pagination className="justify-between pt-1" aria-label="用户列表分页">
      <span className="text-sm text-muted-foreground">
        共 {total.toLocaleString("zh-CN")} 条记录
      </span>
      <PaginationContent>
        <PaginationItem>
          <Button
            variant="outline"
            size="sm"
            disabled={filters.page === 1}
            onClick={() => onPageChange(filters.page - 1)}
          >
            <ChevronLeftIcon data-icon="inline-start" />
            上一页
          </Button>
        </PaginationItem>
        <PaginationItem>
          <span className="px-3 text-sm text-muted-foreground tabular-nums">
            {filters.page} / {totalPages}
          </span>
        </PaginationItem>
        <PaginationItem>
          <Button
            variant="outline"
            size="sm"
            disabled={filters.page === totalPages}
            onClick={() => onPageChange(filters.page + 1)}
          >
            下一页
            <ChevronRightIcon data-icon="inline-end" />
          </Button>
        </PaginationItem>
      </PaginationContent>
    </Pagination>
  )
}

function UsersFrame({ children }: { children: React.ReactNode }) {
  return <StaffPageFrame className="gap-4">{children}</StaffPageFrame>
}

function UsersHeader() {
  return <h1 className="font-heading text-xl font-semibold">用户管理</h1>
}

function UsersSkeleton() {
  return (
    <UsersFrame>
      <div
        className="flex flex-col gap-2"
        aria-label="正在加载账户目录"
        aria-busy="true"
      >
        <Skeleton className="h-9 w-48" />
        <Skeleton className="h-4 w-full max-w-xl" />
      </div>
      <Skeleton className="h-16 w-full rounded-xl" />
      <Skeleton className="h-[34rem] w-full rounded-xl" />
    </UsersFrame>
  )
}
