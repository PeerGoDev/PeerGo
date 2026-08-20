export function formatInteger(value: bigint | number | string) {
  const integer = exactInteger(value)
  return integer === undefined ? "—" : integer.toLocaleString("zh-CN")
}

const compactIntegerUnits = [
  { divisor: 1_000n, suffix: "K" },
  { divisor: 1_000_000n, suffix: "M" },
  { divisor: 1_000_000_000n, suffix: "B" },
  { divisor: 1_000_000_000_000n, suffix: "T" },
] as const

/**
 * Format an exact integer for narrow summary surfaces without converting it
 * to Number. The one-decimal K/M/B/T notation mirrors the legacy header while
 * keeping migrated balances above Number.MAX_SAFE_INTEGER precise.
 */
export function formatCompactInteger(value: bigint | number | string) {
  const integer = exactInteger(value)
  if (integer === undefined) return "—"

  const absolute = integer < 0n ? -integer : integer
  if (absolute < compactIntegerUnits[0].divisor) {
    return integer.toLocaleString("zh-CN")
  }

  let unitIndex = 0
  for (let index = compactIntegerUnits.length - 1; index >= 0; index -= 1) {
    if (absolute >= compactIntegerUnits[index].divisor) {
      unitIndex = index
      break
    }
  }
  let unit = compactIntegerUnits[unitIndex]
  let roundedTenths = roundToTenths(absolute, unit.divisor)

  // Promote values such as 999,999 to 1.0M instead of rendering 1000.0K.
  if (roundedTenths >= 10_000n && unitIndex < compactIntegerUnits.length - 1) {
    unitIndex += 1
    unit = compactIntegerUnits[unitIndex]
    roundedTenths = roundToTenths(absolute, unit.divisor)
  }

  const sign = integer < 0n ? "-" : ""
  return `${sign}${roundedTenths / 10n}.${roundedTenths % 10n}${unit.suffix}`
}

function exactInteger(value: bigint | number | string) {
  if (typeof value === "bigint") return value
  if (typeof value === "number") {
    return Number.isSafeInteger(value) ? BigInt(value) : undefined
  }
  return /^-?(0|[1-9][0-9]*)$/.test(value) ? BigInt(value) : undefined
}

function roundToTenths(value: bigint, divisor: bigint) {
  return (value * 10n + divisor / 2n) / divisor
}
