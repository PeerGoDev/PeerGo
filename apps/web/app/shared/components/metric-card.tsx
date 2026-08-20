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
    <Card
      size="sm"
      className={cn(
        "min-h-36 gap-0 rounded-lg border py-0 shadow-sm ring-0",
        tone === "positive" &&
          "border-success/20 bg-linear-to-br from-success/10 to-card",
        tone === "primary" &&
          "border-primary/20 bg-linear-to-br from-primary/10 to-card",
        tone === "warning" &&
          "border-warning/25 bg-linear-to-br from-warning/10 to-card",
        tone === "muted" && "bg-linear-to-br from-muted/70 to-card"
      )}
    >
      <CardHeader className="px-6 pt-5 pb-0">
        <CardTitle className="text-sm text-muted-foreground">{title}</CardTitle>
        {icon ? (
          <CardAction
            className={cn(
              "rounded-full bg-background/80 p-2.5 text-muted-foreground shadow-xs [&>svg]:size-5",
              tone === "positive" && "text-success-foreground",
              tone === "primary" && "text-primary",
              tone === "warning" && "text-warning-foreground"
            )}
            aria-hidden="true"
          >
            {icon}
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className="px-6 pt-2 pb-0">
        <p
          className={cn(
            "font-heading text-3xl font-bold tabular-nums",
            tone === "positive" && "text-success-foreground",
            tone === "primary" && "text-primary"
          )}
        >
          {value}
        </p>
      </CardContent>
      {description ? (
        <CardFooter className="border-0 bg-transparent px-6 pt-1 pb-5">
          <p className="truncate text-xs text-muted-foreground">
            {description}
          </p>
        </CardFooter>
      ) : null}
    </Card>
  )
}
