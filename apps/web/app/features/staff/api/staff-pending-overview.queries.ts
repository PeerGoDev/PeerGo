import { useQuery } from "@tanstack/react-query"

import { adminSeedboxReportsQueryOptions } from "~/features/seedbox/api/seedbox.queries"
import { accountAccessAppealListQueryOptions } from "~/features/staff/api/account-access-appeal-administration.queries"
import { commentModerationCasesQueryOptions } from "~/features/staff/api/comment-moderation.queries"
import { workerOperationsQueryOptions } from "~/features/staff/api/operations.queries"
import {
  managedPublishedTorrentContentChangesQueryOptions,
  managedPublishedTorrentScreenshotChangesQueryOptions,
  managedTorrentReportCasesQueryOptions,
  managedTorrentWithdrawalsQueryOptions,
} from "~/features/staff/api/torrent-administration.queries"
import { pendingTorrentReviewsQueryOptions } from "~/features/staff/api/torrent-review.queries"
import { adminWorkgroupOverviewQueryOptions } from "~/features/staff/api/workgroup-administration.queries"
import { hasCapability } from "~/features/staff/model/capability"
import { summarizeOperationIncidents } from "~/features/staff/model/operation-incidents"
import type { components } from "~/generated/api"

type CapabilityList = components["schemas"]["CapabilityList"]

const pendingRefreshInterval = 60_000

export type StaffPendingItem = {
  id:
    | "torrent-reviews"
    | "torrent-changes"
    | "comment-reports"
    | "account-appeals"
    | "workgroup-applications"
    | "seedbox-reports"
    | "operation-incidents"
  label: string
  description: string
  to: string
  count: number
}

export function useStaffPendingOverview(
  capabilities: CapabilityList | undefined
) {
  const canReviewTorrents = hasCapability(capabilities, "torrent.review")
  const canReviewContentChanges = hasCapability(
    capabilities,
    "torrent.content.change.review"
  )
  const canReviewScreenshotChanges = hasCapability(
    capabilities,
    "torrent.screenshot.change.review"
  )
  const canReviewWithdrawals = hasCapability(
    capabilities,
    "torrent.withdraw.review"
  )
  const canReviewTorrentReports = hasCapability(
    capabilities,
    "torrent.report.review"
  )
  const canOpenTorrentWorkbench = hasCapability(
    capabilities,
    "torrent.manage.read"
  )
  const canReviewComments = hasCapability(capabilities, "social.report.read")
  const canReadAccountAppeals = hasCapability(
    capabilities,
    "user.account.appeal.read"
  )
  const canReadUsers = hasCapability(capabilities, "user.account.read")
  const canReadWorkgroups = hasCapability(capabilities, "workgroup.manage.read")
  const canReadSeedboxReports =
    hasCapability(capabilities, "tracker.seedbox.registry.read") &&
    hasCapability(capabilities, "tracker.policy.read")
  const canReadOperationIncidents = hasCapability(
    capabilities,
    "operations.monitor.read"
  )

  const torrentReviews = useQuery({
    ...pendingTorrentReviewsQueryOptions(1),
    enabled: canReviewTorrents,
    refetchInterval: pendingRefreshInterval,
  })
  const contentChanges = useQuery({
    ...managedPublishedTorrentContentChangesQueryOptions,
    enabled: canOpenTorrentWorkbench && canReviewContentChanges,
    refetchInterval: pendingRefreshInterval,
  })
  const screenshotChanges = useQuery({
    ...managedPublishedTorrentScreenshotChangesQueryOptions,
    enabled: canOpenTorrentWorkbench && canReviewScreenshotChanges,
    refetchInterval: pendingRefreshInterval,
  })
  const withdrawals = useQuery({
    ...managedTorrentWithdrawalsQueryOptions,
    enabled: canOpenTorrentWorkbench && canReviewWithdrawals,
    refetchInterval: pendingRefreshInterval,
  })
  const torrentReports = useQuery({
    ...managedTorrentReportCasesQueryOptions,
    enabled: canOpenTorrentWorkbench && canReviewTorrentReports,
    refetchInterval: pendingRefreshInterval,
  })
  const commentReports = useQuery({
    ...commentModerationCasesQueryOptions(1, 0),
    enabled: canReviewComments,
    refetchInterval: pendingRefreshInterval,
  })
  const accountAppeals = useQuery({
    ...accountAccessAppealListQueryOptions(
      "pending",
      canReadUsers && canReadAccountAppeals
    ),
    refetchInterval: pendingRefreshInterval,
  })
  const workgroups = useQuery({
    ...adminWorkgroupOverviewQueryOptions,
    enabled: canReadWorkgroups,
    refetchInterval: pendingRefreshInterval,
  })
  const seedboxReports = useQuery({
    ...adminSeedboxReportsQueryOptions("pending"),
    enabled: canReadSeedboxReports,
    refetchInterval: pendingRefreshInterval,
  })
  const workerOperations = useQuery({
    ...workerOperationsQueryOptions(),
    enabled: canReadOperationIncidents,
    refetchInterval: pendingRefreshInterval,
  })

  const torrentChangeCount =
    (contentChanges.data?.total ?? 0) +
    (screenshotChanges.data?.total ?? 0) +
    (withdrawals.data?.total ?? 0) +
    (torrentReports.data?.total ?? 0)
  const operationIncidentCount = workerOperations.data
    ? summarizeOperationIncidents(workerOperations.data.queues)
        .incidentQueueCount
    : 0

  const allItems: StaffPendingItem[] = [
    {
      id: "torrent-reviews",
      label: "待审核种子",
      description: "等待种审成员投票或管理员处理",
      to: "/staff/content/torrent-reviews",
      count: torrentReviews.data?.total ?? 0,
    },
    {
      id: "torrent-changes",
      label: "种子变更与举报",
      description: "内容、截图、撤回与举报待处理",
      to: "/staff/content/torrents",
      count: torrentChangeCount,
    },
    {
      id: "comment-reports",
      label: "评论举报",
      description: "等待核对和处置的评论案件",
      to: "/staff/content/comments",
      count: commentReports.data?.total ?? 0,
    },
    {
      id: "account-appeals",
      label: "账户访问申诉",
      description: "封禁或访问限制的待处理申诉",
      to: "/staff/users",
      count: accountAppeals.data?.total ?? 0,
    },
    {
      id: "workgroup-applications",
      label: "工作组申请",
      description: "种审等工作组的待审批申请",
      to: "/staff/workgroups",
      count: workgroups.data?.pending_applications ?? 0,
    },
    {
      id: "seedbox-reports",
      label: "盒子申报",
      description: "等待核验的盒子与网络申报",
      to: "/staff/settings/seedbox",
      count: seedboxReports.data?.total ?? 0,
    },
    {
      id: "operation-incidents",
      label: "后台任务异常",
      description: "持续重试或进入死信的任务队列",
      to: "/staff/operations/incidents",
      count: operationIncidentCount,
    },
  ]

  const enabledItems = allItems.filter((item) => {
    switch (item.id) {
      case "torrent-reviews":
        return canReviewTorrents
      case "torrent-changes":
        return (
          canOpenTorrentWorkbench &&
          (canReviewContentChanges ||
            canReviewScreenshotChanges ||
            canReviewWithdrawals ||
            canReviewTorrentReports)
        )
      case "comment-reports":
        return canReviewComments
      case "account-appeals":
        return canReadUsers && canReadAccountAppeals
      case "workgroup-applications":
        return canReadWorkgroups
      case "seedbox-reports":
        return canReadSeedboxReports
      case "operation-incidents":
        return canReadOperationIncidents
    }
  })
  const items = enabledItems.filter((item) => item.count > 0)
  const byRoute = Object.fromEntries(
    enabledItems.map((item) => [item.to, item.count])
  ) as Record<string, number>
  const queries = [
    torrentReviews,
    contentChanges,
    screenshotChanges,
    withdrawals,
    torrentReports,
    commentReports,
    accountAppeals,
    workgroups,
    seedboxReports,
    workerOperations,
  ]

  return {
    items,
    byRoute,
    total: items.reduce((total, item) => total + item.count, 0),
    isFetching: queries.some((query) => query.isFetching),
    hasError: queries.some((query) => query.isError),
    refetch: async () => {
      await Promise.all(
        queries
          .filter((query) => query.fetchStatus !== "idle" || query.isEnabled)
          .map((query) => query.refetch())
      )
    },
  }
}
