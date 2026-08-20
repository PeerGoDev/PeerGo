import { useSyncExternalStore } from "react"

const revisions = new Map<string, string>()
const listeners = new Set<() => void>()

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function announceAvatarRevision(username: string, revision: string) {
  revisions.set(username.toLocaleLowerCase(), revision)
  for (const listener of listeners) listener()
}

export function useAvatarRevision(username: string) {
  const key = username.toLocaleLowerCase()
  return useSyncExternalStore(
    subscribe,
    () => revisions.get(key) ?? "",
    () => ""
  )
}
