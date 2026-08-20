import { useQuery } from "@tanstack/react-query"
import {
  CheckCircle2Icon,
  CircleAlertIcon,
  CoinsIcon,
  FileArchiveIcon,
  FileCheck2Icon,
  ImageIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { torrentSettingsQueryOptions } from "~/features/staff/api/operations.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffSettingsValueTable } from "~/features/staff/components/staff-settings-value-table"
import { TorrentPurchasePolicyCard } from "~/features/staff/components/torrent-purchase-policy-card"
import { TorrentUploadPolicyDialog } from "~/features/staff/components/torrent-upload-policy-dialog"
import { hasCapability } from "~/features/staff/model/capability"
import { MetricCard } from "~/shared/components/metric-card"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatInteger } from "~/shared/formatters/integer"

export function StaffTorrentSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="torrent.manage.read"
      pageHeader={{
        title: "种子规则",
        description: "查看上传、原始文件与种子购买规则。",
      }}
    >
      {({ session, capabilities }) => (
        <TorrentSettingsContent
          csrfToken={session.csrf_token}
          canUpdate={hasCapability(
            capabilities,
            "torrent.purchase.manage.update"
          )}
          canIssueUploadPolicy={hasCapability(
            capabilities,
            "torrent.upload.policy.issue"
          )}
        />
      )}
    </StaffAccessGate>
  )
}

function TorrentSettingsContent({
  csrfToken,
  canUpdate,
  canIssueUploadPolicy,
}: {
  csrfToken: string
  canUpdate: boolean
  canIssueUploadPolicy: boolean
}) {
  const settings = useQuery(torrentSettingsQueryOptions())
  if (settings.isPending) return <TorrentSettingsSkeleton />
  if (settings.isError || !settings.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>种子规则暂时无法读取</AlertTitle>
          <AlertDescription>
            请检查 Core 与当前后台会话后重试。
          </AlertDescription>
        </Alert>
        <Button
          variant="outline"
          className="w-fit"
          onClick={() => void settings.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </StaffPageFrame>
    )
  }

  const data = settings.data
  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">种子规则</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            这里只放上传、文件和购买规则；种子上下架在“种子管理”，流量优惠时间线在“优惠规则”。
          </p>
        </div>
        <div className="flex gap-2">
          <TorrentUploadPolicyDialog
            settings={data}
            csrfToken={csrfToken}
            disabled={!canIssueUploadPolicy}
          />
          <Button
            variant="outline"
            size="sm"
            disabled={settings.isFetching}
            onClick={() => void settings.refetch()}
          >
            <RefreshCwIcon
              data-icon="inline-start"
              className={settings.isFetching ? "animate-spin" : undefined}
            />
            刷新
          </Button>
        </div>
      </header>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <MetricCard
          title=".torrent 大小上限"
          value={formatBytes(data.upload.metainfo_max_bytes)}
          description="当前上传服务实际限制"
          icon={<FileArchiveIcon />}
          tone="primary"
        />
        <MetricCard
          title="文件树上限"
          value={formatInteger(data.upload.max_files)}
          description="解析后文件条目数"
          icon={<FileCheck2Icon />}
          tone="default"
        />
        <MetricCard
          title="截图数量"
          value={formatInteger(data.screenshots.max_count)}
          description={`每张 ${formatBytes(data.screenshots.max_bytes_per_file)}`}
          icon={<ImageIcon />}
          tone="default"
        />
        <MetricCard
          title="上传后状态"
          value="待审核"
          description="审核通过后才进入 Tracker"
          icon={<ShieldCheckIcon />}
          tone="warning"
        />
        <MetricCard
          title="付费种子"
          value={formatInteger(data.purchase.priced_torrents)}
          description={`已继承 ${formatInteger(data.purchase.legacy_entitlements)} 份旧站购买权限`}
          icon={<CoinsIcon />}
          tone="primary"
        />
      </div>

      <Alert>
        <CheckCircle2Icon />
        <AlertTitle>老站兼容只用于迁移</AlertTitle>
        <AlertDescription>
          PtYes/Rousi 老种子通过 legacy_import 记录兼容异常；用户新上传始终使用
          strict_upload，不能借迁移规则绕过 private=1、重复键或路径校验。
        </AlertDescription>
      </Alert>

      {data.scheduled_upload_policies.length ? (
        <Alert>
          <CheckCircle2Icon />
          <AlertTitle>
            已有 {data.scheduled_upload_policies.length} 个定时版本
          </AlertTitle>
          <AlertDescription>
            下一版将于{" "}
            {new Date(
              data.scheduled_upload_policies[0].effective_at
            ).toLocaleString("zh-CN")}{" "}
            生效；每次上传都会记录实际使用的规则版本。
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-4 xl:grid-cols-3">
        <Card className="gap-0 py-0">
          <CardHeader className="p-6 pb-4">
            <CardTitle>上传与审核</CardTitle>
            <CardDescription>新上传进入站点前必须满足的规则</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <StaffSettingsValueTable
              rows={[
                { label: "协议版本", value: "BitTorrent v1" },
                {
                  label: "必须为私有种子",
                  value: booleanLabel(data.upload.required_private),
                },
                {
                  label: "重复 info hash",
                  value: data.upload.duplicate_swarm_rejected
                    ? "拒绝上传"
                    : "允许",
                },
                { label: "上传初始状态", value: "待审核" },
                {
                  label: ".torrent 大小",
                  value: formatBytes(data.upload.metainfo_max_bytes),
                },
                {
                  label: "最多文件条目",
                  value: formatInteger(data.upload.max_files),
                },
              ]}
            />
          </CardContent>
        </Card>

        <Card className="gap-0 py-0">
          <CardHeader className="p-6 pb-4">
            <CardTitle>截图与原始种子</CardTitle>
            <CardDescription>展示素材与下载副本的处理方式</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <StaffSettingsValueTable
              rows={[
                {
                  label: "截图格式",
                  value: (
                    <span className="inline-flex flex-wrap justify-end gap-1">
                      {data.screenshots.formats.map((format) => (
                        <Badge key={format} variant="outline">
                          {format.toUpperCase()}
                        </Badge>
                      ))}
                    </span>
                  ),
                },
                {
                  label: "第一张截图",
                  value: data.screenshots.first_is_cover
                    ? "作为封面"
                    : "普通截图",
                },
                {
                  label: "原始 .torrent",
                  value: data.object.original_stored_immutable
                    ? "不可变保存"
                    : "可覆盖",
                },
                {
                  label: "用户下载副本",
                  value: data.object.announce_rewritten_on_download
                    ? "写入个人 Tracker 地址"
                    : "保持原 Announce",
                },
              ]}
            />
          </CardContent>
        </Card>

        <TorrentPurchasePolicyCard
          purchase={data.purchase}
          csrfToken={csrfToken}
          canUpdate={canUpdate}
        />
      </div>
    </StaffPageFrame>
  )
}

function booleanLabel(value: boolean) {
  return value ? "是" : "否"
}

function TorrentSettingsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载种子规则">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        {Array.from({ length: 5 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-20 rounded-lg" />
      <div className="grid gap-4 xl:grid-cols-3">
        <Skeleton className="h-96 rounded-lg" />
        <Skeleton className="h-96 rounded-lg" />
        <Skeleton className="h-96 rounded-lg" />
      </div>
    </StaffPageFrame>
  )
}
