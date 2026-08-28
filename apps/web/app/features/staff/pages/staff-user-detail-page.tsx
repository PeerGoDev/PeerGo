import { Link, useParams } from "react-router"
import { ArrowLeftIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { ManagedUserDetailPanel } from "~/features/staff/components/managed-user-detail-sheet"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"

export function StaffUserDetailPage() {
  const { userId } = useParams()

  return (
    <StaffAccessGate
      requiredAction="user.account.read"
      pageHeader={{
        title: "用户详情",
        description: "查看并管理账户身份、运营数据、网络历史与访问状态。",
      }}
    >
      {({ session, capabilities }) => (
        <StaffPageFrame className="gap-4">
          <Button
            render={<Link to="/staff/users" />}
            nativeButton={false}
            variant="outline"
            size="sm"
            className="self-start"
          >
            <ArrowLeftIcon data-icon="inline-start" />
            返回用户列表
          </Button>

          {!userId ? (
            <Alert variant="destructive">
              <AlertTitle>缺少用户标识</AlertTitle>
              <AlertDescription>
                请从用户列表重新打开账户详情。
              </AlertDescription>
            </Alert>
          ) : (
            <ManagedUserDetailPanel
              userId={userId}
              csrfToken={session.csrf_token}
              currentStaffUserId={session.user.id}
              canRestrict={hasCapability(capabilities, "user.account.restrict")}
              canRevoke={hasCapability(
                capabilities,
                "user.account.restriction.revoke"
              )}
              canDownloadRestrict={hasCapability(
                capabilities,
                "user.downloadrestriction.restrict"
              )}
              canDownloadRevoke={hasCapability(
                capabilities,
                "user.downloadrestriction.revoke"
              )}
              canManageVIP={hasCapability(capabilities, "user.vip.manage")}
              canAssignAssessment={hasCapability(
                capabilities,
                "newcomer.assessment.assign"
              )}
              canAdjustData={hasCapability(capabilities, "user.account.adjust")}
              canReadNetworkHistory={hasCapability(
                capabilities,
                "user.network.read"
              )}
            />
          )}
        </StaffPageFrame>
      )}
    </StaffAccessGate>
  )
}
