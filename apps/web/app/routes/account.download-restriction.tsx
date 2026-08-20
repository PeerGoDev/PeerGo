import { DownloadRestrictionPage } from "~/features/identity/pages/download-restriction-page"

export function meta() {
  return [{ title: "下载限制 · PeerGo" }]
}

export default function AccountDownloadRestrictionRoute() {
  return <DownloadRestrictionPage />
}
