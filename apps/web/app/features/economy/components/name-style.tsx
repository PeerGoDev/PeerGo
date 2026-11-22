import * as React from "react"
import { useQuery } from "@tanstack/react-query"

import { nameStylesQueryOptions } from "~/features/economy/api/magic-store.queries"
import { cn } from "~/lib/utils"

export const SOLID_NAME_STYLES: Record<string, string> = {
  crimson: "绯红",
  orange: "橙光",
  amber: "琥珀",
  emerald: "翡翠",
  teal: "青碧",
  sky: "天蓝",
  indigo: "靛青",
  violet: "紫罗兰",
  pink: "樱粉",
}

export const GRADIENT_NAME_STYLES: Record<string, string> = {
  sunset: "落日",
  ocean: "深海",
  aurora: "极光",
  flame: "烈焰",
  sakura: "樱花",
  gold: "鎏金",
  rainbow: "彩虹（流动）",
}

export function nameStyleLabel(key: string) {
  return SOLID_NAME_STYLES[key] ?? GRADIENT_NAME_STYLES[key] ?? key
}

/**
 * Reads the site-wide colored username list from the query cache. The shell
 * is the only place that fetches it, so rendering many names costs nothing.
 */
export function useNameStyle(username: string | undefined) {
  const styles = useQuery({ ...nameStylesQueryOptions(), enabled: false })
  return React.useMemo(() => {
    if (!username || !styles.data) return undefined
    const now = Date.now()
    return styles.data.items.find(
      (item) => item.username === username && Date.parse(item.expires_at) > now
    )?.style_key
  }, [styles.data, username])
}

export function UserName({
  username,
  children,
  className,
}: {
  username: string
  children?: React.ReactNode
  className?: string
}) {
  const style = useNameStyle(username)
  return (
    <span data-name-style={style} className={className}>
      {children ?? username}
    </span>
  )
}

export function NameStyleSwatch({
  styleKey,
  text,
  className,
}: {
  styleKey: string
  text: string
  className?: string
}) {
  return (
    <span data-name-style={styleKey} className={cn("truncate", className)}>
      {text}
    </span>
  )
}
