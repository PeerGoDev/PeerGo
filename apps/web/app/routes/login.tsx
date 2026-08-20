import { LoginPage } from "~/features/auth/pages/login-page"

export function meta() {
  return [{ title: "登录 · PeerGo" }]
}

export default function LoginRoute() {
  return <LoginPage />
}
