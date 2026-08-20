import * as React from "react"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  CoinsIcon,
  LogInIcon,
  RefreshCwIcon,
  ShieldXIcon,
  ShoppingBagIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import { Card, CardContent } from "~/components/ui/card"
import {
  Empty,
  EmptyContent,
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
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import { useMyTorrentPurchases } from "~/features/torrent/api/torrent-purchases.queries"
import { managedTorrentStateLabel } from "~/features/staff/model/torrent-administration"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { PageLayout } from "~/shared/components/page-layout"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

const purchasePageSize = 20

export function MyTorrentPurchasesPage() {
  const [offset, setOffset] = React.useState(0)
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const canRead = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "torrent.purchase.read.self"
    )
  )
  const purchases = useMyTorrentPurchases(
    session.data?.user.id,
    purchasePageSize,
    offset,
    canRead
  )

  React.useEffect(() => {
    const total = purchases.data?.total
    if (total === undefined || offset === 0 || offset < total) return
    setOffset(
      Math.max(
        0,
        Math.floor((Math.max(total, 1) - 1) / purchasePageSize) *
          purchasePageSize
      )
    )
  }, [offset, purchases.data?.total])

  return (
    <PageLayout className="gap-6">
      <h1 className="flex items-center gap-3 font-heading text-3xl font-bold">
        <ShoppingBagIcon className="size-8" />
        已购种子
        {purchases.data && purchases.data.total > 0 ? (
          <span className="text-lg font-normal text-muted-foreground">
            ({purchases.data.total.toLocaleString("zh-CN")})
          </span>
        ) : null}
      </h1>

      {(session.isPending || (session.data && capabilities.isPending)) && (
        <div className="h-72 animate-pulse rounded-lg border bg-muted/30" />
      )}

      {session.isError ? (
        <PurchaseAlert
          title="会话状态暂时无法读取"
          description={requestErrorDescription(
            session.error,
            "会话请求未能完成，请稍后刷新页面。"
          )}
          retry={() => void session.refetch()}
        />
      ) : null}

      {!session.isPending && !session.isError && !session.data ? (
        <PurchaseAccessCard
          icon={<LogInIcon />}
          title="登录后查看已购种子"
          description="购买记录和永久下载权限仅对本人可见。"
          action={
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          }
        />
      ) : null}

      {session.data && capabilities.isError ? (
        <PurchaseAlert
          title="暂时无法确认购买记录权限"
          description={requestErrorDescription(
            capabilities.error,
            "权限请求未能完成，请稍后再试。"
          )}
          retry={() => void capabilities.refetch()}
        />
      ) : null}

      {session.data && capabilities.data && !canRead ? (
        <PurchaseAccessCard
          icon={<ShieldXIcon />}
          title="当前账户不能查看购买记录"
          description="如有疑问，请联系站点管理人员。"
          action={
            <Link to="/" className={buttonVariants({ variant: "outline" })}>
              返回首页
            </Link>
          }
        />
      ) : null}

      {session.data && canRead && purchases.isPending ? (
        <div className="h-72 animate-pulse rounded-lg border bg-muted/30" />
      ) : null}

      {session.data && canRead && purchases.isError ? (
        <PurchaseAlert
          title="已购种子暂时无法查看"
          description={purchaseErrorDescription(purchases.error)}
          retry={() => void purchases.refetch()}
        />
      ) : null}

      {session.data && canRead && purchases.data ? (
        purchases.data.items.length === 0 ? (
          <PurchaseAccessCard
            icon={<ShoppingBagIcon />}
            title="暂无已购种子"
            description="需要魔力值购买的种子会在购买完成后显示在这里。"
            action={
              <Link to="/torrents" className={buttonVariants()}>
                浏览种子
              </Link>
            }
          />
        ) : (
          <section aria-label="已购种子列表" className="flex flex-col gap-3">
            <div className="hidden overflow-hidden rounded-lg border md:block">
              <Table>
                <TableHeader className="bg-muted/50">
                  <TableRow>
                    <TableHead className="w-20 text-right">ID</TableHead>
                    <TableHead>种子名称</TableHead>
                    <TableHead className="w-28">分类</TableHead>
                    <TableHead className="w-28 text-right">
                      支付魔力值
                    </TableHead>
                    <TableHead className="w-44">购买时间</TableHead>
                    <TableHead className="w-24 text-center">来源</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {purchases.data.items.map((item) => (
                    <TableRow key={item.torrent_id}>
                      <TableCell className="text-right font-mono text-xs text-muted-foreground">
                        {item.torrent_id}
                      </TableCell>
                      <TableCell>
                        <div className="flex min-w-0 flex-col">
                          {item.torrent_state === "published" ? (
                            <Link
                              to={`/torrents/${item.torrent_id}`}
                              className="truncate font-medium hover:text-primary"
                            >
                              {item.title}
                            </Link>
                          ) : (
                            <span className="truncate font-medium">
                              {item.title}
                            </span>
                          )}
                          {item.torrent_state !== "published" ? (
                            <span className="text-xs text-muted-foreground">
                              当前{managedTorrentStateLabel(item.torrent_state)}
                            </span>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell>{item.category_name}</TableCell>
                      <TableCell className="text-right font-medium tabular-nums">
                        {formatInteger(item.price)}
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {formatDateTime(item.purchased_at)}
                      </TableCell>
                      <TableCell className="text-center">
                        <Badge
                          variant={item.legacy_import ? "outline" : "secondary"}
                        >
                          {item.legacy_import ? "旧站继承" : "PeerGo"}
                        </Badge>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>

            <div className="grid gap-3 md:hidden">
              {purchases.data.items.map((item) => (
                <Card key={item.torrent_id} size="sm">
                  <CardContent className="flex flex-col gap-3">
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="text-xs text-muted-foreground">
                          #{item.torrent_id} · {item.category_name}
                        </div>
                        {item.torrent_state === "published" ? (
                          <Link
                            to={`/torrents/${item.torrent_id}`}
                            className="line-clamp-2 font-medium hover:text-primary"
                          >
                            {item.title}
                          </Link>
                        ) : (
                          <div className="line-clamp-2 font-medium">
                            {item.title}
                          </div>
                        )}
                      </div>
                      <Badge
                        variant={item.legacy_import ? "outline" : "secondary"}
                      >
                        {item.legacy_import ? "旧站继承" : "PeerGo"}
                      </Badge>
                    </div>
                    <div className="flex items-center justify-between border-t pt-3 text-sm">
                      <span className="inline-flex items-center gap-1 font-medium tabular-nums">
                        <CoinsIcon className="size-4" />
                        {formatInteger(item.price)} 魔力值
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {formatDateTime(item.purchased_at)}
                      </span>
                    </div>
                  </CardContent>
                </Card>
              ))}
            </div>

            <OffsetPagination
              total={purchases.data.total}
              limit={purchases.data.limit}
              offset={purchases.data.offset}
              onOffsetChange={setOffset}
              ariaLabel="已购种子分页"
            />
          </section>
        )
      ) : null}
    </PageLayout>
  )
}

function purchaseErrorDescription(error: unknown) {
  if (error instanceof ApiProblemError) {
    return error.detail ?? "购买记录请求未能完成，请稍后再试。"
  }
  return "购买记录请求未能完成，请稍后再试。"
}

function PurchaseAlert({
  title,
  description,
  retry,
}: {
  title: string
  description: string
  retry: () => void
}) {
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{description}</AlertDescription>
      <AlertAction>
        <Button type="button" variant="outline" size="sm" onClick={retry}>
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </AlertAction>
    </Alert>
  )
}

function PurchaseAccessCard({
  icon,
  title,
  description,
  action,
}: {
  icon: React.ReactNode
  title: string
  description: string
  action: React.ReactNode
}) {
  return (
    <Card>
      <CardContent>
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">{icon}</EmptyMedia>
            <EmptyTitle>{title}</EmptyTitle>
            <EmptyDescription>{description}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>{action}</EmptyContent>
        </Empty>
      </CardContent>
    </Card>
  )
}
