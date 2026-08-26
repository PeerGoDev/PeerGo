import { PersonalAPIKeyPage } from "~/features/auth/pages/personal-api-key-page"

export function meta() {
  return [{ title: "API Key · PeerGo" }]
}

export default function AccountAPIKeyRoute() {
  return <PersonalAPIKeyPage />
}
