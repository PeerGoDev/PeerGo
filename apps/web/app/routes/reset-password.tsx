import { ResetPasswordPage } from "~/features/auth/pages/reset-password-page"

export function meta() {
  return [{ title: "重置密码 · PeerGo" }]
}

export default function ResetPasswordRoute() {
  return <ResetPasswordPage />
}
