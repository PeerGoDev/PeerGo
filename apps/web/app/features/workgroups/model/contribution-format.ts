import type { components } from "~/generated/api"

type Metric = components["schemas"]["WorkgroupContributionProgress"]["metric"]

export function contributionMetricLabel(metric: Metric) {
  switch (metric) {
    case "trusted_torrents_published":
      return "本月可信发布"
    case "torrent_review_votes":
      return "本月有效审核"
    case "seeding_active_seconds":
      return "本月有效做种"
  }
}

export function formatContributionValue(metric: Metric, value: number) {
  if (metric === "trusted_torrents_published") return `${value} 个种子`
  if (metric === "torrent_review_votes") return `${value} 票`
  return formatSeconds(value)
}

export function contributionPercent(current: number, target: number) {
  if (!Number.isFinite(current) || !Number.isFinite(target) || target <= 0) {
    return 0
  }
  return Math.min(100, Math.max(0, Math.round((current / target) * 100)))
}

function formatSeconds(value: number) {
  const safe = Math.max(0, Math.floor(value))
  const days = Math.floor(safe / 86400)
  const hours = Math.floor((safe % 86400) / 3600)
  const minutes = Math.floor((safe % 3600) / 60)
  if (days > 0) return hours > 0 ? `${days} 天 ${hours} 小时` : `${days} 天`
  if (hours > 0)
    return minutes > 0 ? `${hours} 小时 ${minutes} 分钟` : `${hours} 小时`
  return `${minutes} 分钟`
}
