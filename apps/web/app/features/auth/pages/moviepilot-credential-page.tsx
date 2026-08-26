import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CheckIcon,
  CircleAlertIcon,
  ClipboardIcon,
  KeyRoundIcon,
  LogInIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  Trash2Icon,
} from "lucide-react"

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
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "~/components/ui/input-group"
import { Skeleton } from "~/components/ui/skeleton"
import {
  moviePilotCredentialQueryOptions,
  useRevokeMoviePilotCredential,
  useRotateMoviePilotCredential,
} from "~/features/auth/api/moviepilot-credential.queries"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { AccountSettingsLayout } from "~/features/auth/components/account-settings-layout"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

const scopeLabels: Record<string, string> = {
  "profile:read": "读取账户资料",
  "torrent:read": "搜索种子",
  "torrent:download": "下载种子",
  "attendance:read": "读取签到状态",
  "attendance:claim": "执行签到",
}

export function MoviePilotCredentialPage() {
  const session = useWebSession()
  const credential = useQuery({
    ...moviePilotCredentialQueryOptions(session.data?.user.id),
    enabled: Boolean(session.data),
  })
  const rotate = useRotateMoviePilotCredential()
  const revoke = useRevokeMoviePilotCredential()
  const [issuedKey, setIssuedKey] = React.useState<string>()
  const [copied, setCopied] = React.useState(false)
  const [copyError, setCopyError] = React.useState("")
  const [confirmRotation, setConfirmRotation] = React.useState(false)
  const [confirmRevocation, setConfirmRevocation] = React.useState(false)

  async function createOrRotate(expectedVersion?: number) {
    if (!session.data) return
    const issued = await rotate.mutateAsync({
      csrfToken: session.data.csrf_token,
      expectedVersion,
    })
    setIssuedKey(issued.api_key)
    setCopied(false)
    setCopyError("")
    setConfirmRotation(false)
  }

  async function revokeCredential() {
    if (!session.data || credential.data?.version === undefined) return
    await revoke.mutateAsync({
      csrfToken: session.data.csrf_token,
      expectedVersion: credential.data.version,
    })
    setIssuedKey(undefined)
    setConfirmRevocation(false)
  }

  async function copyIssuedKey() {
    if (!issuedKey) return
    try {
      await navigator.clipboard.writeText(issuedKey)
      setCopied(true)
      setCopyError("")
    } catch {
      setCopied(false)
      setCopyError("复制失败，请手动选择并复制。")
    }
  }

  const mutationError = rotate.error ?? revoke.error

  return (
    <AccountSettingsLayout
      active="api-key"
      title="API Key"
      description="管理 MoviePilot 使用的独立站点凭据。"
    >
      <Alert>
        <ShieldCheckIcon />
        <AlertTitle>这是 MoviePilot 专用凭据</AlertTitle>
        <AlertDescription>
          在 MoviePilot 的 Rousi 站点配置中填入此
          Key。它不会替代浏览器登录，也不会出现在种子下载地址中。
        </AlertDescription>
      </Alert>

      {issuedKey ? (
        <Alert>
          <KeyRoundIcon />
          <AlertTitle>立即保存新 API Key</AlertTitle>
          <AlertDescription className="flex flex-col gap-3">
            <p>原文只显示这一次；关闭页面后无法再次查看，只能重新轮换。</p>
            <InputGroup>
              <InputGroupInput
                aria-label="新 MoviePilot API Key"
                value={issuedKey}
                readOnly
                className="font-mono"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupButton
                  aria-label="复制 API Key"
                  onClick={() => void copyIssuedKey()}
                >
                  {copied ? (
                    <CheckIcon data-icon="inline-start" />
                  ) : (
                    <ClipboardIcon data-icon="inline-start" />
                  )}
                  {copied ? "已复制" : "复制"}
                </InputGroupButton>
              </InputGroupAddon>
            </InputGroup>
            {copyError ? <p className="text-destructive">{copyError}</p> : null}
            <div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setIssuedKey(undefined)}
              >
                我已保存，隐藏密钥
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      ) : null}

      {session.isPending || (session.data && credential.isPending) ? (
        <MoviePilotCredentialSkeleton />
      ) : null}

      {session.isError || credential.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>无法读取 API Key 状态</AlertTitle>
          <AlertDescription className="flex flex-col gap-3">
            <p>
              {requestErrorDescription(
                session.error ?? credential.error,
                "请稍后重试。"
              )}
            </p>
            <div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  void session.refetch()
                  void credential.refetch()
                }}
              >
                <RefreshCwIcon data-icon="inline-start" />
                重试
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      ) : null}

      {!session.isPending && !session.isError && !session.data ? (
        <Card>
          <CardHeader>
            <CardTitle>需要登录</CardTitle>
            <CardDescription>
              登录后可以创建和管理 MoviePilot API Key。
            </CardDescription>
          </CardHeader>
          <CardFooter>
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          </CardFooter>
        </Card>
      ) : null}

      {session.data && credential.data ? (
        <Card>
          <CardHeader>
            <CardTitle>MoviePilot API Key</CardTitle>
            <CardDescription>
              每个账户最多保留一个密钥；轮换会立刻替换旧密钥，撤销会同时让未过期下载链接失效。
            </CardDescription>
            <CardAction>
              <Badge variant={credential.data.active ? "default" : "secondary"}>
                {credential.data.active ? "已启用" : "未创建"}
              </Badge>
            </CardAction>
          </CardHeader>
          <CardContent>
            {credential.data.active ? (
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="moviepilot-key-prefix">
                    当前密钥
                  </FieldLabel>
                  <InputGroup>
                    <InputGroupInput
                      id="moviepilot-key-prefix"
                      value={`${credential.data.key_prefix ?? "pgk_"}••••••••••••••••`}
                      readOnly
                      className="font-mono"
                    />
                    <InputGroupAddon align="inline-end">
                      仅显示前缀
                    </InputGroupAddon>
                  </InputGroup>
                  <FieldDescription>
                    创建于 {formatOptionalDate(credential.data.created_at)}
                    ；最近使用于{" "}
                    {formatOptionalDate(
                      credential.data.last_used_at,
                      "尚未使用"
                    )}
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel>固定权限范围</FieldLabel>
                  <div className="flex flex-wrap gap-2">
                    {credential.data.scopes.map((scope) => (
                      <Badge key={scope} variant="outline">
                        {scopeLabels[scope] ?? scope}
                      </Badge>
                    ))}
                  </div>
                  <FieldDescription>
                    不支持任意扩权，也不会记录每次搜索或下载的数据库日志。
                  </FieldDescription>
                </Field>
              </FieldGroup>
            ) : (
              <p className="text-sm text-muted-foreground">
                创建后，将返回一次完整密钥。PeerGo 仅保存 SHA-256
                哈希和状态，不保存可还原的原文。
              </p>
            )}
          </CardContent>
          <CardFooter className="flex flex-wrap gap-2">
            {credential.data.active ? (
              <>
                <Button
                  variant="outline"
                  onClick={() => setConfirmRotation(true)}
                  disabled={rotate.isPending || revoke.isPending}
                >
                  <RefreshCwIcon data-icon="inline-start" />
                  轮换密钥
                </Button>
                <Button
                  variant="destructive"
                  onClick={() => setConfirmRevocation(true)}
                  disabled={rotate.isPending || revoke.isPending}
                >
                  <Trash2Icon data-icon="inline-start" />
                  撤销密钥
                </Button>
              </>
            ) : (
              <Button
                onClick={() => void createOrRotate()}
                disabled={rotate.isPending}
              >
                <KeyRoundIcon data-icon="inline-start" />
                {rotate.isPending ? "创建中…" : "创建 API Key"}
              </Button>
            )}
          </CardFooter>
        </Card>
      ) : null}

      {mutationError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>API Key 未能更新</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(mutationError, "请刷新状态后重试。")}
          </AlertDescription>
        </Alert>
      ) : null}

      <AlertDialog open={confirmRotation} onOpenChange={setConfirmRotation}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <RefreshCwIcon />
            </AlertDialogMedia>
            <AlertDialogTitle>轮换 MoviePilot API Key？</AlertDialogTitle>
            <AlertDialogDescription>
              旧密钥会立即失效，需要在 MoviePilot
              中替换为新密钥。新原文仍只显示一次。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => void createOrRotate(credential.data?.version)}
              disabled={rotate.isPending}
            >
              {rotate.isPending ? "轮换中…" : "确认轮换"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={confirmRevocation} onOpenChange={setConfirmRevocation}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <Trash2Icon />
            </AlertDialogMedia>
            <AlertDialogTitle>撤销 MoviePilot API Key？</AlertDialogTitle>
            <AlertDialogDescription>
              MoviePilot 将无法再读取账户、搜索、下载或签到；此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => void revokeCredential()}
              disabled={revoke.isPending}
            >
              {revoke.isPending ? "撤销中…" : "确认撤销"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </AccountSettingsLayout>
  )
}

function MoviePilotCredentialSkeleton() {
  return (
    <Card>
      <CardHeader>
        <Skeleton className="h-5 w-44" />
        <Skeleton className="h-4 w-full max-w-lg" />
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-6 w-80 max-w-full" />
      </CardContent>
      <CardFooter>
        <Skeleton className="h-8 w-24" />
      </CardFooter>
    </Card>
  )
}

function formatOptionalDate(value: string | undefined, fallback = "未知") {
  return value ? formatDateTime(value) : fallback
}
