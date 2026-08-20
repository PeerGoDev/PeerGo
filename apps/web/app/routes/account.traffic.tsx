import { TrafficPage } from "~/features/traffic/pages/traffic-page"

export function meta() {
  return [{ title: "我的流量 · PeerGo" }]
}

export default function AccountTrafficRoute() {
  return <TrafficPage />
}
