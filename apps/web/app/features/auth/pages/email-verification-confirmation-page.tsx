import { Link } from "react-router"
import {
  CircleCheckIcon,
  CircleXIcon,
  LoaderCircleIcon,
  MailCheckIcon,
} from "lucide-react"

import { Button } from "~/components/ui/button"
import { CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import { useConfirmEmailVerification } from "~/features/auth/api/email-verification.mutations"
import { AuthEntryCard } from "~/features/auth/components/auth-entry-card"
import { useFragmentToken } from "~/features/auth/hooks/use-fragment-token"
import { opaqueTokenPattern } from "~/features/auth/model/opaque-token"
import { ApiProblemError } from "~/shared/api/problem"

export function EmailVerificationConfirmationPage() {
  const token = useFragmentToken()

  return <EmailVerificationConfirmationContent key={token} token={token} />
}

function EmailVerificationConfirmationContent({ token }: { token: string }) {
  const confirmVerification = useConfirmEmailVerification()
  const validToken = opaqueTokenPattern.test(token)

  const status = !validToken
    ? "invalid"
    : confirmVerification.isPending
      ? "loading"
      : confirmVerification.data
        ? "success"
        : confirmVerification.isError
          ? "error"
          : "ready"

  const title =
    status === "loading"
      ? "正在验证…"
      : status === "success"
        ? "验证成功"
        : status === "ready"
          ? "确认邮箱"
          : "验证失败"

  return (
    <AuthEntryCard
      viewport="full"
      className="gap-0 py-0"
      aria-labelledby="email-confirmation-title"
    >
      <CardHeader className="gap-1.5 p-6 text-center">
        <div className="mx-auto mb-4">
          {status === "loading" ? (
            <div className="rounded-full bg-primary/10 p-4">
              <LoaderCircleIcon className="size-12 animate-spin text-primary" />
            </div>
          ) : status === "success" ? (
            <div className="rounded-full bg-success/10 p-4">
              <CircleCheckIcon className="size-12 text-success" />
            </div>
          ) : status === "ready" ? (
            <div className="rounded-full bg-primary/10 p-4">
              <MailCheckIcon className="size-12 text-primary" />
            </div>
          ) : (
            <div className="rounded-full bg-destructive/10 p-4">
              <CircleXIcon className="size-12 text-destructive" />
            </div>
          )}
        </div>
        <CardTitle>
          <h1
            id="email-confirmation-title"
            className="text-2xl leading-none font-semibold tracking-tight"
          >
            {title}
          </h1>
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 px-6 pb-6 text-center">
        <p className="leading-6 text-muted-foreground">
          {status === "invalid"
            ? "缺少验证令牌"
            : status === "success"
              ? `@${confirmVerification.data?.user.username} 现在可以使用已验证邮箱登录。`
              : status === "error"
                ? confirmationErrorTitle(confirmVerification.error)
                : status === "ready"
                  ? "点击下方按钮完成验证；打开页面本身不会修改账户。"
                  : "正在确认验证链接，请稍候。"}
        </p>
        {status === "error" ? (
          <p className="text-sm text-muted-foreground">
            {confirmationErrorDescription(confirmVerification.error)}
          </p>
        ) : null}
        {status === "ready" ? (
          <Button
            size="lg"
            className="h-10 w-full"
            onClick={() => confirmVerification.mutate(token)}
          >
            确认验证
          </Button>
        ) : null}
        {status === "success" ? (
          <Button
            className="w-full"
            render={<Link to="/login" />}
            nativeButton={false}
          >
            前往登录
          </Button>
        ) : null}
        {status === "invalid" || status === "error" ? (
          <div className="flex flex-col gap-2">
            <Button
              variant="outline"
              className="w-full"
              render={<Link to="/login" />}
              nativeButton={false}
            >
              返回登录
            </Button>
            <p className="text-xs text-muted-foreground">
              如果链接已过期，请登录后重新发送验证邮件
            </p>
          </div>
        ) : null}
        {status === "ready" ? (
          <div className="flex items-center justify-between text-sm">
            <Link to="/account/email">重新发送</Link>
            <Link to="/">返回首页</Link>
          </div>
        ) : null}
      </CardContent>
    </AuthEntryCard>
  )
}

function confirmationErrorTitle(error: Error | null) {
  return error instanceof ApiProblemError ? error.message : "验证失败"
}

function confirmationErrorDescription(error: Error | null) {
  if (error instanceof ApiProblemError) {
    return error.requestId
      ? `可以保留本页后重试。请求编号：${error.requestId}`
      : "链接可能已过期或已被新的验证邮件替换。"
  }
  return "服务暂时不可用，请稍后重试。"
}
