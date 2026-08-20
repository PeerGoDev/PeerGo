import { useMutation } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { resolveApiUrl } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type TorrentSubmission = components["schemas"]["TorrentSubmission"]
type TorrentSubmissionRequest =
  components["schemas"]["TorrentSubmissionRequest"]
type TorrentSubmissionMultipart = Omit<
  TorrentSubmissionRequest,
  "torrent_file" | "screenshots"
> & {
  torrent_file: File
  screenshots?: File[]
}

export type TorrentUploadProgress = {
  phase: "uploading" | "processing"
  percent: number
}

export type SubmitTorrentInput = TorrentSubmissionMultipart & {
  csrfToken: string
  idempotencyKey: string
  onProgress?: (progress: TorrentUploadProgress) => void
}

/**
 * Sends the OpenAPI multipart command through XMLHttpRequest solely because
 * Fetch does not expose upload progress. Session cookies, CSRF and the stable
 * idempotency key retain the same boundary as the generated API client; the
 * browser must not set Content-Type because it owns the multipart boundary.
 */
export function submitTorrent(
  input: SubmitTorrentInput
): Promise<TorrentSubmission> {
  const body = new FormData()
  body.set("category_id", input.category_id)
  body.set("title", input.title)
  body.set("subtitle", input.subtitle)
  body.set("description", input.description ?? "")
  body.set("media_info", input.media_info ?? "")
  body.set("anonymous", String(input.anonymous ?? false))
  if (input.imdb_id) body.set("imdb_id", input.imdb_id)
  if (input.tmdb_id) body.set("tmdb_id", input.tmdb_id)
  if (input.douban_id) body.set("douban_id", input.douban_id)
  input.facet_selections?.forEach((selection, selectionIndex) => {
    body.set(
      `facet_selections[${selectionIndex}][facet_id]`,
      selection.facet_id
    )
    selection.option_keys.forEach((optionKey, optionIndex) => {
      body.set(
        `facet_selections[${selectionIndex}][option_keys][${optionIndex}]`,
        optionKey
      )
    })
  })
  input.screenshots?.forEach((screenshot) => {
    body.append("screenshots", screenshot, screenshot.name)
  })
  body.set("torrent_file", input.torrent_file, input.torrent_file.name)

  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest()
    request.open("POST", resolveApiUrl("/api/v1/torrents"))
    request.withCredentials = true
    request.setRequestHeader(
      "Accept",
      "application/json, application/problem+json"
    )
    request.setRequestHeader("X-CSRF-Token", input.csrfToken)
    request.setRequestHeader("Idempotency-Key", input.idempotencyKey)

    input.onProgress?.({ phase: "uploading", percent: 0 })
    request.upload.addEventListener("progress", (event) => {
      if (!event.lengthComputable || event.total <= 0) {
        return
      }
      input.onProgress?.({
        phase: "uploading",
        percent: Math.min(100, Math.round((event.loaded / event.total) * 100)),
      })
    })
    request.upload.addEventListener("load", () => {
      input.onProgress?.({ phase: "processing", percent: 100 })
    })

    request.addEventListener("load", () => {
      const payload = parseJsonResponse(request.responseText)
      if (request.status === 201 && isTorrentSubmission(payload)) {
        resolve(payload)
        return
      }
      if (request.status >= 200 && request.status < 300) {
        reject(new Error("服务器返回了无法识别的提交结果，请稍后重试。"))
        return
      }
      reject(new ApiProblemError(request.status, payload))
    })
    request.addEventListener("error", () => {
      reject(new Error("无法连接服务器，请检查网络后使用当前页面重试。"))
    })
    request.addEventListener("abort", () => {
      reject(new Error("种子上传已取消。"))
    })

    request.send(body)
  })
}

export function useSubmitTorrent() {
  return useMutation({ mutationFn: submitTorrent })
}

function parseJsonResponse(raw: string): unknown {
  if (!raw) {
    return undefined
  }
  try {
    return JSON.parse(raw)
  } catch {
    return undefined
  }
}

function isTorrentSubmission(value: unknown): value is TorrentSubmission {
  if (!value || typeof value !== "object") {
    return false
  }
  const result = value as Partial<TorrentSubmission>
  return (
    typeof result.id === "number" &&
    Number.isSafeInteger(result.id) &&
    result.id > 0 &&
    typeof result.info_hash_v1 === "string" &&
    result.state === "pending_review" &&
    typeof result.content_name === "string" &&
    typeof result.total_size_bytes === "number" &&
    typeof result.file_count === "number" &&
    typeof result.submitted_at === "string"
  )
}
