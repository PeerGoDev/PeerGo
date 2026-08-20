import type { ReactNode } from "react"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CoinsIcon,
  StarIcon,
  TrendingUpIcon,
} from "lucide-react"

import { Skeleton } from "~/components/ui/skeleton"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "~/components/ui/tooltip"
import { useEconomyOverview } from "~/features/economy/api/economy.queries"
import { useTrafficOverview } from "~/features/traffic/api/traffic.queries"
import { formatShareRatio } from "~/features/traffic/model/format"
import { cn } from "~/lib/utils"
import { exactNonNegativeInteger, formatBytes } from "~/shared/formatters/bytes"
import {
  formatCompactInteger,
  formatInteger,
} from "~/shared/formatters/integer"

export function HeaderTrafficSummary({
  userId,
  trafficEnabled,
  economyEnabled,
}: {
  userId: string | undefined
  trafficEnabled: boolean
  economyEnabled: boolean
}) {
  const traffic = useTrafficOverview(trafficEnabled ? userId : undefined)
  const economy = useEconomyOverview(economyEnabled ? userId : undefined)

  if (!trafficEnabled && !economyEnabled) {
    return null
  }

  const trafficPending = trafficEnabled && traffic.isPending
  const economyPending = economyEnabled && economy.isPending

  if (trafficPending && economyPending) {
    return (
      <div
        className="hidden w-56 grid-cols-2 gap-x-5 md:grid"
        aria-label="正在加载账户汇总"
        aria-busy="true"
      >
        <HeaderSummarySkeleton rows={3} />
        <HeaderSummarySkeleton rows={2} />
      </div>
    )
  }

  if (!traffic.data && !economy.data && !trafficPending && !economyPending) {
    return null
  }

  return (
    <div className="hidden items-start gap-5 text-xs leading-4 md:flex">
      {traffic.data ? (
        <dl className="flex w-[118px] flex-col gap-1">
          <HeaderMetric
            icon={<TrendingUpIcon className="text-muted-foreground" />}
            label="分享率"
            value={formatShareRatio(
              traffic.data.totals.credited_uploaded_bytes,
              traffic.data.totals.charged_downloaded_bytes,
              3
            )}
            valueClassName={shareRatioClassName(
              traffic.data.totals.credited_uploaded_bytes,
              traffic.data.totals.charged_downloaded_bytes
            )}
          />
          <HeaderMetric
            icon={<ArrowUpIcon className="text-success-foreground" />}
            label="上传"
            value={formatBytes(traffic.data.totals.credited_uploaded_bytes)}
          />
          <HeaderMetric
            icon={<ArrowDownIcon className="text-destructive" />}
            label="下载"
            value={formatBytes(traffic.data.totals.charged_downloaded_bytes)}
          />
        </dl>
      ) : trafficPending ? (
        <HeaderSummarySkeleton rows={3} />
      ) : null}

      {economy.data ? (
        <dl className="flex w-[110px] flex-col gap-1">
          <HeaderMetric
            icon={<StarIcon className="text-warning-foreground" />}
            label="等级"
            value={`Lv.${economy.data.progress.level}`}
          />
          <HeaderMetric
            icon={<CoinsIcon className="text-warning-foreground" />}
            label="魔力值"
            value={formatCompactInteger(economy.data.magic_balance)}
            fullValue={`${formatInteger(economy.data.magic_balance)} 魔力值`}
          />
        </dl>
      ) : economyPending ? (
        <HeaderSummarySkeleton rows={2} />
      ) : null}
    </div>
  )
}

function HeaderMetric({
  icon,
  label,
  value,
  valueClassName,
  fullValue,
}: {
  icon: ReactNode
  label: string
  value: string
  valueClassName?: string
  fullValue?: string
}) {
  const valueContent = (
    <span
      className={cn("block max-w-full truncate", valueClassName)}
      aria-label={fullValue}
    >
      {value}
    </span>
  )

  return (
    <div className="flex items-center gap-1.5">
      <dt className="flex min-w-[52px] items-center gap-1 text-muted-foreground">
        <span className="[&>svg]:size-3" aria-hidden="true">
          {icon}
        </span>
        <span>{label}</span>
      </dt>
      <dd className="ml-auto min-w-0 text-right font-medium text-foreground tabular-nums">
        {fullValue ? (
          <Tooltip>
            <TooltipTrigger render={valueContent} />
            <TooltipContent>{fullValue}</TooltipContent>
          </Tooltip>
        ) : (
          valueContent
        )}
      </dd>
    </div>
  )
}

function HeaderSummarySkeleton({ rows }: { rows: number }) {
  return (
    <div className="flex w-[110px] flex-col gap-1" aria-hidden="true">
      {Array.from({ length: rows }, (_, index) => (
        <Skeleton key={index} className="h-3 w-full" />
      ))}
    </div>
  )
}

function shareRatioClassName(uploaded: string, downloaded: string) {
  const uploadedBytes = exactNonNegativeInteger(uploaded)
  const downloadedBytes = exactNonNegativeInteger(downloaded)

  if (uploadedBytes === undefined || downloadedBytes === undefined) {
    return undefined
  }

  if (downloadedBytes === 0n) {
    return uploadedBytes > 0n ? "text-success-foreground" : undefined
  }

  // Compare integer products so large byte counters never lose precision.
  const ratioPermille = (uploadedBytes * 1_000n) / downloadedBytes
  if (ratioPermille < 400n) return "text-destructive"
  if (ratioPermille < 1_000n) return "text-warning-foreground"
  return "text-success-foreground"
}
