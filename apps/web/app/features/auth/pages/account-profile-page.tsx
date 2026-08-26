import { useEffect, useState, type FormEvent, type ReactNode } from "react"
import { Link } from "react-router"
import { CircleAlertIcon, CircleCheckIcon, LogInIcon } from "lucide-react"

import { Alert, AlertDescription } from "~/components/ui/alert"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
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
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { useUpdateMyProfile } from "~/features/auth/api/account-profile.mutations"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { AccountSettingsLayout } from "~/features/auth/components/account-settings-layout"
import { AvatarUpload } from "~/features/auth/components/avatar-upload"
import { EmailStatusCard } from "~/features/auth/components/email-status-card"
import { ApiProblemError } from "~/shared/api/problem"

export function AccountProfilePage() {
  const session = useWebSession()

  if (session.isPending) {
    return (
      <ProfileFrame>
        <Card className="gap-0 py-0">
          <CardHeader className="px-6 pt-6 pb-5">
            <Skeleton className="h-7 w-28" />
          </CardHeader>
          <CardContent className="flex flex-col gap-4 px-6 pb-6">
            <Skeleton className="size-16 rounded-full" />
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
          </CardContent>
        </Card>
      </ProfileFrame>
    )
  }

  if (!session.data) {
    return (
      <ProfileFrame>
        <Card className="gap-0 py-0">
          <CardHeader className="px-6 pt-6 pb-3">
            <CardTitle className="text-2xl leading-none">需要登录</CardTitle>
            <CardDescription>登录后可以查看自己的账户资料。</CardDescription>
          </CardHeader>
          <CardFooter className="border-0 bg-transparent px-6 pb-6">
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          </CardFooter>
        </Card>
      </ProfileFrame>
    )
  }

  const user = session.data.user
  return (
    <ProfileFrame>
      <ProfileEditor session={session.data} />

      <EmailStatusCard verified={user.email_verified} />
    </ProfileFrame>
  )
}

function ProfileEditor({
  session,
}: {
  session: NonNullable<ReturnType<typeof useWebSession>["data"]>
}) {
  const mutation = useUpdateMyProfile()
  const [displayName, setDisplayName] = useState(session.user.display_name)
  const [validationError, setValidationError] = useState("")

  useEffect(() => {
    setDisplayName(session.user.display_name)
  }, [session.user.id, session.user.display_name])

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const normalized = displayName.trim()
    if (normalized.length === 0 || Array.from(normalized).length > 40) {
      setValidationError("昵称需要包含 1 到 40 个字符")
      return
    }
    setValidationError("")
    mutation.mutate({
      displayName: normalized,
      csrfToken: session.csrf_token,
    })
  }

  const mutationError =
    mutation.error instanceof ApiProblemError
      ? mutation.error.message
      : mutation.error
        ? "保存失败，请稍后重试"
        : ""

  return (
    <Card className="gap-0 py-0">
      <CardHeader className="px-6 pt-6 pb-6">
        <CardTitle>
          <h2 className="text-2xl leading-none font-semibold">个人资料</h2>
        </CardTitle>
      </CardHeader>
      <CardContent className="px-6 pb-6">
        <form className="flex flex-col gap-4" onSubmit={submit} noValidate>
          <AvatarUpload
            username={session.user.username}
            displayName={session.user.display_name}
            csrfToken={session.csrf_token}
          />
          {mutation.isSuccess ? (
            <Alert className="border-success/30 text-success">
              <CircleCheckIcon />
              <AlertDescription>个人资料已保存。</AlertDescription>
            </Alert>
          ) : null}
          {validationError || mutationError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertDescription>
                {validationError || mutationError}
              </AlertDescription>
            </Alert>
          ) : null}
          <FieldGroup className="gap-4">
            <Field>
              <FieldLabel htmlFor="profile-display-name">昵称</FieldLabel>
              <Input
                id="profile-display-name"
                value={displayName}
                maxLength={40}
                className="h-10"
                onChange={(event) => {
                  setDisplayName(event.target.value)
                  setValidationError("")
                  mutation.reset()
                }}
              />
              <FieldDescription className="!-mt-1 text-xs leading-4">
                昵称将优先显示在评论和公共内容中（最多 40 个字符）。
              </FieldDescription>
            </Field>
            <ProfileField
              id="profile-username"
              label="用户名"
              value={session.user.username}
            />
            <ProfileField
              id="profile-email-status"
              label="邮箱状态"
              value={session.user.email_verified ? "已验证" : "待验证"}
            />
          </FieldGroup>
          <p className="text-xs text-muted-foreground">
            暂不支持修改用户名和邮箱。
          </p>
          <div>
            <Button
              type="submit"
              className="w-[88px]"
              disabled={mutation.isPending}
            >
              {mutation.isPending ? "保存中…" : "保存资料"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}

function ProfileFrame({ children }: { children: ReactNode }) {
  return (
    <AccountSettingsLayout
      active="profile"
      title="个人资料"
      description="查看当前账户的公开资料与验证状态。"
    >
      {children}
    </AccountSettingsLayout>
  )
}

function ProfileField({
  id,
  label,
  value,
  description,
}: {
  id: string
  label: string
  value: string
  description?: string
}) {
  return (
    <Field data-disabled>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input id={id} disabled value={value} className="h-10" />
      {description ? <FieldDescription>{description}</FieldDescription> : null}
    </Field>
  )
}
