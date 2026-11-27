import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "react-router"

import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import { Input } from "~/components/ui/input"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

type RetentionProgress = components["schemas"]["RetentionProgress"]
type Membership = components["schemas"]["WorkgroupMembership"]
type PayrollRequest = components["schemas"]["WorkgroupPayrollRequest"]

const pageSize = 25
const currentMonth = () => new Date().toISOString().slice(0, 7)
const previousMonth = () => {
  const now = new Date()
  return new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - 1, 1))
    .toISOString()
    .slice(0, 7)
}

function read<T>(result: { data?: T; error?: unknown; response: Response }): T {
  if (result.data === undefined || !result.response.ok) {
    throw new ApiProblemError(result.response.status, result.error)
  }
  return result.data
}

function completed(result: { error?: unknown; response: Response }) {
  if (!result.response.ok)
    throw new ApiProblemError(result.response.status, result.error)
}

function ErrorMessage({ error }: { error: unknown }) {
  return error ? (
    <p role="alert" className="text-sm text-destructive">
      {requestErrorDescription(error)}
    </p>
  ) : null
}

function MonthField({
  month,
  onChange,
  disabled,
}: {
  month: string
  onChange: (value: string) => void
  disabled?: boolean
}) {
  return (
    <label className="flex items-center gap-2 text-sm">
      月份（UTC）
      <Input
        type="month"
        className="w-auto"
        value={month}
        max={currentMonth()}
        disabled={disabled}
        onChange={(event) => {
          if (event.target.value) onChange(event.target.value)
        }}
      />
    </label>
  )
}

function PageButtons({
  offset,
  next,
  setOffset,
}: {
  offset: number
  next: boolean
  setOffset: (value: number) => void
}) {
  if (offset === 0 && !next) return null
  return (
    <div className="flex items-center justify-end gap-2">
      <Button
        variant="outline"
        size="sm"
        disabled={offset === 0}
        onClick={() => setOffset(offset - pageSize)}
      >
        上一页
      </Button>
      <span className="text-sm tabular-nums">{offset / pageSize + 1}</span>
      <Button
        variant="outline"
        size="sm"
        disabled={!next}
        onClick={() => setOffset(offset + pageSize)}
      >
        下一页
      </Button>
    </div>
  )
}

export function RetentionTargetList({
  csrfToken,
  staff,
  canManage,
  userKey,
}: {
  csrfToken: string
  staff: boolean
  canManage?: boolean
  userKey: string
}) {
  const client = useQueryClient()
  const [offset, setOffset] = useState(0)
  const [torrent, setTorrent] = useState("")
  const [hours, setHours] = useState("168")
  const key = [
    "workgroup-monthly",
    "retention",
    staff ? "staff" : "member",
    userKey,
  ]
  const list = useQuery({
    queryKey: [...key, "targets", offset],
    queryFn: async () => {
      const options = { params: { query: { limit: pageSize, offset } } }
      return staff
        ? read(
            await apiClient.GET(
              "/api/v1/admin/workgroups/retention/targets",
              options
            )
          )
        : read(
            await apiClient.GET(
              "/api/v1/me/workgroups/retention/targets",
              options
            )
          )
    },
    retry: false,
  })
  const mutation = useMutation({
    mutationFn: async (input: {
      torrent_id: number
      retire?: boolean
      required_hours?: number
    }) => {
      const params = { header: { "X-CSRF-Token": csrfToken } }
      if (staff) {
        completed(
          await apiClient.POST("/api/v1/admin/workgroups/retention/targets", {
            params,
            body: { ...input, retire: Boolean(input.retire) },
          })
        )
      } else {
        completed(
          await apiClient.POST("/api/v1/me/workgroups/retention/targets", {
            params,
            body: { torrent_id: input.torrent_id },
          })
        )
      }
    },
    onSuccess: async () => {
      setTorrent("")
      await client.invalidateQueries({
        queryKey: ["workgroup-monthly", "retention"],
      })
    },
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>保种认领清单</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {staff && canManage ? (
          <form
            className="flex flex-wrap items-end gap-2"
            onSubmit={(event) => {
              event.preventDefault()
              mutation.mutate({
                torrent_id: Number(torrent),
                required_hours: Number(hours),
              })
            }}
          >
            <label className="grid gap-1 text-sm">
              种子 ID
              <Input
                required
                type="number"
                min={1}
                max={Number.MAX_SAFE_INTEGER}
                value={torrent}
                onChange={(event) => setTorrent(event.target.value)}
              />
            </label>
            <label className="grid gap-1 text-sm">
              每月有效时长（小时）
              <Input
                required
                type="number"
                min={1}
                max={744}
                value={hours}
                onChange={(event) => setHours(event.target.value)}
              />
            </label>
            <Button type="submit" disabled={mutation.isPending}>
              加入清单
            </Button>
          </form>
        ) : null}
        <ErrorMessage error={list.error ?? mutation.error} />
        {list.data ? (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>种子</TableHead>
                  <TableHead>体积</TableHead>
                  <TableHead>每月门槛</TableHead>
                  <TableHead>操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.data.items.map((item) => (
                  <TableRow key={item.torrent_id}>
                    <TableCell className="max-w-96 whitespace-normal">
                      <Link
                        className="text-link"
                        to={`/torrents/${item.torrent_id}`}
                      >
                        {item.title}
                      </Link>
                    </TableCell>
                    <TableCell>{formatBytes(item.size_bytes)}</TableCell>
                    <TableCell>{item.required_hours} 小时</TableCell>
                    <TableCell>
                      {staff ? (
                        canManage ? (
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={mutation.isPending}
                            onClick={() =>
                              mutation.mutate({
                                torrent_id: item.torrent_id,
                                retire: true,
                              })
                            }
                          >
                            移出
                          </Button>
                        ) : (
                          "—"
                        )
                      ) : item.claimed_at ? (
                        "已认领"
                      ) : (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={mutation.isPending}
                          onClick={() =>
                            mutation.mutate({ torrent_id: item.torrent_id })
                          }
                        >
                          认领
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {list.data.items.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                本页暂无可认领种子。
              </p>
            ) : null}
            <PageButtons
              offset={offset}
              next={list.data.items.length === pageSize}
              setOffset={setOffset}
            />
          </>
        ) : list.isPending ? (
          <p role="status">正在读取清单…</p>
        ) : null}
      </CardContent>
    </Card>
  )
}

function RetentionProgressTable({ value }: { value: RetentionProgress }) {
  return (
    <>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        <span>达标种数 {formatInteger(value.qualified_count)}</span>
        <span>达标体积 {formatBytes(value.qualified_bytes)}</span>
        <span>
          完整证据 {value.complete_hours} / {value.expected_hours} 小时
        </span>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>已认领种子</TableHead>
            <TableHead>体积</TableHead>
            <TableHead>有效时长</TableHead>
            <TableHead>状态</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {value.items.map((item) => (
            <TableRow key={item.torrent_id}>
              <TableCell className="max-w-96 whitespace-normal">
                <Link className="text-link" to={`/torrents/${item.torrent_id}`}>
                  {item.title}
                </Link>
              </TableCell>
              <TableCell>{formatBytes(item.size_bytes)}</TableCell>
              <TableCell>
                {Math.floor(item.active_seconds / 3600)} 小时{" "}
                {Math.floor((item.active_seconds % 3600) / 60)} 分 /{" "}
                {item.required_hours} 小时
              </TableCell>
              <TableCell>
                {item.qualified
                  ? "达标"
                  : value.complete_hours < value.expected_hours
                    ? "证据待补齐"
                    : "低于门槛"}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {!value.items.length ? (
        <p className="text-sm text-muted-foreground">该月还没有认领记录。</p>
      ) : null}
    </>
  )
}

export function RetentionProgressPanel({
  userKey,
  membershipId,
}: {
  userKey: string
  membershipId?: string
}) {
  const [month, setMonth] = useState(currentMonth)
  const progress = useQuery({
    queryKey: [
      "workgroup-monthly",
      "retention",
      userKey,
      "progress",
      membershipId,
      month,
    ],
    queryFn: async () =>
      membershipId
        ? read(
            await apiClient.GET(
              "/api/v1/admin/workgroups/retention/memberships/{membership_id}/claims",
              {
                params: {
                  path: { membership_id: membershipId },
                  query: { month },
                },
              }
            )
          )
        : read(
            await apiClient.GET("/api/v1/me/workgroups/retention/progress", {
              params: { query: { month } },
            })
          ),
    retry: false,
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>逐种保种进度</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <MonthField month={month} onChange={setMonth} />
        <ErrorMessage error={progress.error} />
        {progress.data ? (
          <RetentionProgressTable value={progress.data} />
        ) : progress.isPending ? (
          <p role="status">正在读取进度…</p>
        ) : null}
      </CardContent>
    </Card>
  )
}

const resolutionLabels: Record<string, string> = {
  trusted_workgroup: "可信直发",
  staff: "管理员审核",
  review_round: "种审组审核",
}
const reasonLabels: Record<string, string> = {
  duplicate_or_superseded: "重复或已有更合适的版本",
  content_policy_violation: "违反内容规则",
  metadata_incomplete: "种子资料不完整",
  quality_requirements_not_met: "未达到质量要求",
  uploader_action_required: "待发布者整改",
  meets_requirements: "通过后已下架或撤回",
  other: "其他审核原因",
}

export function ReseedPublicationPanel({
  membershipId,
}: {
  membershipId: string
}) {
  const [month, setMonth] = useState(currentMonth)
  const [offset, setOffset] = useState(0)
  const detail = useQuery({
    queryKey: ["workgroup-monthly", "reseed", membershipId, month, offset],
    queryFn: async () =>
      read(
        await apiClient.GET(
          "/api/v1/admin/workgroups/reseed/memberships/{membership_id}/publications",
          {
            params: {
              path: { membership_id: membershipId },
              query: { month, limit: pageSize, offset },
            },
          }
        )
      ),
    retry: false,
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>转种贡献明细</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <MonthField
          month={month}
          onChange={(value) => {
            setMonth(value)
            setOffset(0)
          }}
        />
        <ErrorMessage error={detail.error} />
        {detail.data ? (
          <>
            <p className="text-sm">
              已计入 {detail.data.counted} · 待审核 {detail.data.pending} ·
              未计入{" "}
              {detail.data.total - detail.data.pending - detail.data.counted}
            </p>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>种子</TableHead>
                  <TableHead>提交时间</TableHead>
                  <TableHead>审核来源</TableHead>
                  <TableHead>结果</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {detail.data.items.map((item) => (
                  <TableRow key={item.torrent_id}>
                    <TableCell className="max-w-72 whitespace-normal">
                      {item.status === "counted" ? (
                        <Link
                          className="text-link"
                          to={`/torrents/${item.torrent_id}`}
                        >
                          {item.title}
                        </Link>
                      ) : (
                        item.title
                      )}
                      <p className="text-xs text-muted-foreground">
                        #{item.torrent_id} · {item.category}
                      </p>
                    </TableCell>
                    <TableCell>{formatDateTime(item.submitted_at)}</TableCell>
                    <TableCell>
                      {resolutionLabels[item.resolution_source] ?? "—"}
                    </TableCell>
                    <TableCell>
                      {item.status === "counted"
                        ? "已计入"
                        : item.status === "pending"
                          ? "待审核"
                          : (reasonLabels[item.reason] ??
                            "当前没有合规发布记录")}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {!detail.data.total ? (
              <p className="text-sm text-muted-foreground">
                这个月没有在组期间提交的种子。
              </p>
            ) : null}
            <PageButtons
              offset={offset}
              next={offset + pageSize < detail.data.total}
              setOffset={setOffset}
            />
          </>
        ) : detail.isPending ? (
          <p role="status">正在读取贡献…</p>
        ) : null}
      </CardContent>
    </Card>
  )
}

export function WorkgroupPayrollPanel({
  membership,
  csrfToken,
  canPay,
}: {
  membership: Membership
  csrfToken: string
  canPay: boolean
}) {
  const [month, setMonth] = useState(previousMonth)
  const [amount, setAmount] = useState("")
  const [minimumCount, setMinimumCount] = useState("1")
  const [minimumGiB, setMinimumGiB] = useState("0")
  const [preview, setPreview] =
    useState<components["schemas"]["WorkgroupPayrollPreview"]>()
  const retention = membership.group_kind === "retention"
  const params = {
    header: { "X-CSRF-Token": csrfToken },
    path: { group_kind: membership.group_kind, membership_id: membership.id },
  }
  const input: PayrollRequest = {
    month,
    amount: Number(amount),
    minimum_count: retention ? Number(minimumCount) : 0,
    minimum_bytes:
      retention && /^\d+$/.test(minimumGiB)
        ? (BigInt(minimumGiB) * 1073741824n).toString()
        : "0",
  }
  const mutation = useMutation({
    mutationFn: async ({
      body,
      digest,
    }: {
      body: PayrollRequest
      digest?: string
    }) =>
      digest
        ? read(
            await apiClient.POST(
              "/api/v1/admin/workgroups/{group_kind}/memberships/{membership_id}/payroll/issue",
              { params, body: { ...body, preview_digest: digest } }
            )
          )
        : read(
            await apiClient.POST(
              "/api/v1/admin/workgroups/{group_kind}/memberships/{membership_id}/payroll/preview",
              { params, body }
            )
          ),
    onSuccess: setPreview,
  })
  const update = (setter: (value: string) => void) => (value: string) => {
    setPreview(undefined)
    mutation.reset()
    setter(value)
  }
  const state = {
    paid: "已发放",
    eligible: "达标，待确认发放",
    pending_evidence: "待审核或补证",
    below_target: "低于目标",
    in_progress: "月份进行中",
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>工作组月结工资</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <form
          className="grid gap-3"
          onSubmit={(event) => {
            event.preventDefault()
            setPreview(undefined)
            mutation.mutate({ body: input })
          }}
        >
          <div className="flex flex-wrap items-end gap-3">
            <MonthField
              month={month}
              onChange={update(setMonth)}
              disabled={mutation.isPending}
            />
            <label className="grid gap-1 text-sm">
              应发魔力
              <Input
                className="w-32"
                required
                type="number"
                min={1}
                max={1000000}
                value={amount}
                onChange={(event) => update(setAmount)(event.target.value)}
                disabled={mutation.isPending}
              />
            </label>
            {retention ? (
              <>
                <label className="grid gap-1 text-sm">
                  最低达标种数
                  <Input
                    className="w-28"
                    required
                    type="number"
                    min={1}
                    max={100000}
                    value={minimumCount}
                    onChange={(event) =>
                      update(setMinimumCount)(event.target.value)
                    }
                    disabled={mutation.isPending}
                  />
                </label>
                <label className="grid gap-1 text-sm">
                  最低达标体积（GiB）
                  <Input
                    className="w-28"
                    required
                    type="number"
                    min={0}
                    max={10000000}
                    value={minimumGiB}
                    onChange={(event) =>
                      update(setMinimumGiB)(event.target.value)
                    }
                    disabled={mutation.isPending}
                  />
                </label>
              </>
            ) : null}
            <Button
              type="submit"
              variant="outline"
              disabled={mutation.isPending}
            >
              预览工资
            </Button>
          </div>
        </form>
        <ErrorMessage error={mutation.error} />
        {preview ? (
          <div className="grid gap-2 rounded-md border p-3 text-sm">
            <p className="font-medium">
              {preview.month} · {preview.username} · {state[preview.state]}
            </p>
            <p>
              贡献 {preview.contribution} / {preview.target}
              {retention
                ? ` · 达标体积 ${formatBytes(preview.qualified_bytes)}`
                : ""}
            </p>
            {preview.pending_count || preview.missing_hours ? (
              <p>
                待审核 {preview.pending_count} 个 · 缺证 {preview.missing_hours}{" "}
                小时
              </p>
            ) : null}
            <p>
              {formatInteger(preview.amount)} 魔力
              {preview.paid_at ? ` · ${formatDateTime(preview.paid_at)}` : ""}
            </p>
            {canPay && preview.state === "eligible" ? (
              <Button
                className="justify-self-start"
                disabled={mutation.isPending}
                onClick={() =>
                  mutation.mutate({
                    body: input,
                    digest: preview.preview_digest,
                  })
                }
              >
                确认发放 {formatInteger(preview.amount)} 魔力
              </Button>
            ) : null}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}
