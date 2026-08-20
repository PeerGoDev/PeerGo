import { Navigate, useLocation, useParams } from "react-router"

/**
 * Redirects the public PtYes torrent path to PeerGo's canonical route. Torrent
 * Numeric IDs are passed through as path data only; the destination still
 * performs strict positive-integer validation before issuing an API request.
 */
export default function LegacyTorrentRoute() {
  const location = useLocation()
  const { torrentId = "" } = useParams()
  return (
    <Navigate
      to={`/torrents/${encodeURIComponent(torrentId)}${location.search}`}
      replace
    />
  )
}
