import { Navigate, useLocation } from "react-router"

/** Preserves the PtYes message deep link without duplicating notification UI. */
export default function LegacyMessagesRoute() {
  const location = useLocation()
  return <Navigate to={`/notifications${location.search}`} replace />
}
