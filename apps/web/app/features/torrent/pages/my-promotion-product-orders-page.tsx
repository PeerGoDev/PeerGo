import * as React from "react"
import { Link } from "react-router"
import {
  BadgePercentIcon,
  CircleAlertIcon,
  LogInIcon,
  PinIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { buttonVariants } from "~/components/ui/button"
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
import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type PromotionProductOrder,
  useMyPromotionProductOrders,
} from "~/features/torrent/api/promotion-products.queries"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { PageLayout } from "~/shared/components/page-layout"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"
import { requestErrorDescription } from "~/shared/api/problem"

const pageSize = 20

export function MyPromotionProductOrdersPage() {
  const [offset, setOffset] = React.useState(0)
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const allowed = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "torrent.promotion.purchase.self"
    )
  )
  const orders = useMyPromotionProductOrders(
    session.data?.user.id,
    pageSize,
    offset,
    allowed
  )

  return (
    <PageLayout className="gap-6">
      <h1 className="flex items-center gap-3 font-heading text-3xl font-bold">
        <BadgePercentIcon className="size-8" />
        促销与置顶记录
        {orders.data?.total ? (
          <span className="text-lg font-normal text-muted-foreground">
            ({orders.data.total.toLocaleString("zh-CN")})
          </span>
        ) : null}
      </h1>

      {session.isPending || (session.data && capabilities.isPending) ? (
        <Skeleton className="h-72 rounded-lg border" />
      ) : null}

      {!session.isPending && !session.data ? (
        <Empty className="min-h-72 border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <LogInIcon />
            </EmptyMedia>
            <EmptyTitle>登录后查看购买记录</EmptyTitle>
            <EmptyDescription>
              促销、置顶时间段和支付价格仅对本人可见。
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Link to="/login" className={buttonVariants()}>
              前往登录
            </Link>
          </EmptyContent>
        </Empty>
      ) : null}

      {session.data && capabilities.data && !allowed ? (
        <Alert>
          <CircleAlertIcon />
          <AlertTitle>当前账户不能购买种子促销</AlertTitle>
          <AlertDescription>如有疑问，请联系站点管理人员。</AlertDescription>
        </Alert>
      ) : null}

      {session.data && allowed && orders.isPending ? (
        <Skeleton className="h-72 rounded-lg border" />
      ) : null}

      {session.data && allowed && orders.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>促销购买记录暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              orders.error,
              "请稍后重试，历史订单不会因此改变。"
            )}
          </AlertDescription>
        </Alert>
      ) : null}

      {session.data && allowed && orders.data ? (
        orders.data.items.length === 0 ? (
          <Empty className="min-h-72 border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <BadgePercentIcon />
              </EmptyMedia>
              <EmptyTitle>暂无促销购买记录</EmptyTitle>
              <EmptyDescription>
                在种子详情页可以使用整数魔力值购买优惠或置顶。
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Link to="/torrents" className={buttonVariants()}>
                浏览种子
              </Link>
            </EmptyContent>
          </Empty>
        ) : (
          <section className="flex flex-col gap-3" aria-label="促销与置顶记录">
            <div className="overflow-x-auto rounded-lg border">
              <Table className="min-w-[760px]">
                <TableHeader className="bg-muted/50">
                  <TableRow>
                    <TableHead className="w-20 text-right">ID</TableHead>
                    <TableHead>种子名称</TableHead>
                    <TableHead>购买内容</TableHead>
                    <TableHead className="w-28 text-right">
                      支付魔力值
                    </TableHead>
                    <TableHead className="w-44">购买时间</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {orders.data.items.map((order) => (
                    <TableRow key={order.id}>
                      <TableCell className="text-right font-mono text-xs text-muted-foreground">
                        {order.torrent_id}
                      </TableCell>
                      <TableCell className="max-w-80">
                        <Link
                          to={`/torrents/${order.torrent_id}`}
                          className="line-clamp-2 font-medium hover:text-primary"
                        >
                          {order.torrent_title}
                        </Link>
                      </TableCell>
                      <TableCell>
                        <OrderProducts order={order} />
                      </TableCell>
                      <TableCell className="text-right font-medium tabular-nums">
                        {formatInteger(order.total_price)}
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {formatCompactDateTime(order.purchased_at)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            <OffsetPagination
              total={orders.data.total}
              limit={pageSize}
              offset={offset}
              onOffsetChange={setOffset}
              ariaLabel="促销与置顶记录分页"
            />
          </section>
        )
      ) : null}
    </PageLayout>
  )
}

function OrderProducts({ order }: { order: PromotionProductOrder }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {order.promotion ? (
        <Badge variant="secondary">
          <BadgePercentIcon data-icon="inline-start" />
          {promotionLabel(order.promotion)} · {order.promotion_days} 天
        </Badge>
      ) : null}
      {order.sticky_days ? (
        <Badge variant="outline">
          <PinIcon data-icon="inline-start" />
          置顶 · {order.sticky_days} 天
        </Badge>
      ) : null}
    </div>
  )
}

function promotionLabel(
  value: NonNullable<PromotionProductOrder["promotion"]>
) {
  const labels = {
    free: "免费",
    double_upload: "2× 上传",
    double_upload_free: "2× 上传 / 免费",
    half_download: "50% 下载",
    double_upload_half_download: "2× 上传 / 50% 下载",
    thirty_percent_download: "30% 下载",
  } as const
  return labels[value]
}
