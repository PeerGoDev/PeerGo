import { RegistrationPage } from "~/features/auth/pages/registration-page"

export function meta() {
  return [{ title: "注册 · PeerGo" }]
}

export default function RegisterRoute() {
  return <RegistrationPage />
}
