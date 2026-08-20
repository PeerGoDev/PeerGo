import { NewcomerAssessmentPage } from "~/features/newcomer/pages/newcomer-assessment-page"

export function meta() {
  return [{ title: "新人考核 · PeerGo" }]
}

export default function AccountAssessmentRoute() {
  return <NewcomerAssessmentPage />
}
