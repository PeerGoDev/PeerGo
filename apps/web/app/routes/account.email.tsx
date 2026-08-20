import { EmailVerificationPage } from "~/features/auth/pages/email-verification-page"

export function meta() {
  return [{ title: "验证邮箱 · PeerGo" }]
}

export default function AccountEmailRoute() {
  return <EmailVerificationPage />
}
