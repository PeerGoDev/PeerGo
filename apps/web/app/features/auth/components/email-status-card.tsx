import { CircleCheckIcon, CircleUserRoundIcon } from "lucide-react"
import { Link } from "react-router"

import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import { cn } from "~/lib/utils"

export function EmailStatusCard({ verified }: { verified: boolean }) {
  return (
    <Card
      className={cn(
        "gap-0 py-0",
        verified ? "border-success/30" : "border-warning/30"
      )}
    >
      <CardHeader className="px-6 pt-6 pb-6">
        <CardTitle>
          <h2 className="flex items-center gap-2 text-2xl leading-none font-semibold">
            {verified ? (
              <CircleCheckIcon className="size-6 text-success" />
            ) : (
              <CircleUserRoundIcon className="size-6 text-warning" />
            )}
            {verified ? "邮箱已验证" : "邮箱待验证"}
          </h2>
        </CardTitle>
      </CardHeader>
      <CardContent className="px-6 pb-6 text-sm text-muted-foreground">
        {verified ? (
          "您的邮箱已完成验证，可以正常使用需要验证邮箱的功能。"
        ) : (
          <>
            完成邮箱验证后，可以使用邮箱登录并使用受保护功能。{" "}
            <Link to="/account/email">前往验证</Link>
          </>
        )}
      </CardContent>
    </Card>
  )
}
