import { EmailVerificationConfirmationPage } from "~/features/auth/pages/email-verification-confirmation-page"

export function meta() {
  return [{ title: "确认邮箱 · PeerGo" }]
}

export default function VerifyEmailRoute() {
  return <EmailVerificationConfirmationPage />
}
