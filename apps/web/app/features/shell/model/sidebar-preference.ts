export const legacySidebarCollapsedKey = "user_sidebar_collapsed"

const sidebarStateCookie = "sidebar_state"

/**
 * Restores the desktop rail preference without coupling the app shell to a
 * browser-only storage API. PtYes used localStorage while the shadcn sidebar
 * primitive writes a cookie, so PeerGo accepts both during the transition.
 */
export function readDesktopSidebarOpen(
  storage: Pick<Storage, "getItem"> | undefined = safeLocalStorage(),
  cookie = safeDocumentCookie()
) {
  try {
    const collapsed = storage?.getItem(legacySidebarCollapsedKey)
    if (collapsed === "true") return false
    if (collapsed === "false") return true
  } catch {
    // Privacy modes can deny storage access; the cookie/default still works.
  }

  const cookieState = cookie
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(`${sidebarStateCookie}=`))
    ?.slice(sidebarStateCookie.length + 1)
  if (cookieState === "false") return false
  if (cookieState === "true") return true
  return true
}

export function writeDesktopSidebarOpen(
  open: boolean,
  storage: Pick<Storage, "setItem"> | undefined = safeLocalStorage()
) {
  try {
    storage?.setItem(legacySidebarCollapsedKey, String(!open))
  } catch {
    // The sidebar remains usable when persistent storage is unavailable.
  }
}

function safeLocalStorage() {
  try {
    return globalThis.localStorage
  } catch {
    return undefined
  }
}

function safeDocumentCookie() {
  try {
    return globalThis.document?.cookie ?? ""
  } catch {
    return ""
  }
}
