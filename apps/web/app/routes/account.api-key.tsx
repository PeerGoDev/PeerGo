import { MoviePilotCredentialPage } from "~/features/auth/pages/moviepilot-credential-page"

export function meta() {
  return [{ title: "API Key · PeerGo" }]
}

export default function AccountAPIKeyRoute() {
  return <MoviePilotCredentialPage />
}
