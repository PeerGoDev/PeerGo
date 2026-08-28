import { useNavigate, useParams } from "react-router"

import { StaffUsersPage } from "~/features/staff/pages/staff-users-page"

export function StaffUserDetailPage() {
  const { userId } = useParams()
  const navigate = useNavigate()

  return (
    <StaffUsersPage
      initialSelectedUserId={userId}
      onDialogClose={() => navigate("/staff/users", { replace: true })}
    />
  )
}
