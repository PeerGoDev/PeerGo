export function formatRatioBasisPoints(value: number) {
  if (!Number.isSafeInteger(value) || value < 0) return "—"
  return new Intl.NumberFormat("zh-CN", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(value / 10_000)
}

export function formatWatchPeriod(seconds: number) {
  if (!Number.isSafeInteger(seconds) || seconds < 0) return "—"
  const days = seconds / 86_400
  if (Number.isInteger(days)) return `${days} 天`
  const hours = seconds / 3_600
  return Number.isInteger(hours) ? `${hours} 小时` : `${seconds} 秒`
}

export function formatDeadlineRemaining(deadline: string, observedAt: string) {
  const deadlineTime = Date.parse(deadline)
  const observedTime = Date.parse(observedAt)
  if (!Number.isFinite(deadlineTime) || !Number.isFinite(observedTime)) {
    return "剩余时间未知"
  }
  const seconds = Math.max(0, Math.ceil((deadlineTime - observedTime) / 1000))
  if (seconds === 0) return "观察期已结束"
  const days = Math.floor(seconds / 86_400)
  const hours = Math.floor((seconds % 86_400) / 3_600)
  if (days > 0) return `还剩 ${days} 天 ${hours} 小时`
  const minutes = Math.max(1, Math.ceil(seconds / 60))
  return `还剩 ${Math.floor(minutes / 60)} 小时 ${minutes % 60} 分钟`
}
