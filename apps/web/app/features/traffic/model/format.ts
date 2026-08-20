import type { components } from "~/generated/api"

import { exactNonNegativeInteger } from "~/shared/formatters/bytes"

type TrafficEntry = components["schemas"]["TrafficEntry"]
type TrafficAdjustment = Pick<
  TrafficEntry,
  | "raw_uploaded_bytes"
  | "raw_downloaded_bytes"
  | "credited_uploaded_bytes"
  | "charged_downloaded_bytes"
>

export function formatShareRatio(
  uploaded: string,
  downloaded: string,
  fractionDigits = 2
) {
  const uploadBytes = exactNonNegativeInteger(uploaded)
  const downloadBytes = exactNonNegativeInteger(downloaded)
  if (uploadBytes === undefined || downloadBytes === undefined) {
    return "—"
  }
  if (downloadBytes === 0n) {
    return uploadBytes === 0n ? "—" : "∞"
  }

  const scale = 10n ** BigInt(fractionDigits)
  const scaled = (uploadBytes * scale + downloadBytes / 2n) / downloadBytes
  return new Intl.NumberFormat("zh-CN", {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  }).format(Number(scaled) / 10 ** fractionDigits)
}

export function trafficAdjustmentLabels(entry: TrafficAdjustment) {
  const rawUploaded = exactNonNegativeInteger(entry.raw_uploaded_bytes)
  const creditedUploaded = exactNonNegativeInteger(
    entry.credited_uploaded_bytes
  )
  const rawDownloaded = exactNonNegativeInteger(entry.raw_downloaded_bytes)
  const chargedDownloaded = exactNonNegativeInteger(
    entry.charged_downloaded_bytes
  )
  const labels: string[] = []

  if (
    rawUploaded !== undefined &&
    creditedUploaded !== undefined &&
    rawUploaded > 0n &&
    creditedUploaded !== rawUploaded
  ) {
    labels.push(`上传 ${formatFactor(creditedUploaded, rawUploaded, "×")}`)
  }
  if (
    rawDownloaded !== undefined &&
    chargedDownloaded !== undefined &&
    rawDownloaded > 0n &&
    chargedDownloaded !== rawDownloaded
  ) {
    labels.push(
      chargedDownloaded === 0n
        ? "免费下载"
        : `下载 ${formatFactor(chargedDownloaded, rawDownloaded, "%")}`
    )
  }
  return labels
}

function formatFactor(
  numerator: bigint,
  denominator: bigint,
  suffix: "×" | "%"
) {
  if (suffix === "%") {
    const tenths = (numerator * 1000n + denominator / 2n) / denominator
    return `${Number(tenths) / 10}%`
  }
  const hundredths = (numerator * 100n + denominator / 2n) / denominator
  return `${Number(hundredths) / 100}×`
}
