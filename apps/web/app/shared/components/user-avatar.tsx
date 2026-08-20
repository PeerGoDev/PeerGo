import type { ComponentProps } from "react"

import { Avatar, AvatarFallback, AvatarImage } from "~/components/ui/avatar"
import { cn } from "~/lib/utils"
import { useAvatarRevision } from "~/shared/components/avatar-revision"

const fallbackColors = [
  "bg-primary text-primary-foreground",
  "bg-success text-primary-foreground",
  "bg-warning text-warning-foreground",
  "bg-chart-3 text-primary-foreground",
  "bg-chart-5 text-primary-foreground",
  "bg-destructive text-primary-foreground",
] as const

/**
 * Shared PtYes-compatible fallback avatar. Image migration is intentionally
 * out of scope, so every identity surface derives the same stable color and
 * one-character label from public display data without storing another field.
 */
export function UserAvatar({
  username,
  displayName,
  colorSeed,
  className,
  fallbackClassName,
  ...props
}: Omit<ComponentProps<typeof Avatar>, "children"> & {
  username: string
  displayName: string
  colorSeed?: string
  fallbackClassName?: string
}) {
  const identity = displayName.trim() || username.trim()
  const revision = useAvatarRevision(username)
  const avatarPath = `/api/v1/users/${encodeURIComponent(username)}/avatar${revision ? `?v=${encodeURIComponent(revision)}` : ""}`
  return (
    <Avatar className={className} {...props}>
      <AvatarImage src={avatarPath} alt={`${identity}的头像`} />
      <AvatarFallback
        className={cn(
          "font-medium",
          avatarColor(colorSeed?.trim() || identity),
          fallbackClassName
        )}
      >
        {avatarInitial(identity)}
      </AvatarFallback>
    </Avatar>
  )
}

export function avatarInitial(value: string) {
  return (Array.from(value.trim())[0] ?? "?").toLocaleUpperCase()
}

function avatarColor(value: string) {
  let hash = 0
  for (const character of value) {
    hash = character.codePointAt(0)! + ((hash << 5) - hash)
  }
  return fallbackColors[Math.abs(hash) % fallbackColors.length]
}
