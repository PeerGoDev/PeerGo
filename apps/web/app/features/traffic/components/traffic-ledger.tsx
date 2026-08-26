import {
  ChevronDownIcon,
  Clock3Icon,
  DatabaseIcon,
  ListTreeIcon,
} from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import type { TrafficOverview } from "~/features/traffic/api/traffic.queries"
import { trafficAdjustmentLabels } from "~/features/traffic/model/format"
import { formatBytes } from "~/shared/formatters/bytes"
import { exactNonNegativeInteger } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"

type TrafficEntry = TrafficOverview["entries"][number]

export function TrafficLedger({
  entries,
  totalEntries,
}: {
  entries: TrafficEntry[]
  totalEntries: string
}) {
  const total = exactNonNegativeInteger(totalEntries)?.toLocaleString("zh-CN")
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="border-b py-3.5">
        <CardTitle>最近流量</CardTitle>
        <CardDescription>
          {total ? `共 ${total} 条，` : ""}按时间从新到旧显示最近 20 条记录。
        </CardDescription>
        <CardAction>
          <Badge variant="outline">当前 {entries.length} 条</Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="px-0">
        {entries.length === 0 ? (
          <Empty className="min-h-64 rounded-none border-0 py-12">
            <EmptyHeader>
              <EmptyMedia className="mb-2 size-auto bg-transparent text-muted-foreground/50 [&_svg]:size-12">
                <DatabaseIcon />
              </EmptyMedia>
              <EmptyTitle>还没有流量记录</EmptyTitle>
              <EmptyDescription>
                开始下载或做种后，相关记录会陆续出现在这里。
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <>
            <div className="hidden md:block">
              <Table containerClassName="px-3">
                <TableHeader className="bg-secondary/70">
                  <TableRow>
                    <TableHead className="w-full min-w-72 pl-4 text-muted-foreground">
                      种子 / 区间
                    </TableHead>
                    <TableHead className="text-right text-muted-foreground">
                      实际上传
                    </TableHead>
                    <TableHead className="text-right text-muted-foreground">
                      有效上传
                    </TableHead>
                    <TableHead className="text-right text-muted-foreground">
                      实际下载
                    </TableHead>
                    <TableHead className="text-right text-muted-foreground">
                      有效下载
                    </TableHead>
                    <TableHead className="pr-4 text-right text-muted-foreground">
                      记录时间
                    </TableHead>
                  </TableRow>
                </TableHeader>
                {entries.map((entry) => (
                  <TrafficLedgerRows key={entry.id} entry={entry} />
                ))}
              </Table>
            </div>
            <div className="grid gap-0 divide-y md:hidden">
              {entries.map((entry) => (
                <TrafficLedgerCard key={entry.id} entry={entry} />
              ))}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}

function TrafficLedgerRows({ entry }: { entry: TrafficEntry }) {
  if (entry.explanation.status !== "complete") {
    return (
      <TableBody>
        <TrafficLedgerRow entry={entry} />
      </TableBody>
    )
  }

  return (
    <Collapsible render={<TableBody />}>
      <TrafficLedgerRow entry={entry} />
      <CollapsibleContent
        render={<TableRow className="hover:bg-transparent" />}
      >
        <TableCell colSpan={6} className="bg-muted/30 p-4 whitespace-normal">
          <TrafficExplanationDetails entry={entry} />
        </TableCell>
      </CollapsibleContent>
    </Collapsible>
  )
}

function TrafficLedgerRow({ entry }: { entry: TrafficEntry }) {
  const labels = trafficAdjustmentLabels(entry)
  return (
    <TableRow className="h-14 hover:bg-accent/70">
      <TableCell className="max-w-0 pl-4">
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate font-medium">{entry.torrent.title}</span>
            {labels.map((label) => (
              <Badge key={label} variant="secondary">
                {label}
              </Badge>
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span>
              {formatDateTime(entry.interval_started_at)} —{" "}
              {formatDateTime(entry.interval_ended_at)}
            </span>
            {entry.explanation.status === "complete" ? (
              <ExplanationTrigger count={entry.explanation.segment_count} />
            ) : (
              <ExplanationAvailability status={entry.explanation.status} />
            )}
          </div>
        </div>
      </TableCell>
      <ByteCell value={entry.raw_uploaded_bytes} />
      <ByteCell value={entry.credited_uploaded_bytes} emphasized="upload" />
      <ByteCell value={entry.raw_downloaded_bytes} />
      <ByteCell value={entry.charged_downloaded_bytes} emphasized="download" />
      <TableCell className="pr-4 text-right text-xs text-muted-foreground">
        <time dateTime={entry.settled_at}>
          {formatDateTime(entry.settled_at)}
        </time>
      </TableCell>
    </TableRow>
  )
}

function ByteCell({
  value,
  emphasized,
}: {
  value: string
  emphasized?: "upload" | "download"
}) {
  return (
    <TableCell
      className={
        emphasized === "upload"
          ? "text-right text-success-foreground tabular-nums"
          : emphasized === "download"
            ? "text-right text-primary tabular-nums"
            : "text-right tabular-nums"
      }
    >
      {formatBytes(value)}
    </TableCell>
  )
}

function TrafficLedgerCard({ entry }: { entry: TrafficEntry }) {
  const labels = trafficAdjustmentLabels(entry)
  const content = (
    <article className="flex flex-col gap-3 p-4">
      <div className="flex flex-col gap-1">
        <h3 className="line-clamp-2 font-medium">{entry.torrent.title}</h3>
        <div className="flex flex-wrap gap-1.5">
          {labels.map((label) => (
            <Badge key={label} variant="secondary">
              {label}
            </Badge>
          ))}
        </div>
      </div>
      <dl className="grid grid-cols-2 gap-3 text-xs">
        <TrafficMeasure label="实际上传" value={entry.raw_uploaded_bytes} />
        <TrafficMeasure
          label="有效上传"
          value={entry.credited_uploaded_bytes}
          emphasized="upload"
        />
        <TrafficMeasure label="实际下载" value={entry.raw_downloaded_bytes} />
        <TrafficMeasure
          label="有效下载"
          value={entry.charged_downloaded_bytes}
          emphasized="download"
        />
      </dl>
      <div className="flex items-start gap-2 text-xs text-muted-foreground">
        <Clock3Icon className="mt-0.5 size-3.5 shrink-0" />
        <span>
          {formatDateTime(entry.interval_started_at)} —{" "}
          {formatDateTime(entry.interval_ended_at)}
        </span>
      </div>
      {entry.explanation.status === "complete" ? (
        <>
          <CollapsibleTrigger render={<Button variant="outline" size="sm" />}>
            <ListTreeIcon data-icon="inline-start" />
            查看 {entry.explanation.segment_count} 个优惠时段
            <ChevronDownIcon data-icon="inline-end" />
          </CollapsibleTrigger>
          <CollapsibleContent>
            <TrafficExplanationDetails entry={entry} />
          </CollapsibleContent>
        </>
      ) : (
        <ExplanationAvailability status={entry.explanation.status} />
      )}
    </article>
  )
  return entry.explanation.status === "complete" ? (
    <Collapsible>{content}</Collapsible>
  ) : (
    content
  )
}

function ExplanationTrigger({ count }: { count: string }) {
  return (
    <CollapsibleTrigger render={<Button variant="ghost" size="xs" />}>
      <ListTreeIcon data-icon="inline-start" />
      {count} 个时段
      <ChevronDownIcon data-icon="inline-end" />
    </CollapsibleTrigger>
  )
}

function ExplanationAvailability({
  status,
}: {
  status: TrafficEntry["explanation"]["status"]
}) {
  return (
    <Badge variant="outline">
      {status === "too_many_segments" ? "明细较多" : "暂无时段明细"}
    </Badge>
  )
}

function TrafficExplanationDetails({ entry }: { entry: TrafficEntry }) {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <p className="max-w-3xl text-xs leading-relaxed text-muted-foreground">
          一条记录跨越多个优惠时段时会分别计算，合计即本条记录显示的有效流量。
        </p>
        <Badge variant="outline">
          {entry.explanation.segment_count} 个时段
        </Badge>
      </div>
      <div className="grid gap-2 xl:grid-cols-2">
        {entry.explanation.segments.map((segment, index) => {
          const labels = trafficAdjustmentLabels(segment)
          return (
            <Card key={`${segment.started_at}-${index}`} size="sm">
              <CardHeader>
                <CardTitle>时段 {index + 1}</CardTitle>
                <CardDescription>
                  {formatDateTime(segment.started_at)} —{" "}
                  {formatDateTime(segment.ended_at)}
                </CardDescription>
                <CardAction className="flex flex-wrap justify-end gap-1">
                  {labels.length === 0 ? (
                    <Badge variant="outline">无优惠</Badge>
                  ) : (
                    labels.map((label) => (
                      <Badge key={label} variant="secondary">
                        {label}
                      </Badge>
                    ))
                  )}
                </CardAction>
              </CardHeader>
              <CardContent>
                <dl className="grid grid-cols-2 gap-3 text-xs sm:grid-cols-4">
                  <TrafficMeasure
                    label="实际上传"
                    value={segment.raw_uploaded_bytes}
                  />
                  <TrafficMeasure
                    label="有效上传"
                    value={segment.credited_uploaded_bytes}
                    emphasized="upload"
                  />
                  <TrafficMeasure
                    label="实际下载"
                    value={segment.raw_downloaded_bytes}
                  />
                  <TrafficMeasure
                    label="有效下载"
                    value={segment.charged_downloaded_bytes}
                    emphasized="download"
                  />
                </dl>
              </CardContent>
            </Card>
          )
        })}
      </div>
    </div>
  )
}

function TrafficMeasure({
  label,
  value,
  emphasized,
}: {
  label: string
  value: string
  emphasized?: "upload" | "download"
}) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-muted-foreground">{label}</dt>
      <dd
        className={
          emphasized === "upload"
            ? "font-medium text-success-foreground tabular-nums"
            : emphasized === "download"
              ? "font-medium text-primary tabular-nums"
              : "font-medium tabular-nums"
        }
      >
        {formatBytes(value)}
      </dd>
    </div>
  )
}
