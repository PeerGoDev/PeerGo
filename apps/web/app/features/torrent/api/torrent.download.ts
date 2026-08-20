import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

const fallbackFilename = "peergo.torrent"

export type TorrentDownload = {
  blob: Blob
  filename: string
}

/**
 * Requests a server-generated metainfo copy by canonical numeric torrent ID. Tracker
 * credentials are deliberately absent from this request and from browser
 * storage; Core inserts the user's announce URL only into the response body.
 */
export async function requestTorrentDownload(
  torrentId: number
): Promise<TorrentDownload> {
  const { data, error, response } = await apiClient.GET(
    "/api/v1/torrents/{torrent_id}/download",
    {
      params: { path: { torrent_id: torrentId } },
      parseAs: "blob",
    }
  )

  if (!response.ok || !data) {
    throw new ApiProblemError(response.status, error)
  }

  return {
    blob: data,
    filename: torrentDownloadFilename(
      response.headers.get("Content-Disposition")
    ),
  }
}

export function saveTorrentDownload(download: TorrentDownload) {
  const objectUrl = URL.createObjectURL(download.blob)
  const anchor = document.createElement("a")
  anchor.href = objectUrl
  anchor.download = download.filename
  anchor.hidden = true
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(objectUrl)
}

export function isTorrentId(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) > 0
}

export function parseTorrentId(value: string | undefined): number | undefined {
  if (!value || !/^[1-9][0-9]*$/.test(value)) {
    return undefined
  }
  const parsed = Number(value)
  return isTorrentId(parsed) ? parsed : undefined
}

export function torrentDownloadFilename(contentDisposition: string | null) {
  if (!contentDisposition) {
    return fallbackFilename
  }

  const extended = contentDisposition.match(
    /(?:^|;)\s*filename\*\s*=\s*UTF-8''([^;]+)/i
  )
  const quoted = contentDisposition.match(
    /(?:^|;)\s*filename\s*=\s*"((?:[^"\\]|\\.)*)"/i
  )
  const plain = contentDisposition.match(/(?:^|;)\s*filename\s*=\s*([^;]+)/i)

  let candidate = ""
  if (extended) {
    try {
      candidate = decodeURIComponent(extended[1].trim())
    } catch {
      return fallbackFilename
    }
  } else if (quoted) {
    candidate = quoted[1].replace(/\\(["\\])/g, "$1")
  } else if (plain) {
    candidate = plain[1].trim()
  }

  // The server already emits a safe name, but the browser boundary still
  // strips path/control characters so a malformed proxy response cannot turn
  // the download attribute into a path-like value.
  const safe = candidate
    .split(/[\\/]/)
    .at(-1)
    ?.replace(/[\u0000-\u001f\u007f]/g, "")
    .trim()

  if (!safe) {
    return fallbackFilename
  }
  return safe.toLowerCase().endsWith(".torrent") ? safe : `${safe}.torrent`
}
