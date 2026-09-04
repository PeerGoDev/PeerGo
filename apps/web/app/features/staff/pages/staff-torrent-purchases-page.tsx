import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useSearchParams } from "react-router"
import {
  CheckCircle2Icon,
  CircleAlertIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  SearchIcon,
  ShoppingBagIcon,
  XIcon,
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
import { Separator } from "~/components/ui/separator"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import {
  type ManagedTorrentPurchase,
  managedTorrentPurchaseListQueryOptions,
} from "~/features/staff/api/torrent-purchase-administration.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { TorrentPurchaseRefundDialog } from "~/features/staff/components/torrent-purchase-refund-dialog"
import { hasCapability } from "~/features/staff/model/capability"
import {
  managedPurchaseSearchParams,
  parseManagedPurchaseFilters,
  type ManagedPurchaseFilters,
} from "~/features/staff/model/torrent-purchase-administration"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function StaffTorrentPurchasesPage() {
  return (
    <StaffAccessGate
      requiredAction="torrent.purchase.manage.read"
      pageHeader={{
        title: "购买记录",
        description: "查询全站种子购买、旧站继承权限和退款记录。",
      }}
    >
      {({ session, capabilities }) => (
        <PurchaseWorkbench
          csrfToken={session.csrf_token}
          canRefund={hasCapability(
            capabilities,
            "torrent.purchase.manage.refund"
          )}
        />
      )}
    </StaffAccessGate>
  )
}

function PurchaseWorkbench({
  csrfToken,
  canRefund,
}: {
  csrfToken: string
  canRefund: boolean
}) {
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = React.useMemo(
    () => parseManagedPurchaseFilters(searchParams),
    [searchParams]
  )
  const purchases = useQuery(managedTorrentPurchaseListQueryOptions(filters))
  const [queryDraft, setQueryDraft] = React.useState(filters.query)
  const [refundTarget, setRefundTarget] =
    React.useState<ManagedTorrentPurchase>()
  const [successMessage, setSuccessMessage] = React.useState("")

  React.useEffect(() => setQueryDraft(filters.query), [filters.query])
  React.useEffect(() => {
    const total = purchases.data?.total
    if (total === undefined || filters.page === 1) return
    const lastPage = Math.max(1, Math.ceil(total / filters.pageSize))
    if (filters.page > lastPage) {
      setSearchParams(
        managedPurchaseSearchParams({ ...filters, page: lastPage }),
        { replace: true }
      )
    }
  }, [filters, purchases.data?.total, setSearchParams])

  function updateFilters(update: Partial<ManagedPurchaseFilters>) {
    setSuccessMessage("")
    setRefundTarget(undefined)
    setSearchParams(managedPurchaseSearchParams({ ...filters, ...update }))
  }

  function handleSearch(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    updateFilters({ query: queryDraft.trim(), page: 1 })
  }

  if (purchases.isPending) return <PurchaseWorkbenchSkeleton />
  if (purchases.isError || !purchases.data) {
    return (
      <StaffPageFrame className="gap-4">
        <PurchaseHeading />
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>购买记录暂时无法读取</AlertTitle>
          <AlertDescription>
            请检查后台登录状态与 Core 服务后重试。
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void purchases.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      </StaffPageFrame>
    )
  }

  const data = purchases.data
  const hasFilters =
    Boolean(filters.query) ||
    filters.status !== "all" ||
    filters.source !== "all"
  return (
    <StaffPageFrame className="gap-4">
      {successMessage ? (
        <Alert>
          <CheckCircle2Icon />
          <AlertTitle>退款已经完成</AlertTitle>
          <AlertDescription>{successMessage}</AlertDescription>
        </Alert>
      ) : null}

      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-3">
          <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
            <CardTitle className="flex min-h-8 items-center gap-2 text-2xl">
              <ShoppingBagIcon className="size-5" aria-hidden="true" />
              <h1>购买记录</h1>
              <span className="text-sm font-normal text-muted-foreground">
                ({data.total.toLocaleString("zh-CN")} 条记录)
              </span>
            </CardTitle>
            <div className="grid min-w-0 grid-cols-1 gap-2 sm:grid-cols-[minmax(240px,360px)_140px_140px_32px]">
              <form onSubmit={handleSearch}>
                <Field>
                  <FieldLabel
                    htmlFor="managed-purchase-search"
                    className="sr-only"
                  >
                    搜索用户或种子
                  </FieldLabel>
                  <InputGroup className="h-8">
                    <InputGroupInput
                      id="managed-purchase-search"
                      value={queryDraft}
                      maxLength={100}
                      placeholder="搜索用户 ID / 用户名 / 种子..."
                      onChange={(event) => setQueryDraft(event.target.value)}
                    />
                    {queryDraft ? (
                      <InputGroupAddon align="inline-end">
                        <InputGroupButton
                          size="icon-xs"
                          aria-label="清空购买记录搜索"
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
                        aria-label="搜索购买记录"
                      >
                        <SearchIcon />
                      </InputGroupButton>
                    </InputGroupAddon>
                  </InputGroup>
                </Field>
              </form>
              <Select
                value={filters.status}
                onValueChange={(value) =>
                  updateFilters({
                    status: value as ManagedPurchaseFilters["status"],
                    page: 1,
                  })
                }
              >
                <SelectTrigger
                  size="sm"
                  aria-label="按购买状态筛选"
                  className="w-full"
                >
                  <SelectValue>
                    {managedPurchaseStatusFilterLabel(filters.status)}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="all">全部状态</SelectItem>
                    <SelectItem value="active">有效权限</SelectItem>
                    <SelectItem value="refunded">已退款</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Select
                value={filters.source}
                onValueChange={(value) =>
                  updateFilters({
                    source: value as ManagedPurchaseFilters["source"],
                    page: 1,
                  })
                }
              >
                <SelectTrigger
                  size="sm"
                  aria-label="按购买来源筛选"
                  className="w-full"
                >
                  <SelectValue>
                    {managedPurchaseSourceFilterLabel(filters.source)}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="all">全部来源</SelectItem>
                    <SelectItem value="live_purchase">PeerGo</SelectItem>
                    <SelectItem value="legacy_import">旧站继承</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Button
                type="button"
                variant="outline"
                size="icon-xs"
                aria-label="刷新购买记录"
                disabled={purchases.isFetching}
                onClick={() => void purchases.refetch()}
              >
                <RefreshCwIcon
                  className={purchases.isFetching ? "animate-spin" : undefined}
                />
              </Button>
            </div>
          </div>
        </CardHeader>

        <CardContent className="flex flex-col gap-3 p-6 pt-0">
          {data.items.length === 0 ? (
            <Empty className="min-h-64 rounded-lg border">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ShoppingBagIcon />
                </EmptyMedia>
                <EmptyTitle>
                  {hasFilters ? "没有匹配的购买记录" : "暂无购买记录"}
                </EmptyTitle>
                <EmptyDescription>
                  {hasFilters
                    ? "请调整搜索关键词或筛选条件。"
                    : "用户完成付费购买或旧站权限导入后会显示在这里。"}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <>
              <PurchaseTable
                purchases={data.items}
                canRefund={canRefund}
                onRefund={setRefundTarget}
              />
              <div className="grid gap-3 md:hidden">
                {data.items.map((item) => (
                  <PurchaseCard
                    key={`${item.buyer_numeric_id}:${item.torrent_id}:${item.purchased_at}`}
                    purchase={item}
                    canRefund={canRefund}
                    onRefund={() => setRefundTarget(item)}
                  />
                ))}
              </div>
            </>
          )}

          <OffsetPagination
            total={data.total}
            limit={data.limit}
            offset={data.offset}
            onOffsetChange={(offset) =>
              updateFilters({ page: Math.floor(offset / data.limit) + 1 })
            }
            ariaLabel="购买记录分页"
            className="border-t pt-4"
          />
        </CardContent>
      </Card>

      <TorrentPurchaseRefundDialog
        purchase={refundTarget}
        csrfToken={csrfToken}
        onOpenChange={(open) => {
          if (!open) setRefundTarget(undefined)
        }}
        onRefunded={setSuccessMessage}
      />
    </StaffPageFrame>
  )
}

function managedPurchaseStatusFilterLabel(
  status: ManagedPurchaseFilters["status"]
) {
  switch (status) {
    case "active":
      return "有效权限"
    case "refunded":
      return "已退款"
    default:
      return "全部状态"
  }
}

function managedPurchaseSourceFilterLabel(
  source: ManagedPurchaseFilters["source"]
) {
  switch (source) {
    case "live_purchase":
      return "PeerGo"
    case "legacy_import":
      return "旧站继承"
    default:
      return "全部来源"
  }
}

function PurchaseTable({
  purchases,
  canRefund,
  onRefund,
}: {
  purchases: ManagedTorrentPurchase[]
  canRefund: boolean
  onRefund: (purchase: ManagedTorrentPurchase) => void
}) {
  return (
    <div className="hidden overflow-hidden rounded-lg border md:block">
      <Table>
        <TableHeader className="bg-muted/50">
          <TableRow>
            <TableHead>购买用户</TableHead>
            <TableHead>种子</TableHead>
            <TableHead className="w-32 text-right">支付魔力值</TableHead>
            <TableHead className="w-28">来源</TableHead>
            <TableHead className="w-40">购买时间</TableHead>
            <TableHead className="w-40">状态</TableHead>
            <TableHead className="w-16 text-center">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {purchases.map((item) => (
            <TableRow
              key={`${item.buyer_numeric_id}:${item.torrent_id}:${item.purchased_at}`}
            >
              <TableCell>
                <div className="font-medium">{item.buyer_username}</div>
                <div className="text-xs text-muted-foreground">
                  #{item.buyer_numeric_id} · {item.buyer_display_name}
                </div>
              </TableCell>
              <TableCell className="max-w-[420px]">
                <Link
                  to={`/torrents/${item.torrent_id}`}
                  className="block truncate font-bold text-title transition-colors hover:text-title-hover"
                >
                  {item.torrent_title}
                </Link>
                <div className="text-xs text-muted-foreground">
                  #{item.torrent_id} · {item.category_name} · 发布者 #
                  {item.seller_numeric_id} {item.seller_username}
                </div>
              </TableCell>
              <TableCell className="text-right">
                <div className="font-medium tabular-nums">
                  {formatInteger(item.price)}
                </div>
                <div className="text-xs text-muted-foreground tabular-nums">
                  收入 {formatInteger(item.seller_income)} · 手续费{" "}
                  {formatInteger(item.tax)}
                </div>
              </TableCell>
              <TableCell>
                <SourceBadge source={item.source} />
              </TableCell>
              <TableCell className="text-muted-foreground">
                {formatDateTime(item.purchased_at)}
              </TableCell>
              <TableCell>
                <PurchaseStatus purchase={item} />
              </TableCell>
              <TableCell className="text-center">
                {item.status === "active" && canRefund ? (
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label={`退款用户 ${item.buyer_numeric_id} 的种子 ${item.torrent_id}`}
                    onClick={() => onRefund(item)}
                  >
                    <RotateCcwIcon />
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
  )
}

function PurchaseCard({
  purchase,
  canRefund,
  onRefund,
}: {
  purchase: ManagedTorrentPurchase
  canRefund: boolean
  onRefund: () => void
}) {
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle className="line-clamp-2 text-base">
          <Link to={`/torrents/${purchase.torrent_id}`}>
            #{purchase.torrent_id} · {purchase.torrent_title}
          </Link>
        </CardTitle>
        <div className="flex items-center gap-2">
          <SourceBadge source={purchase.source} />
          <Badge
            variant={purchase.status === "active" ? "secondary" : "destructive"}
          >
            {purchase.status === "active" ? "有效" : "已退款"}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        <div>
          用户 #{purchase.buyer_numeric_id} · {purchase.buyer_username}
        </div>
        <Separator />
        <div className="flex items-center justify-between">
          <span className="font-medium tabular-nums">
            {formatInteger(purchase.price)} 魔力值
          </span>
          <span className="text-xs text-muted-foreground">
            {formatDateTime(purchase.purchased_at)}
          </span>
        </div>
        {purchase.status === "active" && canRefund ? (
          <Button variant="outline" size="sm" onClick={onRefund}>
            <RotateCcwIcon data-icon="inline-start" />
            退款并撤销权限
          </Button>
        ) : null}
      </CardContent>
    </Card>
  )
}

function SourceBadge({ source }: { source: ManagedTorrentPurchase["source"] }) {
  return (
    <Badge variant={source === "legacy_import" ? "outline" : "secondary"}>
      {source === "legacy_import" ? "旧站继承" : "PeerGo"}
    </Badge>
  )
}

function PurchaseStatus({ purchase }: { purchase: ManagedTorrentPurchase }) {
  if (purchase.status === "active") {
    return <Badge variant="secondary">有效</Badge>
  }
  return (
    <div className="flex flex-col items-start gap-1">
      <Badge variant="destructive">已退款</Badge>
      <span className="max-w-36 truncate text-xs text-muted-foreground">
        {purchase.refund_reason}
      </span>
      {purchase.refunded_at ? (
        <span className="text-xs text-muted-foreground">
          {formatDateTime(purchase.refunded_at)}
        </span>
      ) : null}
    </div>
  )
}

function PurchaseHeading() {
  return (
    <h1 className="flex items-center gap-2 font-heading text-2xl font-semibold">
      <ShoppingBagIcon className="size-6" />
      购买记录
    </h1>
  )
}

function PurchaseWorkbenchSkeleton() {
  return (
    <StaffPageFrame
      className="gap-4"
      aria-label="正在加载购买记录"
      aria-busy="true"
    >
      <Skeleton className="h-[38rem] w-full rounded-xl" />
    </StaffPageFrame>
  )
}
