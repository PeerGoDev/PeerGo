import type { ReactNode } from "react"

import {
  Card,
  CardAction,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { cn } from "~/lib/utils"

type MetricCardProps = {
  title: ReactNode
  value: ReactNode
  description?: ReactNode
  icon?: ReactNode
  tone?: "default" | "positive" | "primary" | "warning" | "muted"
}

export function MetricCard({
  title,
  value,
  description,
  icon,
  tone = "default",
}: MetricCardProps) {
  return (
    <Card size="sm" className="min-h-32 gap-0 py-0">
      <CardHeader className="px-4 pt-4 pb-0 sm:px-5">
        <CardTitle className="text-xs font-medium tracking-normal text-muted-foreground">
          {title}
        </CardTitle>
        {icon ? (
          <CardAction
            className={cn(
              "flex size-8 items-center justify-center rounded-lg border bg-muted text-muted-foreground [&>svg]:size-4",
              tone === "positive" &&
                "border-success/20 bg-success/10 text-success-foreground",
              tone === "primary" && "border-primary/20 bg-primary/10 text-link",
              tone === "warning" &&
                "border-warning/20 bg-warning/10 text-warning-foreground"
            )}
            aria-hidden="true"
          >
            {icon}
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className="px-4 pt-1 pb-0 sm:px-5">
        <p
          className={cn(
            "font-mono text-2xl font-semibold tracking-tight tabular-nums",
            tone === "positive" && "text-success-foreground",
            tone === "primary" && "text-link",
            tone === "warning" && "text-warning-foreground",
            tone === "muted" && "text-muted-foreground"
          )}
        >
          {value}
        </p>
      </CardContent>
      {description ? (
        <CardFooter className="border-0 bg-transparent px-4 pt-1 pb-4 sm:px-5">
          <p className="truncate text-xs text-faint">{description}</p>
        </CardFooter>
      ) : null}
    </Card>
  )
}
