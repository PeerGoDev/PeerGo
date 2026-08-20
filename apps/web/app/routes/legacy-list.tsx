import { Navigate, useLocation } from "react-router"

/**
 * Keeps imported bookmarks and user-entered PtYes catalog URLs working while
 * the application owns a single canonical catalog route and component tree.
 */
export default function LegacyListRoute() {
  const location = useLocation()
  return <Navigate to={`/torrents${location.search}`} replace />
}
