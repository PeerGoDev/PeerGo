export type TorrentView = "list" | "poster" | "waterfall"

export function parseTorrentView(value: unknown): TorrentView | undefined {
  return value === "list" || value === "poster" || value === "waterfall"
    ? value
    : undefined
}
