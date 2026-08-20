import { describe, expect, it } from "vitest"

import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"

describe("requestErrorDescription", () => {
  it("keeps a typed server failure distinct from a network failure", () => {
    const error = new ApiProblemError(500, {
      status: 500,
      code: "internal_error",
      title: "服务暂时不可用",
      detail: "请稍后重试。",
      request_id: "request-123",
    })

    expect(requestErrorDescription(error)).toBe(
      "请稍后重试。 请求编号：request-123"
    )
  })

  it("uses connection wording only for a transport TypeError", () => {
    expect(requestErrorDescription(new TypeError("Failed to fetch"))).toBe(
      "无法连接 PeerGo 服务，请检查网络或服务运行状态后重试。"
    )
  })

  it("uses the caller fallback for an unknown failure", () => {
    expect(requestErrorDescription(new Error("internal"), "读取失败。")).toBe(
      "读取失败。"
    )
  })
})
