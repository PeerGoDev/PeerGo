import * as React from "react"
import { useLocation } from "react-router"

export function useFragmentToken(parameter = "token") {
  const location = useLocation()
  const [token, setToken] = React.useState(() =>
    tokenFromFragment(location.hash, parameter)
  )

  React.useEffect(() => {
    const fragment = new URLSearchParams(location.hash.slice(1))
    setToken(fragment.get(parameter) ?? "")

    if (!fragment.has(parameter)) return

    // Fragments are not sent to the server, but removing the bearer credential
    // also keeps it out of screenshots and copied URLs. Preserve Router's
    // history metadata so back/forward navigation continues to work normally.
    globalThis.history.replaceState(
      globalThis.history.state,
      "",
      location.pathname + location.search
    )
  }, [location.hash, location.pathname, location.search, parameter])

  return token
}

function tokenFromFragment(hash: string, parameter: string) {
  return new URLSearchParams(hash.slice(1)).get(parameter) ?? ""
}
