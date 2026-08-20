import { Navigate, useLocation } from "react-router"

/** Preserves the PtYes administration entry without duplicating staff pages. */
export default function LegacyAdminRoute() {
  const location = useLocation()
  return <Navigate to={`/staff${location.search}`} replace />
}
