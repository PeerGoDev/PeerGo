import * as React from "react"

import {
  Avatar,
  AvatarBadge,
  AvatarFallback,
  AvatarImage,
} from "~/components/ui/avatar"
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
const unavailableAvatarUsernames = new Set<string>()

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
  loadImage = true,
  online = false,
  ...props
}: Omit<React.ComponentProps<typeof Avatar>, "children"> & {
  username: string
  displayName: string
  colorSeed?: string
  fallbackClassName?: string
  loadImage?: boolean
  online?: boolean
}) {
  const identity = displayName.trim() || username.trim()
  const revision = useAvatarRevision(username)
  const usernameKey = username.toLocaleLowerCase()
  const [imageUnavailable, setImageUnavailable] = React.useState(() =>
    unavailableAvatarUsernames.has(usernameKey)
  )

  React.useEffect(() => {
    if (revision) unavailableAvatarUsernames.delete(usernameKey)
    setImageUnavailable(unavailableAvatarUsernames.has(usernameKey))
  }, [revision, usernameKey])

  const avatarPath =
    loadImage && !imageUnavailable
      ? `/api/v1/users/${encodeURIComponent(username)}/avatar${revision ? `?v=${encodeURIComponent(revision)}` : ""}`
      : ""
  return (
    <Avatar className={className} {...props}>
      {avatarPath ? (
        <AvatarImage
          src={avatarPath}
          alt={`${identity}的头像`}
          loading="lazy"
          decoding="async"
          onLoadingStatusChange={(status) => {
            if (status !== "error") return
            unavailableAvatarUsernames.add(usernameKey)
            setImageUnavailable(true)
          }}
        />
      ) : null}
      <AvatarFallback
        className={cn(
          "font-medium",
          avatarColor(colorSeed?.trim() || identity),
          fallbackClassName
        )}
      >
        {avatarInitial(identity)}
      </AvatarFallback>
      {online ? (
        <AvatarBadge
          className="bg-emerald-500 shadow-[0_0_6px_color-mix(in_oklab,var(--color-emerald-500)_70%,transparent)]"
          title="在线"
          aria-label={`${identity}在线`}
        />
      ) : null}
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
