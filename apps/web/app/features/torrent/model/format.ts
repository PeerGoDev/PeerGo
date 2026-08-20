import { exactNonNegativeInteger } from "~/shared/formatters/bytes"

type TorrentSwarmSnapshot = {
  swarm_observed_at: string
  swarm_stale: boolean
}

export type TorrentSwarmFreshness = "fresh" | "stale" | "unavailable"

export function getTorrentSwarmFreshness(
  torrent: TorrentSwarmSnapshot
): TorrentSwarmFreshness {
  const observedAt = Date.parse(torrent.swarm_observed_at)

  // 旧站迁移只带来了种子元数据。Unix epoch 是导入器写入的“尚未观察”哨兵值，
  // 不能把此时的 0/0/0 当成 Tracker 已确认的真实统计展示给用户。
  if (!Number.isFinite(observedAt) || observedAt <= 0) {
    return "unavailable"
  }

  return torrent.swarm_stale ? "stale" : "fresh"
}

export function formatRelativeTime(value: string, now = Date.now()) {
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) {
    return "时间未知"
  }

  const deltaSeconds = Math.round((timestamp - now) / 1000)
  const formatter = new Intl.RelativeTimeFormat("zh-CN", { numeric: "auto" })
  if (Math.abs(deltaSeconds) < 60) {
    return formatter.format(deltaSeconds, "second")
  }

  const deltaMinutes = Math.round(deltaSeconds / 60)
  if (Math.abs(deltaMinutes) < 60) {
    return formatter.format(deltaMinutes, "minute")
  }

  const deltaHours = Math.round(deltaMinutes / 60)
  if (Math.abs(deltaHours) < 24) {
    return formatter.format(deltaHours, "hour")
  }

  return formatter.format(Math.round(deltaHours / 24), "day")
}

const torrentSizeUnits = ["B", "KB", "MB", "GB", "TB", "PB", "EB"] as const

export type TorrentSizeTone = "muted" | "blue" | "green" | "purple" | "red"

export function formatTorrentSizeParts(
  value: number | bigint | string
): { value: string; unit: string; tone: TorrentSizeTone } | undefined {
  const bytes = exactNonNegativeInteger(value)
  if (bytes === undefined) return undefined

  let unitIndex = 0
  let unitSize = 1n
  while (bytes >= unitSize * 1024n && unitIndex < torrentSizeUnits.length - 1) {
    unitSize *= 1024n
    unitIndex += 1
  }

  // PtYes torrent surfaces keep at most two decimals and remove trailing
  // zeroes. This is separate from the compact formatter used by traffic data.
  const decimalPlaces = unitIndex >= 2 ? 2 : 0
  const decimalScale = 10n ** BigInt(decimalPlaces)
  const rounded = (bytes * decimalScale + unitSize / 2n) / unitSize
  const numericValue = Number(rounded) / Number(decimalScale)
  const tone: TorrentSizeTone =
    unitIndex <= 1
      ? "muted"
      : unitIndex === 2
        ? "blue"
        : unitIndex === 3
          ? "green"
          : unitIndex === 4
            ? "purple"
            : "red"

  return {
    value: new Intl.NumberFormat("zh-CN", {
      maximumFractionDigits: decimalPlaces,
    }).format(numericValue),
    unit: torrentSizeUnits[unitIndex],
    tone,
  }
}

export function formatTorrentSize(value: number | bigint | string) {
  const size = formatTorrentSizeParts(value)
  return size ? `${size.value} ${size.unit}` : "—"
}
