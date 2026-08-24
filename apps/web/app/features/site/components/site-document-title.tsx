import { useEffect, useRef } from "react"
import { useLocation } from "react-router"

import { useSiteInfo } from "~/features/site/api/site.queries"

const defaultSiteName = "PeerGo"
const titleSeparator = " · "
const titleContexts = ["", " Staff", " 管理后台"] as const

export function SiteDocumentTitle() {
  const location = useLocation()
  const siteInfo = useSiteInfo()
  const previousSiteName = useRef(defaultSiteName)
  const siteName = siteInfo.data?.name.trim()

  useEffect(() => {
    if (!siteName) return

    const nextTitle = formatSiteDocumentTitle(
      document.title,
      siteName,
      previousSiteName.current
    )
    if (nextTitle !== document.title) document.title = nextTitle
    previousSiteName.current = siteName
  }, [location.pathname, location.search, siteName])

  return null
}

export function formatSiteDocumentTitle(
  currentTitle: string,
  siteName: string,
  previousSiteName = defaultSiteName
) {
  const nextSiteName = siteName.trim() || defaultSiteName
  const knownSiteNames = new Set([
    defaultSiteName,
    previousSiteName.trim() || defaultSiteName,
  ])

  for (const knownSiteName of knownSiteNames) {
    for (const context of titleContexts) {
      const currentSuffix = `${knownSiteName}${context}`
      const nextSuffix = `${nextSiteName}${context}`

      if (currentTitle === currentSuffix) return nextSuffix
      if (currentTitle.endsWith(`${titleSeparator}${currentSuffix}`)) {
        return `${currentTitle.slice(0, -currentSuffix.length)}${nextSuffix}`
      }
    }
  }

  return currentTitle
}
