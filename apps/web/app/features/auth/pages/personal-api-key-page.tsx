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
import { Checkbox } from "~/components/ui/checkbox"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "~/components/ui/field"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "~/components/ui/input-group"
import { Skeleton } from "~/components/ui/skeleton"
import {
  personalAPIKeyQueryOptions,
  useRevokePersonalAPIKey,
  useRotatePersonalAPIKey,
  type PersonalAPIKeyScope,
} from "~/features/auth/api/personal-api-key.queries"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { AccountSettingsLayout } from "~/features/auth/components/account-settings-layout"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

const availableScopes = [
  {
    value: "profile:read",
    label: "读取账户资料",
    description: "读取用户名、等级、流量和做种概况。",
  },
  {
    value: "torrent:read",
    label: "读取与搜索种子",
    description: "查看种子列表、分类与详情。",
  },
  {
    value: "torrent:download",
    label: "下载种子",
    description: "为当前账户生成受限、短时的种子下载链接。",
  },
  {
    value: "attendance:read",
    label: "读取签到状态",
    description: "查看今日签到状态和连续签到数据。",
  },
  {
    value: "attendance:claim",
    label: "执行签到",
    description: "允许外部工具代表当前账户完成签到。",
  },
] as const satisfies ReadonlyArray<{
  value: PersonalAPIKeyScope
  label: string
  description: string
}>

const defaultScopes = availableScopes.map((scope) => scope.value)
const scopeLabels = Object.fromEntries(
  availableScopes.map((scope) => [scope.value, scope.label])
) as Record<PersonalAPIKeyScope, string>

export function PersonalAPIKeyPage() {
  const session = useWebSession()
  const credential = useQuery({
    ...personalAPIKeyQueryOptions(session.data?.user.id),
    enabled: Boolean(session.data),
  })
  const rotate = useRotatePersonalAPIKey()
  const revoke = useRevokePersonalAPIKey()
  const [selectedScopes, setSelectedScopes] =
    React.useState<PersonalAPIKeyScope[]>(defaultScopes)
  const [issuedKey, setIssuedKey] = React.useState<string>()
  const [copied, setCopied] = React.useState(false)
  const [copyError, setCopyError] = React.useState("")
  const [confirmRotation, setConfirmRotation] = React.useState(false)
  const [confirmRevocation, setConfirmRevocation] = React.useState(false)

  React.useEffect(() => {
    if (!credential.data) return
    setSelectedScopes(
      credential.data.active ? [...credential.data.scopes] : [...defaultScopes]
    )
  }, [credential.data])

  async function createOrRotate(expectedVersion?: number) {
    if (!session.data || selectedScopes.length === 0) return
    const issued = await rotate.mutateAsync({
      csrfToken: session.data.csrf_token,
      expectedVersion,
      scopes: selectedScopes,
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

  function setScope(scope: PersonalAPIKeyScope, checked: boolean) {
    setSelectedScopes((current) =>
      checked
        ? defaultScopes.filter(
            (candidate) => candidate === scope || current.includes(candidate)
          )
        : current.filter((candidate) => candidate !== scope)
    )
  }

  const mutationError = rotate.error ?? revoke.error
  const canIssue = selectedScopes.length > 0 && !rotate.isPending

  return (
    <AccountSettingsLayout
      active="api-key"
      title="API Key"
      description="管理供外部工具共用的个人站点凭据与最小权限。"
    >
      <Alert>
        <ShieldCheckIcon />
        <AlertTitle>一把通用的个人 API Key</AlertTitle>
        <AlertDescription className="space-y-2">
          <p>
            同一把 Key 可用于 MoviePilot
            及后续接入的工具；每个工具只能调用你授予的权限。它不会替代浏览器登录，也不会出现在种子下载地址中。
          </p>
          <div className="flex flex-wrap gap-2">
            <Badge variant="outline">MoviePilot · 已支持</Badge>
          </div>
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
                aria-label="新个人 API Key"
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
        <PersonalAPIKeySkeleton />
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
              登录后可以创建和管理个人 API Key。
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
            <CardTitle>个人 API Key</CardTitle>
            <CardDescription>
              每个账户最多保留一个共用密钥；轮换会替换所有工具正在使用的旧密钥，撤销也会让尚未过期的下载能力失效。
            </CardDescription>
            <CardAction>
              <Badge variant={credential.data.active ? "default" : "secondary"}>
                {credential.data.active ? "已启用" : "未创建"}
              </Badge>
            </CardAction>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              {credential.data.active ? (
                <Field>
                  <FieldLabel htmlFor="personal-api-key-prefix">
                    当前密钥
                  </FieldLabel>
                  <InputGroup>
                    <InputGroupInput
                      id="personal-api-key-prefix"
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
                  <div className="flex flex-wrap gap-2">
                    {credential.data.scopes.map((scope) => (
                      <Badge key={scope} variant="outline">
                        {scopeLabels[scope]}
                      </Badge>
                    ))}
                  </div>
                </Field>
              ) : (
                <p className="text-sm text-muted-foreground">
                  创建后，将返回一次完整密钥。PeerGo 仅保存 SHA-256
                  哈希、权限和按六小时合并的最近使用状态，不保存可还原的原文或逐请求日志。
                </p>
              )}

              <FieldSet>
                <FieldLegend>授予权限</FieldLegend>
                <FieldDescription>
                  至少选择一项。现有密钥的修改会在轮换后生效。
                </FieldDescription>
                <div className="grid gap-2 sm:grid-cols-2">
                  {availableScopes.map((scope) => {
                    const id = `personal-api-key-scope-${scope.value.replace(":", "-")}`
                    return (
                      <Field key={scope.value} orientation="horizontal">
                        <Checkbox
                          id={id}
                          checked={selectedScopes.includes(scope.value)}
                          onCheckedChange={(checked) =>
                            setScope(scope.value, checked)
                          }
                          disabled={rotate.isPending || revoke.isPending}
                        />
                        <div className="space-y-0.5">
                          <FieldLabel htmlFor={id} className="font-normal">
                            {scope.label}
                          </FieldLabel>
                          <FieldDescription>
                            {scope.description}
                          </FieldDescription>
                        </div>
                      </Field>
                    )
                  })}
                </div>
                {selectedScopes.length === 0 ? (
                  <p className="text-sm text-destructive" role="alert">
                    请至少选择一项权限。
                  </p>
                ) : null}
              </FieldSet>
            </FieldGroup>
          </CardContent>
          <CardFooter className="flex flex-wrap gap-2">
            {credential.data.active ? (
              <>
                <Button
                  variant="outline"
                  onClick={() => setConfirmRotation(true)}
                  disabled={!canIssue || revoke.isPending}
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
                disabled={!canIssue}
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
            <AlertDialogTitle>轮换个人 API Key？</AlertDialogTitle>
            <AlertDialogDescription>
              旧密钥会立即失效，需要在所有已连接工具中替换为新密钥；当前选择的权限会同时生效。新原文仍只显示一次。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => void createOrRotate(credential.data?.version)}
              disabled={!canIssue}
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
            <AlertDialogTitle>撤销个人 API Key？</AlertDialogTitle>
            <AlertDialogDescription>
              MoviePilot 和其他已连接工具将无法再访问当前账户；此操作无法撤销。
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

function PersonalAPIKeySkeleton() {
  return (
    <Card>
      <CardHeader>
        <Skeleton className="h-5 w-44" />
        <Skeleton className="h-4 w-full max-w-lg" />
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-24 w-full" />
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
