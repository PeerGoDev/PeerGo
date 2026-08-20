import { Navigate, useLocation } from "react-router"

import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useMyWorkgroups } from "~/features/workgroups/api/workgroups.queries"
import { PageLayout } from "~/shared/components/page-layout"

/**
 * Preserves the PtYes review-center entry for every signed-in member.
 * Reviewers enter the protected queue; regular members land on their own
 * submissions instead of being sent through the staff WebAuthn gate.
 */
export default function LegacyReviewRoute() {
  const location = useLocation()
  const session = useWebSession()
  const workgroups = useMyWorkgroups(session.data?.user.id)

  if (session.isPending || (session.data && workgroups.isPending)) {
    return (
      <PageLayout className="gap-4">
        <Skeleton className="h-10 w-full max-w-lg" />
        <Skeleton className="h-44 w-full" />
      </PageLayout>
    )
  }

  if (!session.data) {
    return <Navigate to="/login" replace />
  }

  const canOpenReviewQueue = Boolean(
    workgroups.data?.items.some(
      (item) =>
        item.definition.kind === "review" &&
        item.membership?.status === "active"
    )
  )
  const destination = canOpenReviewQueue
    ? "/review/queue"
    : "/account/submissions"
  return <Navigate to={`${destination}${location.search}`} replace />
}
