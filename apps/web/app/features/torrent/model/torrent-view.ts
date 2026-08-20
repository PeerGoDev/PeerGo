export type TorrentView = "list" | "poster"

export function parseTorrentView(value: unknown): TorrentView | undefined {
  return value === "list" || value === "poster" ? value : undefined
}
