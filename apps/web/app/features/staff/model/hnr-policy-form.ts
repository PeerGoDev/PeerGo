import type { components } from "~/generated/api"

export type HNRPolicyInput = components["schemas"]["HNRPolicyInput"]
export type HNRPolicySettings = components["schemas"]["HNRPolicySettings"]

export type HNRPolicyDraft = {
  mode: HNRPolicyInput["mode"]
  seedHours: string
  ratio: string
  assessmentDays: string
  graceHours: string
  intervalMinutes: string
}

const defaultDraft: HNRPolicyDraft = {
  mode: "enforced",
  seedHours: "72",
  ratio: "1",
  assessmentDays: "7",
  graceHours: "24",
  intervalMinutes: "60",
}

export function hnrPolicyDraftFromCurrent(
  current: HNRPolicySettings
): HNRPolicyDraft {
  if (!current.configured || current.mode === "") return { ...defaultDraft }
  return {
    mode: current.mode,
    seedHours: displayScaled(current.required_seed_seconds, 3600),
    ratio: displayScaled(current.required_ratio_basis_points, 10_000),
    assessmentDays: displayScaled(current.assessment_window_seconds, 86_400),
    graceHours: displayScaled(current.grace_period_seconds, 3600),
    intervalMinutes: displayScaled(current.max_interval_credit_seconds, 60),
  }
}

export function hnrPolicyFromDraft(
  draft: HNRPolicyDraft
): HNRPolicyInput | undefined {
  if (draft.mode === "disabled" || draft.mode === "exempt") {
    return {
      mode: draft.mode,
      required_seed_seconds: 0,
      required_ratio_basis_points: 0,
      assessment_window_seconds: 0,
      grace_period_seconds: 0,
      max_interval_credit_seconds: 0,
    }
  }

  const requiredSeedSeconds = scaledInteger(
    draft.seedHours,
    3600,
    0,
    315_360_000
  )
  const requiredRatioBasisPoints = scaledInteger(
    draft.ratio,
    10_000,
    0,
    1_000_000
  )
  const assessmentWindowSeconds = scaledInteger(
    draft.assessmentDays,
    86_400,
    1,
    315_360_000
  )
  const gracePeriodSeconds = scaledInteger(
    draft.graceHours,
    3600,
    0,
    31_536_000
  )
  const maxIntervalCreditSeconds = scaledInteger(
    draft.intervalMinutes,
    60,
    60,
    86_400
  )
  if (
    requiredSeedSeconds === undefined ||
    requiredRatioBasisPoints === undefined ||
    assessmentWindowSeconds === undefined ||
    gracePeriodSeconds === undefined ||
    maxIntervalCreditSeconds === undefined ||
    (requiredSeedSeconds === 0 && requiredRatioBasisPoints === 0) ||
    assessmentWindowSeconds < requiredSeedSeconds
  ) {
    return undefined
  }
  return {
    mode: "enforced",
    required_seed_seconds: requiredSeedSeconds,
    required_ratio_basis_points: requiredRatioBasisPoints,
    assessment_window_seconds: assessmentWindowSeconds,
    grace_period_seconds: gracePeriodSeconds,
    max_interval_credit_seconds: maxIntervalCreditSeconds,
  }
}

function displayScaled(value: number, scale: number) {
  return Number((value / scale).toFixed(6)).toString()
}

function scaledInteger(
  raw: string,
  scale: number,
  minimum: number,
  maximum: number
) {
  if (raw.trim() === "") return undefined
  const value = Number(raw)
  const scaled = value * scale
  if (
    !Number.isFinite(value) ||
    !Number.isSafeInteger(scaled) ||
    scaled < minimum ||
    scaled > maximum
  ) {
    return undefined
  }
  return scaled
}
