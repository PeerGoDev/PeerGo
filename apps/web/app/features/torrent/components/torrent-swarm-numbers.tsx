import type { components } from "~/generated/api"
import { getTorrentSwarmFreshness } from "~/features/torrent/model/format"

type Torrent = components["schemas"]["TorrentSummary"]

export function TorrentSwarmNumbers({ torrent }: { torrent: Torrent }) {
  if (getTorrentSwarmFreshness(torrent) === "unavailable") {
    return (
      <span
        className="text-muted-foreground"
        title="尚无 Tracker 统计"
        aria-label="尚无 Tracker 统计"
      >
        — / — / —
      </span>
    )
  }

  return (
    <>
      <span className="text-success-foreground" title="做种数">
        {torrent.seeders}
      </span>
      <span className="text-muted-foreground"> / </span>
      <span className="text-destructive" title="下载数">
        {torrent.leechers}
      </span>
      <span className="text-muted-foreground"> / </span>
      <span title="完成数">{torrent.completed}</span>
    </>
  )
}
