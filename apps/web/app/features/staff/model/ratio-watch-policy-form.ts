import type { RatioWatchPolicyInput } from "~/features/staff/api/ratio-watch-administration.queries"

export type RatioWatchPolicyDraft = {
  enabled: boolean
  thresholdGiB: string
  minimumRatio: string
  watchDays: string
  restrictionRatio: string
}

const defaultDraft: RatioWatchPolicyDraft = {
  enabled: true,
  thresholdGiB: "50",
  minimumRatio: "0.4",
  watchDays: "14",
  restrictionRatio: "0.3",
}

export function ratioWatchDraftFromCurrent(
  current: RatioWatchPolicyInput | null
): RatioWatchPolicyDraft {
  if (!current) return { ...defaultDraft }
  if (!current.enabled) return { ...defaultDraft, enabled: false }
  return {
    enabled: true,
    thresholdGiB: displayGiB(current.download_threshold_bytes),
    minimumRatio: displayScaled(current.minimum_ratio_basis_points, 10_000),
    watchDays: displayScaled(current.watch_period_seconds, 86_400),
    restrictionRatio: displayScaled(
      current.restriction_ratio_basis_points,
      10_000
    ),
  }
}

export function ratioWatchPolicyFromDraft(
  draft: RatioWatchPolicyDraft
): RatioWatchPolicyInput | undefined {
  if (!draft.enabled) {
    return {
      enabled: false,
      download_threshold_bytes: "0",
      minimum_ratio_basis_points: 0,
      watch_period_seconds: 0,
      restriction_ratio_basis_points: 0,
    }
  }
  const threshold = integerGiB(draft.thresholdGiB)
  const minimumRatio = scaledInteger(draft.minimumRatio, 10_000)
  const watchDays = scaledInteger(draft.watchDays, 86_400)
  const restrictionRatio = scaledInteger(draft.restrictionRatio, 10_000)
  if (
    threshold === undefined ||
    minimumRatio === undefined ||
    watchDays === undefined ||
    restrictionRatio === undefined ||
    threshold < 1_073_741_824n ||
    threshold > 9_000_000_000_000_000_000n ||
    minimumRatio < 1 ||
    minimumRatio > 1_000_000 ||
    restrictionRatio < 1 ||
    restrictionRatio > minimumRatio ||
    watchDays < 86_400 ||
    watchDays > 31_536_000
  ) {
    return undefined
  }
  return {
    enabled: true,
    download_threshold_bytes: threshold.toString(),
    minimum_ratio_basis_points: minimumRatio,
    watch_period_seconds: watchDays,
    restriction_ratio_basis_points: restrictionRatio,
  }
}

function integerGiB(raw: string) {
  if (!/^\d+$/.test(raw.trim())) return undefined
  try {
    return BigInt(raw.trim()) * 1_073_741_824n
  } catch {
    return undefined
  }
}

function scaledInteger(raw: string, scale: number) {
  if (raw.trim() === "") return undefined
  const value = Number(raw)
  const scaled = value * scale
  if (!Number.isFinite(value) || !Number.isSafeInteger(scaled)) return undefined
  return scaled
}

function displayGiB(raw: string) {
  try {
    const bytes = BigInt(raw)
    const gib = 1_073_741_824n
    return bytes % gib === 0n ? (bytes / gib).toString() : raw
  } catch {
    return raw
  }
}

function displayScaled(value: number, scale: number) {
  return Number((value / scale).toFixed(6)).toString()
}
