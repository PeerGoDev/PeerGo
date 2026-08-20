import { HitAndRunPage } from "~/features/traffic/pages/hit-and-run-page"

export function meta() {
  return [{ title: "H&R · PeerGo" }]
}

export default function AccountHNRRoute() {
  return <HitAndRunPage />
}
