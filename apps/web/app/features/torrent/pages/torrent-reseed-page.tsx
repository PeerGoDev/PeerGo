import * as React from "react"
import { Link, useSearchParams } from "react-router"
import { CircleAlertIcon, RefreshCwIcon } from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Card } from "~/components/ui/card"
import {
  Empty,
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
import {
  type OpenTorrentReseedRequest,
  useOpenTorrentReseedRequests,
} from "~/features/torrent/api/torrent.queries"
import { TorrentListSkeleton } from "~/features/torrent/components/torrent-list-state"
import { TorrentTitleLink } from "~/features/torrent/components/torrent-title-link"
import { TorrentNsfwControl } from "~/features/torrent/components/torrent-view-controls"
import { useNsfwFilter } from "~/features/torrent/hooks/use-nsfw-filter"
import { requestErrorDescription } from "~/shared/api/problem"
import { NumberedPagination } from "~/shared/components/numbered-pagination"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatInteger } from "~/shared/formatters/integer"

const pageSize = 20
const maximumPage = Math.floor(1_000_000 / pageSize) + 1

export function TorrentReseedPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [hideNsfw, setHideNsfw] = useNsfwFilter()
  const requestedPage = Number(searchParams.get("page") ?? 1)
  const page =
    Number.isSafeInteger(requestedPage) &&
    requestedPage >= 1 &&
    requestedPage <= maximumPage
      ? requestedPage
      : 1
  const session = useWebSession()
  const authenticated = Boolean(session.data)
  const requests = useOpenTorrentReseedRequests(
    { limit: pageSize, offset: (page - 1) * pageSize, excludeNsfw: hideNsfw },
    authenticated
  )
  const totalPages = Math.min(
    maximumPage,
    Math.max(1, Math.ceil((requests.data?.total ?? 0) / pageSize))
  )
  const pageOutOfRange = Boolean(requests.data) && page > totalPages

  React.useEffect(() => {
    if (pageOutOfRange) {
      setSearchParams(totalPages === 1 ? {} : { page: String(totalPages) }, {
        replace: true,
      })
    }
  }, [pageOutOfRange, setSearchParams, totalPages])

  if (!session.isPending && !authenticated) {
    return (
      <PageLayout className="items-center justify-center">
        <Card className="w-full max-w-md items-center p-6 text-center">
          <h1 className="text-lg font-semibold">登录后查看补种专区</h1>
          <Button nativeButton={false} render={<Link to="/login" />}>
            前往登录
          </Button>
        </Card>
      </PageLayout>
    )
  }

  return (
    <PageLayout>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <PageHeader
          title="补种专区"
          description="还保留完整文件时，可从种子详情获取原种，在客户端校验后继续做种。"
        />
        <TorrentNsfwControl
          hideNsfw={hideNsfw}
          onHideNsfwChange={setHideNsfw}
        />
      </div>
      {requests.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>补种请求暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(requests.error, "请稍后重试。")}
          </AlertDescription>
          <AlertAction>
            <Button
              size="sm"
              variant="outline"
              onClick={() => void requests.refetch()}
            >
              重试
            </Button>
          </AlertAction>
        </Alert>
      ) : requests.isPending || pageOutOfRange ? (
        <TorrentListSkeleton />
      ) : requests.data.items.length === 0 ? (
        <Card className="py-0">
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <RefreshCwIcon />
              </EmptyMedia>
              <EmptyTitle>当前暂无待补种的种子</EmptyTitle>
            </EmptyHeader>
          </Empty>
        </Card>
      ) : (
        <>
          <Card className="gap-0 overflow-hidden py-0">
            <Table aria-label="等待补种的种子" className="table-fixed">
              <TableHeader>
                <TableRow>
                  <TableHead scope="col">种子</TableHead>
                  <TableHead
                    scope="col"
                    className="hidden w-24 text-right sm:table-cell"
                  >
                    请求人数
                  </TableHead>
                  <TableHead
                    scope="col"
                    className="hidden w-32 text-right sm:table-cell"
                  >
                    有效做种门槛
                  </TableHead>
                  <TableHead
                    scope="col"
                    className="hidden w-36 text-right sm:table-cell"
                  >
                    预计复活奖励
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {requests.data.items.map((request) => (
                  <ReseedRequestRow
                    key={request.torrent_id}
                    request={request}
                  />
                ))}
              </TableBody>
            </Table>
          </Card>
          {totalPages > 1 ? (
            <NumberedPagination
              ariaLabel="补种专区分页"
              currentPage={page}
              totalPages={totalPages}
              summary={`共 ${formatInteger(requests.data.total)} 个种子等待补种`}
              onPageChange={(next) =>
                setSearchParams(next === 1 ? {} : { page: String(next) })
              }
            />
          ) : null}
        </>
      )}
    </PageLayout>
  )
}

function ReseedRequestRow({ request }: { request: OpenTorrentReseedRequest }) {
  const reward = request.reseed_reward
  const people = `${formatInteger(request.requester_count)} 人`
  const hours = reward ? `${formatInteger(reward.required_hours)} 小时` : "—"
  const amount = reward
    ? `${formatInteger(reward.magic_amount)} 魔力值`
    : "暂无奖励"

  return (
    <TableRow>
      <TableCell className="[overflow-wrap:anywhere] whitespace-normal">
        <TorrentTitleLink
          torrentId={request.torrent_id}
          title={request.torrent_title}
        />
        <p className="mt-1.5 text-xs text-muted-foreground sm:hidden">
          {people}请求补种
          {reward
            ? ` · 有效做种满 ${hours} · 预计奖励 ${amount}`
            : ` · ${amount}`}
        </p>
      </TableCell>
      <TableCell className="hidden text-right tabular-nums sm:table-cell">
        {people}
      </TableCell>
      <TableCell className="hidden text-right tabular-nums sm:table-cell">
        {hours}
      </TableCell>
      <TableCell className="hidden text-right tabular-nums sm:table-cell">
        {reward ? (
          <span className="font-medium text-primary">{amount}</span>
        ) : (
          <span className="text-muted-foreground">{amount}</span>
        )}
      </TableCell>
    </TableRow>
  )
}
