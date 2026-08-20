import { ForgotPasswordPage } from "~/features/auth/pages/forgot-password-page"

export function meta() {
  return [{ title: "找回密码 · PeerGo" }]
}

export default function ForgotPasswordRoute() {
  return <ForgotPasswordPage />
}
