import type { ReactNode } from "react"
import { ArrowDownIcon, ArrowUpIcon } from "lucide-react"

import { Skeleton } from "~/components/ui/skeleton"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "~/components/ui/tooltip"
import { useEconomyOverview } from "~/features/economy/api/economy.queries"
import { useTrafficOverview } from "~/features/traffic/api/traffic.queries"
import {
  formatShareRatio,
  shareRatioToneClass,
} from "~/features/traffic/model/format"
import { cn } from "~/lib/utils"
import { formatBytes } from "~/shared/formatters/bytes"
import {
  formatCompactInteger,
  formatInteger,
} from "~/shared/formatters/integer"

export function HeaderTrafficSummary({
  userId,
  trafficEnabled,
  economyEnabled,
  variant = "header",
  className,
}: {
  userId: string | undefined
  trafficEnabled: boolean
  economyEnabled: boolean
  variant?: "header" | "menu"
  className?: string
}) {
  const traffic = useTrafficOverview(trafficEnabled ? userId : undefined)
  const economy = useEconomyOverview(economyEnabled ? userId : undefined)

  if (!trafficEnabled && !economyEnabled) {
    return null
  }

  const trafficPending = trafficEnabled && traffic.isPending
  const economyPending = economyEnabled && economy.isPending

  if (!traffic.data && !economy.data && !trafficPending && !economyPending) {
    return null
  }

  const totals = traffic.data?.totals
  const ratio = totals
    ? {
        value: formatShareRatio(
          totals.credited_uploaded_bytes,
          totals.charged_downloaded_bytes,
          variant === "header" ? 3 : 2
        ),
        className: shareRatioToneClass(
          totals.credited_uploaded_bytes,
          totals.charged_downloaded_bytes
        ),
      }
    : undefined

  if (variant === "menu") {
    const metrics: Array<{
      label: string
      value: string
      className?: string
    }> = []
    if (totals && ratio) {
      metrics.push(
        { label: "分享率", value: ratio.value, className: ratio.className },
        {
          label: "上传",
          value: formatBytes(totals.credited_uploaded_bytes),
        },
        {
          label: "下载",
          value: formatBytes(totals.charged_downloaded_bytes),
        }
      )
    }
    if (economy.data) {
      metrics.push(
        { label: "等级", value: `Lv.${economy.data.progress.level}` },
        {
          label: "魔力值",
          value: formatCompactInteger(economy.data.magic_balance),
          className: "text-warning-foreground",
        }
      )
    }
    if (metrics.length === 0) return null

    return (
      <dl
        aria-label="账户汇总"
        className={cn("grid grid-cols-3 gap-x-2 gap-y-2", className)}
      >
        {metrics.map((metric) => (
          <div
            key={metric.label}
            className="flex min-w-0 flex-col gap-0.5 rounded-md bg-accent px-2 py-1.5"
          >
            <dt className="text-2xs text-faint">{metric.label}</dt>
            <dd
              className={cn(
                "truncate font-mono text-xs font-medium text-foreground tabular-nums",
                metric.className
              )}
            >
              {metric.value}
            </dd>
          </div>
        ))}
      </dl>
    )
  }

  if (trafficPending && economyPending) {
    return (
      <div
        className={cn("grid w-[13.75rem] grid-cols-2 gap-x-4", className)}
        aria-label="正在加载账户汇总"
        aria-busy="true"
      >
        <HeaderSummarySkeleton rows={3} />
        <HeaderSummarySkeleton rows={2} />
      </div>
    )
  }

  return (
    <div
      className={cn("flex items-start gap-4 text-[11px] leading-4", className)}
    >
      {totals && ratio ? (
        <dl className="flex w-[7.375rem] flex-col gap-0.5">
          <HeaderMetric
            label="分享率"
            value={ratio.value}
            valueClassName={ratio.className}
          />
          <HeaderMetric
            icon={<ArrowUpIcon className="text-success-foreground" />}
            label="上传"
            value={formatBytes(totals.credited_uploaded_bytes)}
          />
          <HeaderMetric
            icon={<ArrowDownIcon className="text-destructive" />}
            label="下载"
            value={formatBytes(totals.charged_downloaded_bytes)}
          />
        </dl>
      ) : trafficPending ? (
        <HeaderSummarySkeleton rows={3} />
      ) : null}

      {economy.data ? (
        <dl className="flex w-[6.375rem] flex-col gap-0.5">
          <HeaderMetric
            label="等级"
            value={`Lv.${economy.data.progress.level}`}
          />
          <HeaderMetric
            label="魔力值"
            value={formatCompactInteger(economy.data.magic_balance)}
            fullValue={`${formatInteger(economy.data.magic_balance)} 魔力值`}
            valueClassName="text-warning-foreground"
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
  icon?: ReactNode
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
    <div className="flex items-center gap-1">
      <dt className="flex shrink-0 items-center gap-1 text-muted-foreground">
        {icon ? (
          <span className="[&>svg]:size-3" aria-hidden="true">
            {icon}
          </span>
        ) : null}
        <span>{label}</span>
      </dt>
      <dd className="ml-auto min-w-0 text-right font-mono font-medium text-foreground tabular-nums">
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
    <div className="flex w-[6.375rem] flex-col gap-1.5" aria-hidden="true">
      {Array.from({ length: rows }, (_, index) => (
        <Skeleton key={index} className="h-2.5 w-full" />
      ))}
    </div>
  )
}
