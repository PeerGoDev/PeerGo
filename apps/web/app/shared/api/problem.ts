import type { components } from "~/generated/api"

type Problem = components["schemas"]["Problem"]

export class ApiProblemError extends Error {
  readonly status: number
  readonly code: string
  readonly detail?: string
  readonly requestId?: string

  constructor(status: number, payload: unknown) {
    const problem = isProblem(payload) ? payload : undefined
    super(problem?.title ?? "请求失败")
    this.name = "ApiProblemError"
    this.status = status
    this.code = problem?.code ?? "unknown_error"
    this.detail = problem?.detail
    this.requestId = problem?.request_id
  }
}

/**
 * Converts transport and typed API failures into honest user-facing detail.
 * A fetch TypeError is the only case described as a connection problem;
 * server responses retain their public Problem detail and request ID instead
 * of being mislabeled as a broken network.
 */
export function requestErrorDescription(
  error: unknown,
  fallback = "请求未能完成，请稍后重试。"
) {
  if (error instanceof ApiProblemError) {
    const description = error.detail?.trim() || error.message
    return error.requestId
      ? `${description} 请求编号：${error.requestId}`
      : description
  }
  if (error instanceof TypeError) {
    return "无法连接 PeerGo 服务，请检查网络或服务运行状态后重试。"
  }
  return fallback
}

function isProblem(payload: unknown): payload is Problem {
  if (!payload || typeof payload !== "object") {
    return false
  }

  return (
    "title" in payload &&
    typeof payload.title === "string" &&
    "status" in payload &&
    typeof payload.status === "number" &&
    "code" in payload &&
    typeof payload.code === "string"
  )
}
