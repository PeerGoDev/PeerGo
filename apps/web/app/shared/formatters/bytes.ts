const byteUnits = ["B", "KB", "MB", "GB", "TB", "PB", "EB"] as const

export function formatBytes(value: number | bigint | string) {
  const bytes = exactNonNegativeInteger(value)
  if (bytes === undefined) {
    return "—"
  }

  let unitIndex = 0
  let unitSize = 1n
  while (bytes >= unitSize * 1024n && unitIndex < byteUnits.length - 1) {
    unitSize *= 1024n
    unitIndex += 1
  }

  const whole = bytes / unitSize
  const decimalPlaces = unitIndex === 0 ? 0 : whole >= 10n ? 1 : 2
  const decimalScale = decimalPlaces === 0 ? 1n : 10n ** BigInt(decimalPlaces)
  const rounded = (bytes * decimalScale + unitSize / 2n) / unitSize
  const numericValue = Number(rounded) / Number(decimalScale)

  return `${new Intl.NumberFormat("zh-CN", {
    maximumFractionDigits: decimalPlaces,
  }).format(numericValue)} ${byteUnits[unitIndex]}`
}

export function exactNonNegativeInteger(
  value: number | bigint | string
): bigint | undefined {
  if (typeof value === "bigint") {
    return value >= 0n ? value : undefined
  }
  if (typeof value === "number") {
    return Number.isSafeInteger(value) && value >= 0 ? BigInt(value) : undefined
  }
  if (!/^(0|[1-9][0-9]*)$/.test(value)) {
    return undefined
  }
  return BigInt(value)
}
