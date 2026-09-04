import { Link } from "react-router"
import { CalendarCheck2Icon } from "lucide-react"

import { Button } from "~/components/ui/button"
import { Spinner } from "~/components/ui/spinner"
import {
  type AttendanceMode,
  useAttendanceOverview,
  useClaimAttendance,
} from "~/features/economy/api/attendance.queries"

export function HeaderAttendanceButton({
  userId,
  csrfToken,
  enabled,
}: {
  userId: string
  csrfToken: string
  enabled: boolean
}) {
  const query = useAttendanceOverview(userId, enabled)
  const mutation = useClaimAttendance(userId)
  const overview = query.data

  if (
    !enabled ||
    query.isPending ||
    query.isError ||
    !overview?.policy?.settings.enabled
  ) {
    return null
  }
  if (overview.claimed_today) {
    return (
      <Button
        render={<Link to="/account/economy" />}
        nativeButton={false}
        variant="ghost"
        size="sm"
        className="hidden h-8 gap-1.5 rounded-full px-2.5 text-success-foreground md:inline-flex"
      >
        <CalendarCheck2Icon />
        已签到 {overview.current_streak} 天
      </Button>
    )
  }

  const settings = overview.policy.settings
  const mode: AttendanceMode = settings.fixed_enabled ? "fixed" : "random"
  return (
    <Button
      variant="outline"
      size="sm"
      className="hidden h-8 rounded-full border-primary/30 bg-primary/5 px-2.5 text-primary hover:bg-primary/10 hover:text-primary md:inline-flex"
      disabled={mutation.isPending}
      onClick={() =>
        mutation.mutate({
          csrfToken,
          mode,
          idempotencyKey: globalThis.crypto.randomUUID(),
        })
      }
      title={
        settings.fixed_enabled && settings.random_enabled
          ? "快捷签到使用固定奖励；可在等级与魔力值页面选择随机奖励"
          : undefined
      }
    >
      {mutation.isPending ? <Spinner /> : <CalendarCheck2Icon />}
      {mutation.isPending ? "签到中" : "签到"}
    </Button>
  )
}
