import { exactNonNegativeInteger } from "~/shared/formatters/bytes"

const minuteSeconds = 60n
const hourSeconds = 60n * minuteSeconds
const daySeconds = 24n * hourSeconds

export function formatHNRDuration(value: string) {
  const seconds = exactNonNegativeInteger(value)
  if (seconds === undefined) return "—"
  if (seconds === 0n) return "0 分钟"

  const days = seconds / daySeconds
  const hours = (seconds % daySeconds) / hourSeconds
  const minutes = (seconds % hourSeconds) / minuteSeconds
  if (days > 0n) {
    return hours > 0n ? `${days} 天 ${hours} 小时` : `${days} 天`
  }
  if (hours > 0n) {
    return minutes > 0n ? `${hours} 小时 ${minutes} 分钟` : `${hours} 小时`
  }
  return `${minutes > 0n ? minutes : 1n} 分钟`
}

export function formatHNRRatio(value: string) {
  const basisPoints = exactNonNegativeInteger(value)
  if (basisPoints === undefined) return "—"
  const hundredths = (basisPoints + 50n) / 100n
  const whole = hundredths / 100n
  const fraction = (hundredths % 100n).toString().padStart(2, "0")
  return `${whole.toLocaleString("zh-CN")}.${fraction}`
}

export function hnrProgressPercent(value: string, required: string) {
  const current = exactNonNegativeInteger(value)
  const target = exactNonNegativeInteger(required)
  if (current === undefined || target === undefined) return 0
  if (target === 0n) return 100
  const hundredths = ((current > target ? target : current) * 10_000n) / target
  return Number(hundredths) / 100
}

export function formatHNRCount(...values: string[]) {
  let total = 0n
  for (const value of values) {
    const parsed = exactNonNegativeInteger(value)
    if (parsed === undefined) return "—"
    total += parsed
  }
  return total.toLocaleString("zh-CN")
}
