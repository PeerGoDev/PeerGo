import * as React from "react"
import { useMutation } from "@tanstack/react-query"
import { CheckIcon, CopyIcon, DownloadIcon, SparklesIcon } from "lucide-react"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Spinner } from "~/components/ui/spinner"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "~/components/ui/tooltip"
import {
  isTorrentId,
  requestTorrentDownload,
  saveTorrentDownload,
} from "~/features/torrent/api/torrent.download"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  usePurchaseTorrent,
  useTorrentPurchaseStatus,
} from "~/features/torrent/api/torrent-purchases.queries"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"

export function TorrentDownloadButton({
  torrentId,
  torrentName,
  purchaseAware = false,
  showLabel = false,
  showCopyAction = false,
  className,
}: {
  torrentId: number
  torrentName: string
  purchaseAware?: boolean
  showLabel?: boolean
  showCopyAction?: boolean
  className?: string
}) {
  const available = isTorrentId(torrentId)
  const session = useWebSession()
  const purchaseStatus = useTorrentPurchaseStatus(
    session.data?.user.id,
    torrentId,
    purchaseAware && available
  )
  const purchase = usePurchaseTorrent(
    session.data?.user.id,
    session.data?.csrf_token,
    torrentId
  )
  const [purchaseOpen, setPurchaseOpen] = React.useState(false)
  const purchaseRequestId = React.useRef<string>(undefined)
  const [copyState, setCopyState] = React.useState<
    "idle" | "copied" | "failed"
  >("idle")
  const copyResetTimer = React.useRef<ReturnType<typeof setTimeout>>(undefined)
  const download = useMutation({
    mutationFn: () => requestTorrentDownload(torrentId),
    onSuccess: saveTorrentDownload,
  })
  const accessState = purchaseStatus.data?.state
  const purchaseRequired = purchaseAware && accessState === "purchase_required"
  const purchaseDisabled = purchaseAware && accessState === "purchase_disabled"
  const purchaseChecking =
    purchaseAware && Boolean(session.data) && purchaseStatus.isPending
  const purchaseUnavailable =
    purchaseAware && Boolean(session.data) && purchaseStatus.isError
  const accessReady =
    !purchaseAware ||
    !session.data ||
    (!purchaseChecking && !purchaseUnavailable && !purchaseDisabled)
  const description = purchaseUnavailable
    ? requestErrorDescription(
        purchaseStatus.error,
        "购买状态暂时无法读取，请稍后重试"
      )
    : purchaseDisabled
      ? "站点暂时停止新的种子购买"
      : purchaseChecking
        ? "正在检查购买权限…"
        : download.isError
          ? downloadErrorDescription(download.error)
          : download.isPending
            ? "正在生成专属种子副本…"
            : available
              ? "下载种子"
              : "当前条目暂不提供下载"
  const buttonLabel = purchaseChecking
    ? "检查权限…"
    : purchaseRequired && purchaseStatus.data
      ? "购买并下载"
      : download.isPending
        ? "正在生成…"
        : "下载种子"

  React.useEffect(
    () => () => {
      if (copyResetTimer.current) clearTimeout(copyResetTimer.current)
    },
    []
  )

  async function copyDownloadAddress() {
    if (!available || !accessReady || purchaseRequired || !navigator.clipboard)
      return
    try {
      // This is the authenticated Core endpoint, not a tracker URL. The
      // copied address contains neither the user's passkey nor a reusable
      // session secret; Core still authorizes it with the HttpOnly session.
      const downloadURL = new URL(
        `/api/v1/torrents/${encodeURIComponent(torrentId)}/download`,
        window.location.origin
      )
      await navigator.clipboard.writeText(downloadURL.href)
      setCopyState("copied")
    } catch {
      setCopyState("failed")
    }
    if (copyResetTimer.current) clearTimeout(copyResetTimer.current)
    copyResetTimer.current = setTimeout(() => setCopyState("idle"), 2_000)
  }

  function beginDownload() {
    if (purchaseRequired) {
      purchase.reset()
      setPurchaseOpen(true)
      return
    }
    download.mutate()
  }

  async function confirmPurchase() {
    if (!purchaseStatus.data || purchase.isPending) return
    purchaseRequestId.current ??= globalThis.crypto.randomUUID()
    try {
      await purchase.mutateAsync(purchaseRequestId.current)
      purchaseRequestId.current = undefined
      setPurchaseOpen(false)
      download.mutate()
    } catch {
      // Keep the same idempotency key so a transport-level retry cannot charge
      // the member twice after Core has already committed the first request.
    }
  }

  const insufficientBalance = purchaseStatus.data
    ? integerLessThan(
        purchaseStatus.data.magic_balance,
        purchaseStatus.data.price
      )
    : false

  const downloadAction = (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant={showLabel ? "default" : "ghost"}
            size={showLabel ? "default" : "icon-xs"}
            className={
              showCopyAction
                ? "h-full min-w-0 flex-1 rounded-r-none"
                : className
            }
            disabled={
              !available ||
              !accessReady ||
              (purchaseAware && session.isPending) ||
              download.isPending
            }
            focusableWhenDisabled
            aria-label={`下载“${torrentName}”的种子文件`}
            onClick={beginDownload}
          >
            {download.isPending ? (
              <Spinner />
            ) : (
              <DownloadIcon data-icon="inline-start" />
            )}
            {showLabel && buttonLabel}
          </Button>
        }
      />
      <TooltipContent>{description}</TooltipContent>
    </Tooltip>
  )

  return (
    <>
      {showCopyAction ? (
        <div className={className} data-slot="torrent-download-actions">
          <div className="flex size-full">
            {downloadAction}
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    className="h-full shrink-0 rounded-l-none border-l-0"
                    disabled={
                      !available ||
                      !accessReady ||
                      purchaseRequired ||
                      (purchaseAware && session.isPending)
                    }
                    focusableWhenDisabled
                    aria-label={`复制“${torrentName}”的下载地址`}
                    onClick={() => void copyDownloadAddress()}
                  >
                    {copyState === "copied" ? (
                      <CheckIcon data-icon="inline-start" />
                    ) : (
                      <CopyIcon data-icon="inline-start" />
                    )}
                  </Button>
                }
              />
              <TooltipContent>
                {copyState === "copied"
                  ? "已复制下载地址"
                  : copyState === "failed"
                    ? "复制失败，请重试"
                    : "复制下载地址"}
              </TooltipContent>
            </Tooltip>
          </div>
        </div>
      ) : (
        downloadAction
      )}
      <span
        className="sr-only"
        role={download.isError ? "alert" : "status"}
        aria-live="polite"
      >
        {download.isPending || download.isError ? description : ""}
      </span>
      <span
        className="sr-only"
        role={copyState === "failed" ? "alert" : "status"}
        aria-live="polite"
      >
        {copyState === "copied"
          ? "已复制下载地址"
          : copyState === "failed"
            ? "复制失败，请重试"
            : ""}
      </span>
      {purchaseAware && purchaseStatus.data ? (
        <AlertDialog open={purchaseOpen} onOpenChange={setPurchaseOpen}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogMedia>
                <SparklesIcon className="text-primary" />
              </AlertDialogMedia>
              <AlertDialogTitle>购买种子下载权限</AlertDialogTitle>
              <AlertDialogDescription>
                购买后可永久下载此种子；旧站已购买的数据会直接显示为已购买，
                不会重复扣款。
              </AlertDialogDescription>
            </AlertDialogHeader>

            <div className="rounded-md border bg-muted/35 p-3 text-sm">
              <p className="line-clamp-2 font-medium">{torrentName}</p>
              <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2">
                <dt className="text-muted-foreground">价格</dt>
                <dd className="text-right font-semibold text-primary">
                  {purchaseStatus.data.price} 魔力值
                </dd>
                <dt className="text-muted-foreground">当前余额</dt>
                <dd className="text-right font-medium">
                  {purchaseStatus.data.magic_balance}
                </dd>
                <dt className="text-muted-foreground">发布者所得</dt>
                <dd className="text-right">
                  {purchaseStatus.data.seller_income}
                </dd>
                <dt className="text-muted-foreground">站点手续费</dt>
                <dd className="text-right">{purchaseStatus.data.tax}</dd>
              </dl>
              {insufficientBalance ? (
                <Badge variant="destructive" className="mt-3">
                  魔力值余额不足
                </Badge>
              ) : null}
            </div>

            {purchase.isError ? (
              <p className="text-sm text-destructive" role="alert">
                {requestErrorDescription(
                  purchase.error,
                  "购买未能完成，请稍后重试。"
                )}
              </p>
            ) : null}

            <AlertDialogFooter>
              <AlertDialogCancel disabled={purchase.isPending}>
                取消
              </AlertDialogCancel>
              <AlertDialogAction
                type="button"
                disabled={insufficientBalance || purchase.isPending}
                onClick={() => void confirmPurchase()}
              >
                {purchase.isPending ? <Spinner /> : null}
                {purchase.isPending ? "正在购买…" : "确认购买并下载"}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      ) : null}
    </>
  )
}

function downloadErrorDescription(error: Error) {
  if (!(error instanceof ApiProblemError)) {
    return "下载服务暂时不可用，请稍后重试"
  }
  if (error.status === 401) {
    return "请先登录后下载"
  }
  if (error.code === "torrent_purchase_required") {
    return "请打开种子详情页购买后下载"
  }
  if (error.code === "verified_email_required") {
    return "请先完成邮箱验证"
  }
  if (error.status === 403) {
    return "当前账户没有下载权限"
  }
  if (error.status === 404) {
    return "该种子不存在或尚未发布"
  }
  return "种子暂时无法下载，请稍后重试"
}

function integerLessThan(left: string, right: string) {
  try {
    return BigInt(left) < BigInt(right)
  } catch {
    return true
  }
}
