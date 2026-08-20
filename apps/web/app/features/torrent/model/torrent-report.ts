import type { TorrentReportReasonCode } from "~/features/torrent/api/torrent-report.mutations"

export const torrentReportReasonOptions: Array<{
  value: TorrentReportReasonCode
  label: string
}> = [
  { value: "content_mismatch", label: "虚假种子 / 内容与描述不符" },
  { value: "copyright", label: "侵权或站点规则问题" },
  { value: "duplicate_or_spam", label: "重复种子 / 垃圾内容" },
  { value: "malicious", label: "恶意文件 / 安全风险" },
  { value: "other", label: "其他原因" },
]

export function torrentReportReasonLabel(value: string) {
  if (value === "no_violation") return "确认无违规"
  return (
    torrentReportReasonOptions.find((option) => option.value === value)
      ?.label ?? value
  )
}
