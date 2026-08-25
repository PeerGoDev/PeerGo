import * as React from "react"
import { CircleAlertIcon, CoinsIcon, LoaderCircleIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
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
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import {
  type TorrentSettingsOverview,
  useUpdateTorrentPurchasePolicySettings,
} from "~/features/staff/api/operations.queries"
import { formatInteger } from "~/shared/formatters/integer"

type PurchaseRules = TorrentSettingsOverview["purchase"]

export function TorrentPurchasePolicyCard({
  purchase,
  csrfToken,
  canUpdate,
}: {
  purchase: PurchaseRules
  csrfToken: string
  canUpdate: boolean
}) {
  const mutation = useUpdateTorrentPurchasePolicySettings()
  const updatePurchasePolicyRequestId = React.useRef<string | undefined>(
    undefined
  )
  const [enabled, setEnabled] = React.useState(purchase.enabled)
  const [feePercent, setFeePercent] = React.useState(
    String(purchase.tax_basis_points / 100)
  )
  const [reason, setReason] = React.useState("")
  const [confirmationOpen, setConfirmationOpen] = React.useState(false)
  const [success, setSuccess] = React.useState("")

  React.useEffect(() => {
    if (mutation.isPending) return
    setEnabled(purchase.enabled)
    setFeePercent(String(purchase.tax_basis_points / 100))
  }, [mutation.isPending, purchase.enabled, purchase.tax_basis_points])

  const parsedFee = Number(feePercent)
  const basisPoints = Math.round(parsedFee * 100)
  const feeValid =
    feePercent !== "" &&
    Number.isFinite(parsedFee) &&
    parsedFee >= 0 &&
    parsedFee <= 100 &&
    Number.isInteger(parsedFee * 100)
  const reasonLength = Array.from(reason.trim()).length
  const changed =
    enabled !== purchase.enabled ||
    (feeValid && basisPoints !== purchase.tax_basis_points)

  async function handleConfirm() {
    if (!changed || !feeValid || reasonLength > 1000) {
      return
    }
    try {
      const idempotencyKey =
        updatePurchasePolicyRequestId.current ?? crypto.randomUUID()
      updatePurchasePolicyRequestId.current = idempotencyKey
      const result = await mutation.mutateAsync({
        csrfToken,
        idempotencyKey,
        body: {
          enabled,
          tax_basis_points: basisPoints,
          expected_revision: purchase.policy_revision,
          reason: reason.trim(),
        },
      })
      updatePurchasePolicyRequestId.current = undefined
      setReason("")
      setConfirmationOpen(false)
      setSuccess(
        `种子购买已${result.enabled ? "开放" : "暂停"}，站点手续费为 ${result.tax_basis_points / 100}%。`
      )
    } catch {
      setConfirmationOpen(false)
    }
  }

  return (
    <Card className="xl:col-span-1">
      <CardHeader>
        <CardTitle
          className="flex items-center gap-2"
          role="heading"
          aria-level={2}
        >
          <CoinsIcon className="size-4" />
          种子购买
        </CardTitle>
        <CardDescription>
          控制新购买与站点手续费；已购权限不受后续修改影响。
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {success ? (
          <Alert>
            <CoinsIcon />
            <AlertTitle>购买设置已更新</AlertTitle>
            <AlertDescription>{success}</AlertDescription>
          </Alert>
        ) : null}
        {mutation.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>购买设置未保存</AlertTitle>
            <AlertDescription>
              设置可能已被其他管理员修改，请刷新页面后重试。
            </AlertDescription>
          </Alert>
        ) : null}

        <FieldGroup>
          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>开放种子购买</FieldTitle>
              <FieldDescription>
                暂停后不接受新购买，已有购买用户仍可下载。
              </FieldDescription>
            </FieldContent>
            <Switch
              checked={enabled}
              disabled={!canUpdate || mutation.isPending}
              aria-label="开放种子购买"
              onCheckedChange={(value) => {
                setEnabled(value)
                setSuccess("")
                mutation.reset()
                updatePurchasePolicyRequestId.current = undefined
              }}
            />
          </Field>

          <Field data-invalid={(!feeValid && feePercent !== "") || undefined}>
            <FieldLabel htmlFor="torrent-purchase-fee">站点手续费</FieldLabel>
            <div className="relative">
              <Input
                id="torrent-purchase-fee"
                className="pr-9"
                type="number"
                inputMode="decimal"
                min={0}
                max={100}
                step={0.01}
                value={feePercent}
                disabled={!canUpdate || mutation.isPending}
                aria-invalid={(!feeValid && feePercent !== "") || undefined}
                onChange={(event) => {
                  setFeePercent(event.target.value)
                  setSuccess("")
                  mutation.reset()
                  updatePurchasePolicyRequestId.current = undefined
                }}
              />
              <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-sm text-muted-foreground">
                %
              </span>
            </div>
            <FieldDescription>
              从购买价格中扣除，剩余魔力值计入发布者账户。
            </FieldDescription>
            {!feeValid && feePercent !== "" ? (
              <FieldError>请输入 0–100，最多两位小数。</FieldError>
            ) : null}
          </Field>

          <Field data-invalid={reasonLength > 1000 || undefined}>
            <FieldLabel htmlFor="torrent-purchase-policy-reason">
              修改说明
            </FieldLabel>
            <Textarea
              id="torrent-purchase-policy-reason"
              rows={3}
              maxLength={1000}
              value={reason}
              disabled={!canUpdate || mutation.isPending}
              placeholder="例如：按站点现行购买规则调整手续费"
              onChange={(event) => {
                setReason(event.target.value)
                setSuccess("")
                mutation.reset()
                updatePurchasePolicyRequestId.current = undefined
              }}
            />
            <FieldDescription>{reasonLength}/1000 字符</FieldDescription>
            {reasonLength > 1000 ? (
              <FieldError>修改说明不能超过 1000 个字符。</FieldError>
            ) : null}
          </Field>
        </FieldGroup>

        <div className="grid grid-cols-3 gap-2 rounded-lg border bg-muted/20 p-3 text-center text-xs">
          <PurchaseMetric
            label="付费种子"
            value={formatInteger(purchase.priced_torrents)}
          />
          <PurchaseMetric
            label="旧站已购"
            value={formatInteger(purchase.legacy_entitlements)}
          />
          <PurchaseMetric
            label="新站已购"
            value={formatInteger(purchase.live_entitlements)}
          />
        </div>
        <p className="text-xs text-muted-foreground">
          扣款与永久权限在同一事务提交，保存设置不会回算历史购买。
        </p>

        {canUpdate ? (
          <Button
            type="button"
            disabled={
              mutation.isPending || !changed || !feeValid || reasonLength > 1000
            }
            onClick={() => setConfirmationOpen(true)}
          >
            保存购买设置
          </Button>
        ) : (
          <p className="text-sm text-muted-foreground">当前权限仅可查看。</p>
        )}
      </CardContent>

      <AlertDialog open={confirmationOpen} onOpenChange={setConfirmationOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <CoinsIcon />
            </AlertDialogMedia>
            <AlertDialogTitle>确认修改种子购买设置？</AlertDialogTitle>
            <AlertDialogDescription>
              新设置会立即用于之后的购买，历史扣款、发布者收入和永久权限保持原样。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="rounded-lg border bg-muted/30 p-3 text-sm">
            购买状态：{enabled ? "开放" : "暂停"} · 站点手续费：
            {feePercent}%
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={mutation.isPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={mutation.isPending}
              onClick={() => void handleConfirm()}
            >
              {mutation.isPending ? (
                <LoaderCircleIcon
                  className="animate-spin"
                  data-icon="inline-start"
                />
              ) : null}
              {mutation.isPending ? "保存中…" : "确认保存"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}

function PurchaseMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="font-medium tabular-nums">{value}</span>
      <span className="text-muted-foreground">{label}</span>
    </div>
  )
}
