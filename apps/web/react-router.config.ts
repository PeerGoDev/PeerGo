import type { Config } from "@react-router/dev/config"

export default {
  // PeerGo is an authenticated product application. The first release ships as
  // static SPA assets so the web process cannot expand the Core API fault domain.
  ssr: false,
} satisfies Config
