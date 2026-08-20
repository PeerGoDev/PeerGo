import { RSSSubscriptionsPage } from "~/features/rss/pages/rss-subscriptions-page"

export function meta() {
  return [{ title: "RSS 订阅 · PeerGo" }]
}

export default function AccountRSSRoute() {
  return <RSSSubscriptionsPage />
}
