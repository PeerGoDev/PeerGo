import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { useBeforeUnload, useBlocker } from "react-router"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  ClipboardCheckIcon,
  Globe2Icon,
  KeyRoundIcon,
  LockKeyholeIcon,
  RefreshCwIcon,
  SaveIcon,
  ShieldCheckIcon,
  TriangleAlertIcon,
  UserRoundPlusIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
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
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Separator } from "~/components/ui/separator"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type RegistrationPolicySettings,
  registrationPolicySettingsQueryOptions,
  useUpdateRegistrationPolicySettings,
} from "~/features/staff/api/registration-policy-settings.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { NewcomerPolicySettingsCard } from "~/features/staff/components/newcomer-policy-settings-card"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import {
  type RegistrationPolicySettingsFormValues,
  emailDomainModeLabel,
  humanVerificationProviderLabel,
  registrationModeLabel,
  registrationPolicySettingsFormSchema,
} from "~/features/staff/model/registration-policy-settings-form"
import type { components } from "~/generated/api"
import { ApiProblemError } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

type CapabilityList = components["schemas"]["CapabilityList"]
type RegistrationMode = RegistrationPolicySettings["mode"]
type EmailDomainMode = RegistrationPolicySettings["email_domain_mode"]
type HumanVerificationProvider =
  RegistrationPolicySettings["human_verification_provider"]

export function StaffRegistrationSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="site.registration.manage.read"
      pageHeader={{
        title: "注册设置",
        description: "管理新用户注册、账户规则、登录期限和成员邀请。",
      }}
    >
      {({ session, capabilities }) => (
        <RegistrationSettingsContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function RegistrationSettingsContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const settings = useQuery(registrationPolicySettingsQueryOptions)
  const canUpdate = hasCapability(capabilities, "site.registration.update")
  const canReadNewcomerPolicy = hasCapability(
    capabilities,
    "newcomer.policy.read"
  )
  const canIssueNewcomerPolicy = hasCapability(
    capabilities,
    "newcomer.policy.issue"
  )

  if (settings.isPending) {
    return (
      <StaffPageFrame>
        <RegistrationSettingsCard>
          <div
            role="status"
            aria-label="正在读取注册设置"
            className="flex flex-col gap-4"
          >
            <Skeleton className="h-6 w-40" aria-hidden="true" />
            <Skeleton className="h-10 w-full" aria-hidden="true" />
            <Skeleton className="h-10 w-full" aria-hidden="true" />
            <Skeleton className="h-24 w-full" aria-hidden="true" />
          </div>
        </RegistrationSettingsCard>
      </StaffPageFrame>
    )
  }

  if (settings.isError || !settings.data) {
    return (
      <StaffPageFrame>
        <RegistrationSettingsCard>
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>注册设置暂时无法读取</AlertTitle>
            <AlertDescription>
              暂时无法取得当前准入策略，请稍后重试。
            </AlertDescription>
            <AlertAction>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void settings.refetch()}
              >
                <RefreshCwIcon data-icon="inline-start" />
                重试
              </Button>
            </AlertAction>
          </Alert>
        </RegistrationSettingsCard>
      </StaffPageFrame>
    )
  }

  return (
    <StaffPageFrame>
      <div className="flex flex-col gap-6">
        <RegistrationPolicyForm
          initialSettings={settings.data}
          csrfToken={csrfToken}
          canUpdate={canUpdate}
        />
        {canReadNewcomerPolicy ? (
          <NewcomerPolicySettingsCard
            csrfToken={csrfToken}
            canIssue={canIssueNewcomerPolicy}
          />
        ) : null}
      </div>
    </StaffPageFrame>
  )
}

function RegistrationPolicyForm({
  initialSettings,
  csrfToken,
  canUpdate,
}: {
  initialSettings: RegistrationPolicySettings
  csrfToken: string
  canUpdate: boolean
}) {
  const mutation = useUpdateRegistrationPolicySettings()
  const [baseline, setBaseline] = React.useState(initialSettings)
  const [mode, setMode] = React.useState<RegistrationMode>(initialSettings.mode)
  const [memberInvitesEnabled, setMemberInvitesEnabled] = React.useState(
    initialSettings.member_invites_enabled
  )
  const [inviteValidDays, setInviteValidDays] = React.useState(
    initialSettings.invite_valid_days
  )
  const [maxInvitesPerMember, setMaxInvitesPerMember] = React.useState(
    initialSettings.max_invites_per_member
  )
  const [minimumInviteAccountAgeDays, setMinimumInviteAccountAgeDays] =
    React.useState(initialSettings.minimum_invite_account_age_days)
  const [minimumInviteLevel, setMinimumInviteLevel] = React.useState(
    initialSettings.minimum_invite_level
  )
  const [usernameMinCharacters, setUsernameMinCharacters] = React.useState(
    initialSettings.username_min_characters
  )
  const [usernameMaxCharacters, setUsernameMaxCharacters] = React.useState(
    initialSettings.username_max_characters
  )
  const [reservedUsernames, setReservedUsernames] = React.useState(
    initialSettings.reserved_usernames.join("\n")
  )
  const [emailDomainMode, setEmailDomainMode] = React.useState<EmailDomainMode>(
    initialSettings.email_domain_mode
  )
  const [emailDomains, setEmailDomains] = React.useState(
    initialSettings.email_domains.join("\n")
  )
  const [sessionValidHours, setSessionValidHours] = React.useState(
    initialSettings.session_valid_hours
  )
  const [rememberSessionValidHours, setRememberSessionValidHours] =
    React.useState(initialSettings.remember_session_valid_hours)
  const [humanVerificationProvider, setHumanVerificationProvider] =
    React.useState<HumanVerificationProvider>(
      initialSettings.human_verification_provider
    )
  const [humanVerificationSiteKey, setHumanVerificationSiteKey] =
    React.useState(initialSettings.human_verification_site_key)
  const [
    humanVerificationRegistrationEnabled,
    setHumanVerificationRegistrationEnabled,
  ] = React.useState(initialSettings.human_verification_registration_enabled)
  const [humanVerificationLoginEnabled, setHumanVerificationLoginEnabled] =
    React.useState(initialSettings.human_verification_login_enabled)
  const [
    humanVerificationPasswordRecoveryEnabled,
    setHumanVerificationPasswordRecoveryEnabled,
  ] = React.useState(
    initialSettings.human_verification_password_recovery_enabled
  )
  const [reason, setReason] = React.useState("")
  const [reasonError, setReasonError] = React.useState("")
  const [validationError, setValidationError] = React.useState("")
  const [pendingValues, setPendingValues] =
    React.useState<RegistrationPolicySettingsFormValues>()
  const [confirmationOpen, setConfirmationOpen] = React.useState(false)
  const [successMessage, setSuccessMessage] = React.useState("")

  React.useEffect(() => {
    if (mutation.isPending || initialSettings.version === baseline.version) {
      return
    }
    setBaseline(initialSettings)
    setMode(initialSettings.mode)
    setMemberInvitesEnabled(initialSettings.member_invites_enabled)
    setInviteValidDays(initialSettings.invite_valid_days)
    setMaxInvitesPerMember(initialSettings.max_invites_per_member)
    setMinimumInviteAccountAgeDays(
      initialSettings.minimum_invite_account_age_days
    )
    setMinimumInviteLevel(initialSettings.minimum_invite_level)
    setUsernameMinCharacters(initialSettings.username_min_characters)
    setUsernameMaxCharacters(initialSettings.username_max_characters)
    setReservedUsernames(initialSettings.reserved_usernames.join("\n"))
    setEmailDomainMode(initialSettings.email_domain_mode)
    setEmailDomains(initialSettings.email_domains.join("\n"))
    setSessionValidHours(initialSettings.session_valid_hours)
    setRememberSessionValidHours(initialSettings.remember_session_valid_hours)
    setHumanVerificationProvider(initialSettings.human_verification_provider)
    setHumanVerificationSiteKey(initialSettings.human_verification_site_key)
    setHumanVerificationRegistrationEnabled(
      initialSettings.human_verification_registration_enabled
    )
    setHumanVerificationLoginEnabled(
      initialSettings.human_verification_login_enabled
    )
    setHumanVerificationPasswordRecoveryEnabled(
      initialSettings.human_verification_password_recovery_enabled
    )
    setReason("")
    setReasonError("")
    setValidationError("")
    setPendingValues(undefined)
    setConfirmationOpen(false)
  }, [baseline.version, initialSettings, mutation.isPending])

  const changed =
    mode !== baseline.mode ||
    memberInvitesEnabled !== baseline.member_invites_enabled ||
    inviteValidDays !== baseline.invite_valid_days ||
    maxInvitesPerMember !== baseline.max_invites_per_member ||
    minimumInviteAccountAgeDays !== baseline.minimum_invite_account_age_days ||
    minimumInviteLevel !== baseline.minimum_invite_level ||
    usernameMinCharacters !== baseline.username_min_characters ||
    usernameMaxCharacters !== baseline.username_max_characters ||
    policyEntries(reservedUsernames).join("\0") !==
      baseline.reserved_usernames.join("\0") ||
    emailDomainMode !== baseline.email_domain_mode ||
    policyEntries(emailDomains).join("\0") !==
      baseline.email_domains.join("\0") ||
    sessionValidHours !== baseline.session_valid_hours ||
    rememberSessionValidHours !== baseline.remember_session_valid_hours ||
    humanVerificationProvider !== baseline.human_verification_provider ||
    humanVerificationSiteKey.trim() !== baseline.human_verification_site_key ||
    humanVerificationRegistrationEnabled !==
      baseline.human_verification_registration_enabled ||
    humanVerificationLoginEnabled !==
      baseline.human_verification_login_enabled ||
    humanVerificationPasswordRecoveryEnabled !==
      baseline.human_verification_password_recovery_enabled
  const dirty = changed || reason.trim().length > 0
  const blocker = useBlocker(
    React.useCallback(
      () => dirty && !mutation.isPending,
      [dirty, mutation.isPending]
    )
  )
  useBeforeUnload(
    React.useCallback(
      (event) => {
        if (dirty && !mutation.isPending) {
          event.preventDefault()
          event.returnValue = ""
        }
      },
      [dirty, mutation.isPending]
    )
  )

  function handleReview(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.reset()
    setSuccessMessage("")
    setValidationError("")
    if (!changed) {
      setReasonError("注册设置没有变化，无需创建空版本。")
      return
    }
    const result = registrationPolicySettingsFormSchema.safeParse({
      mode,
      memberInvitesEnabled,
      inviteValidDays,
      maxInvitesPerMember,
      minimumInviteAccountAgeDays,
      minimumInviteLevel,
      usernameMinCharacters,
      usernameMaxCharacters,
      reservedUsernames,
      emailDomainMode,
      emailDomains,
      sessionValidHours,
      rememberSessionValidHours,
      humanVerificationProvider,
      humanVerificationSiteKey,
      humanVerificationRegistrationEnabled,
      humanVerificationLoginEnabled,
      humanVerificationPasswordRecoveryEnabled,
      humanVerificationSecretConfigured:
        initialSettings.human_verification_secret_configured ?? false,
      reason,
    })
    if (!result.success) {
      const reasonIssue = result.error.issues.find(
        (issue) => issue.path[0] === "reason"
      )
      setReasonError(reasonIssue?.message ?? "")
      setValidationError(
        reasonIssue
          ? ""
          : (result.error.issues[0]?.message ?? "请检查注册设置。")
      )
      return
    }
    setReasonError("")
    setPendingValues(result.data)
    setConfirmationOpen(true)
  }

  async function handleConfirm() {
    if (!pendingValues) {
      return
    }
    try {
      const updated = await mutation.mutateAsync({
        csrfToken,
        body: {
          mode: pendingValues.mode,
          member_invites_enabled: pendingValues.memberInvitesEnabled,
          invite_valid_days: pendingValues.inviteValidDays,
          max_invites_per_member: pendingValues.maxInvitesPerMember,
          minimum_invite_account_age_days:
            pendingValues.minimumInviteAccountAgeDays,
          minimum_invite_level: pendingValues.minimumInviteLevel,
          username_min_characters: pendingValues.usernameMinCharacters,
          username_max_characters: pendingValues.usernameMaxCharacters,
          reserved_usernames: pendingValues.reservedUsernames,
          email_domain_mode: pendingValues.emailDomainMode,
          email_domains: pendingValues.emailDomains,
          session_valid_hours: pendingValues.sessionValidHours,
          remember_session_valid_hours: pendingValues.rememberSessionValidHours,
          human_verification_provider: pendingValues.humanVerificationProvider,
          human_verification_site_key: pendingValues.humanVerificationSiteKey,
          human_verification_registration_enabled:
            pendingValues.humanVerificationRegistrationEnabled,
          human_verification_login_enabled:
            pendingValues.humanVerificationLoginEnabled,
          human_verification_password_recovery_enabled:
            pendingValues.humanVerificationPasswordRecoveryEnabled,
          expected_version: baseline.version,
          reason: pendingValues.reason,
        },
      })
      setBaseline(updated)
      setMode(updated.mode)
      setMemberInvitesEnabled(updated.member_invites_enabled)
      setInviteValidDays(updated.invite_valid_days)
      setMaxInvitesPerMember(updated.max_invites_per_member)
      setMinimumInviteAccountAgeDays(updated.minimum_invite_account_age_days)
      setMinimumInviteLevel(updated.minimum_invite_level)
      setUsernameMinCharacters(updated.username_min_characters)
      setUsernameMaxCharacters(updated.username_max_characters)
      setReservedUsernames(updated.reserved_usernames.join("\n"))
      setEmailDomainMode(updated.email_domain_mode)
      setEmailDomains(updated.email_domains.join("\n"))
      setSessionValidHours(updated.session_valid_hours)
      setRememberSessionValidHours(updated.remember_session_valid_hours)
      setHumanVerificationProvider(updated.human_verification_provider)
      setHumanVerificationSiteKey(updated.human_verification_site_key)
      setHumanVerificationRegistrationEnabled(
        updated.human_verification_registration_enabled
      )
      setHumanVerificationLoginEnabled(updated.human_verification_login_enabled)
      setHumanVerificationPasswordRecoveryEnabled(
        updated.human_verification_password_recovery_enabled
      )
      setReason("")
      setValidationError("")
      setPendingValues(undefined)
      setConfirmationOpen(false)
      setSuccessMessage(
        `注册设置已生效，当前注册模式为${registrationModeLabel(updated.mode)}，当前为第 ${updated.version} 版。`
      )
    } catch {
      setConfirmationOpen(false)
    }
  }

  const disabled = !canUpdate || mutation.isPending

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {successMessage ? (
        <Alert>
          <CircleCheckIcon />
          <AlertTitle>注册设置已更新</AlertTitle>
          <AlertDescription>{successMessage}</AlertDescription>
        </Alert>
      ) : null}

      {mutation.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>{mutationErrorTitle(mutation.error)}</AlertTitle>
          <AlertDescription>
            {mutationErrorDescription(mutation.error)}
          </AlertDescription>
        </Alert>
      ) : null}

      {validationError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>请检查账户准入设置</AlertTitle>
          <AlertDescription>{validationError}</AlertDescription>
        </Alert>
      ) : null}

      {!canUpdate ? (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>当前权限仅可查看</AlertTitle>
          <AlertDescription>
            可以查看当前准入策略，但不能保存注册或邀请设置变更。
          </AlertDescription>
        </Alert>
      ) : null}

      <RegistrationSettingsCard
        saveAction={
          canUpdate ? (
            <Button
              type="submit"
              form="registration-policy-settings-form"
              className="w-28"
              disabled={!changed || mutation.isPending}
            >
              {mutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <SaveIcon data-icon="inline-start" />
              )}
              {mutation.isPending ? "保存中…" : "保存修改"}
            </Button>
          ) : undefined
        }
      >
        <form
          id="registration-policy-settings-form"
          onSubmit={handleReview}
          noValidate
        >
          <FieldGroup>
            <Field data-disabled={disabled}>
              <FieldTitle id="registration-mode-label">注册模式</FieldTitle>
              <ToggleGroup
                value={[mode]}
                onValueChange={(values) => {
                  const nextMode = values[0]
                  if (
                    nextMode === "open" ||
                    nextMode === "invite" ||
                    nextMode === "closed"
                  ) {
                    setMode(nextMode)
                    setReasonError("")
                    setValidationError("")
                  }
                }}
                variant="outline"
                spacing={0}
                className="w-full"
                disabled={disabled}
                aria-labelledby="registration-mode-label"
              >
                <ToggleGroupItem value="open" className="h-11 flex-1">
                  <Globe2Icon data-icon="inline-start" />
                  开放注册
                </ToggleGroupItem>
                <ToggleGroupItem value="invite" className="h-11 flex-1">
                  <KeyRoundIcon data-icon="inline-start" />
                  邀请注册
                </ToggleGroupItem>
                <ToggleGroupItem value="closed" className="h-11 flex-1">
                  <LockKeyholeIcon data-icon="inline-start" />
                  关闭注册
                </ToggleGroupItem>
              </ToggleGroup>
              <FieldDescription>
                开放允许任何访客注册；邀请要求有效邀请码；关闭会拒绝全部新注册，现有账户不受影响。
              </FieldDescription>
            </Field>

            <Separator />

            <FieldSet>
              <FieldLegend>账户命名与邮箱准入</FieldLegend>
              <FieldDescription>
                这些规则只检查后续新注册；历史用户不会因用户名长度或邮箱域名变化而失效。
              </FieldDescription>
              <FieldGroup>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field data-disabled={disabled}>
                    <FieldLabel htmlFor="username-min-characters">
                      用户名最少字符
                    </FieldLabel>
                    <Input
                      id="username-min-characters"
                      type="number"
                      min={3}
                      max={32}
                      value={usernameMinCharacters}
                      onChange={(event) => {
                        setUsernameMinCharacters(
                          event.currentTarget.valueAsNumber || 0
                        )
                        setReasonError("")
                        setValidationError("")
                      }}
                      disabled={disabled}
                    />
                    <FieldDescription>可设置 3–32 个字符。</FieldDescription>
                  </Field>
                  <Field data-disabled={disabled}>
                    <FieldLabel htmlFor="username-max-characters">
                      用户名最多字符
                    </FieldLabel>
                    <Input
                      id="username-max-characters"
                      type="number"
                      min={3}
                      max={32}
                      value={usernameMaxCharacters}
                      onChange={(event) => {
                        setUsernameMaxCharacters(
                          event.currentTarget.valueAsNumber || 0
                        )
                        setReasonError("")
                        setValidationError("")
                      }}
                      disabled={disabled}
                    />
                    <FieldDescription>
                      默认 20；迁移用户不受此限制。
                    </FieldDescription>
                  </Field>
                </div>

                <Field data-disabled={disabled}>
                  <FieldLabel htmlFor="reserved-usernames">
                    保留用户名
                  </FieldLabel>
                  <Textarea
                    id="reserved-usernames"
                    rows={4}
                    value={reservedUsernames}
                    onChange={(event) => {
                      setReservedUsernames(event.currentTarget.value)
                      setReasonError("")
                      setValidationError("")
                    }}
                    disabled={disabled}
                    placeholder={"admin\nroot\npeergo"}
                  />
                  <FieldDescription>
                    每行或逗号分隔；保存时会转为小写、去重并排序，最多 200 个。
                  </FieldDescription>
                </Field>

                <Field data-disabled={disabled}>
                  <FieldTitle id="email-domain-mode-label">
                    邮箱域名策略
                  </FieldTitle>
                  <ToggleGroup
                    value={[emailDomainMode]}
                    onValueChange={(values) => {
                      const nextMode = values[0]
                      if (
                        nextMode === "any" ||
                        nextMode === "allowlist" ||
                        nextMode === "blocklist"
                      ) {
                        setEmailDomainMode(nextMode)
                        setReasonError("")
                        setValidationError("")
                      }
                    }}
                    variant="outline"
                    spacing={0}
                    className="w-full"
                    disabled={disabled}
                    aria-labelledby="email-domain-mode-label"
                  >
                    <ToggleGroupItem value="any" className="h-10 flex-1">
                      不限制
                    </ToggleGroupItem>
                    <ToggleGroupItem value="allowlist" className="h-10 flex-1">
                      仅白名单
                    </ToggleGroupItem>
                    <ToggleGroupItem value="blocklist" className="h-10 flex-1">
                      排除黑名单
                    </ToggleGroupItem>
                  </ToggleGroup>
                  <FieldDescription>
                    白名单只接受列出的域名；黑名单拒绝列出的域名；均按完整域名精确匹配。
                  </FieldDescription>
                </Field>

                <Field data-disabled={disabled || emailDomainMode === "any"}>
                  <FieldLabel htmlFor="email-domains">邮箱域名列表</FieldLabel>
                  <Textarea
                    id="email-domains"
                    rows={4}
                    value={emailDomains}
                    onChange={(event) => {
                      setEmailDomains(event.currentTarget.value)
                      setReasonError("")
                      setValidationError("")
                    }}
                    disabled={disabled || emailDomainMode === "any"}
                    placeholder={"example.com\nexample.org"}
                  />
                  <FieldDescription>
                    不填写 @，每行或逗号分隔，最多 100 个域名。
                  </FieldDescription>
                </Field>
              </FieldGroup>
            </FieldSet>

            <Separator />

            <FieldSet>
              <FieldLegend>登录会话</FieldLegend>
              <FieldDescription>
                修改后只影响新登录；已经签发的会话保持原到期时间，不会被静默延长。
              </FieldDescription>
              <FieldGroup>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field data-disabled={disabled}>
                    <FieldLabel htmlFor="session-valid-hours">
                      普通登录有效期（小时）
                    </FieldLabel>
                    <Input
                      id="session-valid-hours"
                      type="number"
                      min={1}
                      max={720}
                      value={sessionValidHours}
                      onChange={(event) => {
                        setSessionValidHours(
                          event.currentTarget.valueAsNumber || 0
                        )
                        setReasonError("")
                        setValidationError("")
                      }}
                      disabled={disabled}
                    />
                    <FieldDescription>
                      1–720 小时；默认 168 小时（7 天）。
                    </FieldDescription>
                  </Field>
                  <Field data-disabled={disabled}>
                    <FieldLabel htmlFor="remember-session-valid-hours">
                      记住登录有效期（小时）
                    </FieldLabel>
                    <Input
                      id="remember-session-valid-hours"
                      type="number"
                      min={24}
                      max={2160}
                      value={rememberSessionValidHours}
                      onChange={(event) => {
                        setRememberSessionValidHours(
                          event.currentTarget.valueAsNumber || 0
                        )
                        setReasonError("")
                        setValidationError("")
                      }}
                      disabled={disabled}
                    />
                    <FieldDescription>
                      24–2160 小时，且不能短于普通登录有效期；默认 720 小时（30
                      天）。
                    </FieldDescription>
                  </Field>
                </div>
              </FieldGroup>
            </FieldSet>

            <Separator />

            <FieldSet>
              <FieldLegend>人机验证</FieldLegend>
              <FieldDescription>
                可按页面启用 Cloudflare Turnstile。浏览器只取得公开 site
                key，secret 始终由 Core 的部署环境保管。
              </FieldDescription>
              <FieldGroup>
                {!initialSettings.human_verification_secret_configured ? (
                  <Alert className="border-warning/40 bg-warning/10">
                    <TriangleAlertIcon />
                    <AlertTitle>Core 尚未配置 Turnstile secret</AlertTitle>
                    <AlertDescription>
                      请先在部署环境设置 PEERGO_TURNSTILE_SECRET_KEY 并重启
                      Core，之后才能在这里启用。
                    </AlertDescription>
                  </Alert>
                ) : (
                  <Alert>
                    <ShieldCheckIcon />
                    <AlertTitle>服务端验证密钥已就绪</AlertTitle>
                    <AlertDescription>
                      secret 不会通过接口返回，也不会写入版本化站点设置。
                    </AlertDescription>
                  </Alert>
                )}

                <Field data-disabled={disabled}>
                  <FieldTitle id="human-verification-provider-label">
                    验证方式
                  </FieldTitle>
                  <ToggleGroup
                    value={[humanVerificationProvider]}
                    onValueChange={(values) => {
                      const nextProvider = values[0]
                      if (nextProvider === "disabled") {
                        setHumanVerificationProvider(nextProvider)
                        setHumanVerificationSiteKey("")
                        setHumanVerificationRegistrationEnabled(false)
                        setHumanVerificationLoginEnabled(false)
                        setHumanVerificationPasswordRecoveryEnabled(false)
                      } else if (
                        nextProvider === "turnstile" &&
                        initialSettings.human_verification_secret_configured
                      ) {
                        setHumanVerificationProvider(nextProvider)
                        setHumanVerificationRegistrationEnabled(true)
                      }
                      setReasonError("")
                      setValidationError("")
                    }}
                    variant="outline"
                    spacing={0}
                    className="w-full"
                    disabled={disabled}
                    aria-labelledby="human-verification-provider-label"
                  >
                    <ToggleGroupItem value="disabled" className="h-10 flex-1">
                      关闭
                    </ToggleGroupItem>
                    <ToggleGroupItem
                      value="turnstile"
                      className="h-10 flex-1"
                      disabled={
                        disabled ||
                        !initialSettings.human_verification_secret_configured
                      }
                    >
                      Cloudflare Turnstile
                    </ToggleGroupItem>
                  </ToggleGroup>
                </Field>

                {humanVerificationProvider === "turnstile" ? (
                  <>
                    <Field data-disabled={disabled}>
                      <FieldLabel htmlFor="human-verification-site-key">
                        Turnstile site key
                      </FieldLabel>
                      <Input
                        id="human-verification-site-key"
                        value={humanVerificationSiteKey}
                        maxLength={128}
                        onChange={(event) => {
                          setHumanVerificationSiteKey(event.currentTarget.value)
                          setReasonError("")
                          setValidationError("")
                        }}
                        disabled={disabled}
                        autoComplete="off"
                        placeholder="0x4AAAA..."
                      />
                      <FieldDescription>
                        这是可公开的站点密钥；不要在这里填写 secret key。
                      </FieldDescription>
                    </Field>

                    <div className="grid gap-3 sm:grid-cols-3">
                      <HumanVerificationFlowSwitch
                        id="human-verification-registration"
                        title="新用户注册"
                        description="提交注册资料前验证"
                        checked={humanVerificationRegistrationEnabled}
                        onCheckedChange={
                          setHumanVerificationRegistrationEnabled
                        }
                        disabled={disabled}
                      />
                      <HumanVerificationFlowSwitch
                        id="human-verification-login"
                        title="账户登录"
                        description="每次建立新会话时验证"
                        checked={humanVerificationLoginEnabled}
                        onCheckedChange={setHumanVerificationLoginEnabled}
                        disabled={disabled}
                      />
                      <HumanVerificationFlowSwitch
                        id="human-verification-password-recovery"
                        title="找回密码"
                        description="发送恢复邮件前验证"
                        checked={humanVerificationPasswordRecoveryEnabled}
                        onCheckedChange={
                          setHumanVerificationPasswordRecoveryEnabled
                        }
                        disabled={disabled}
                      />
                    </div>
                  </>
                ) : null}
              </FieldGroup>
            </FieldSet>

            <Separator />

            <div className="flex flex-col gap-1">
              <h2 className="text-base font-semibold">成员邀请</h2>
              <p className="text-sm text-muted-foreground">
                控制现有用户能否生成一次性邀请码；邀请码明文只显示一次。
              </p>
            </div>

            <Field orientation="horizontal" data-disabled={disabled}>
              <div className="min-w-0 flex-1">
                <FieldLabel htmlFor="member-invites-enabled">
                  允许成员签发邀请码
                </FieldLabel>
                <FieldDescription>
                  关闭后只阻止新签发，不会撤销仍在有效期内的邀请码。
                </FieldDescription>
              </div>
              <Switch
                id="member-invites-enabled"
                checked={memberInvitesEnabled}
                onCheckedChange={(checked) => {
                  setMemberInvitesEnabled(checked)
                  setReasonError("")
                  setValidationError("")
                }}
                disabled={disabled}
              />
            </Field>

            <div className="grid gap-4 sm:grid-cols-2">
              <Field data-disabled={disabled}>
                <FieldLabel htmlFor="invite-valid-days">
                  邀请码有效天数
                </FieldLabel>
                <Input
                  id="invite-valid-days"
                  type="number"
                  min={1}
                  max={90}
                  value={inviteValidDays}
                  onChange={(event) => {
                    setInviteValidDays(event.currentTarget.valueAsNumber || 0)
                    setReasonError("")
                    setValidationError("")
                  }}
                  disabled={disabled}
                />
                <FieldDescription>可设置 1–90 天。</FieldDescription>
              </Field>
              <Field data-disabled={disabled}>
                <FieldLabel htmlFor="max-invites-per-member">
                  每位成员邀请名额
                </FieldLabel>
                <Input
                  id="max-invites-per-member"
                  type="number"
                  min={0}
                  max={100}
                  value={maxInvitesPerMember}
                  onChange={(event) => {
                    setMaxInvitesPerMember(
                      event.currentTarget.valueAsNumber || 0
                    )
                    setReasonError("")
                    setValidationError("")
                  }}
                  disabled={disabled}
                />
                <FieldDescription>
                  已成功邀请和当前有效邀请码都会占用名额；0 表示不发放名额。
                </FieldDescription>
              </Field>
              <Field data-disabled={disabled}>
                <FieldLabel htmlFor="minimum-invite-account-age-days">
                  最短注册天数
                </FieldLabel>
                <Input
                  id="minimum-invite-account-age-days"
                  type="number"
                  min={0}
                  max={3650}
                  value={minimumInviteAccountAgeDays}
                  onChange={(event) => {
                    setMinimumInviteAccountAgeDays(
                      event.currentTarget.valueAsNumber || 0
                    )
                    setReasonError("")
                    setValidationError("")
                  }}
                  disabled={disabled}
                />
                <FieldDescription>可设置 0–3650 天。</FieldDescription>
              </Field>
              <Field data-disabled={disabled}>
                <FieldLabel htmlFor="minimum-invite-level">
                  最低用户等级
                </FieldLabel>
                <Input
                  id="minimum-invite-level"
                  type="number"
                  min={1}
                  max={99}
                  value={minimumInviteLevel}
                  onChange={(event) => {
                    setMinimumInviteLevel(
                      event.currentTarget.valueAsNumber || 0
                    )
                    setReasonError("")
                    setValidationError("")
                  }}
                  disabled={disabled}
                />
                <FieldDescription>
                  邮箱还必须已经验证，账户状态必须正常。
                </FieldDescription>
              </Field>
            </div>

            {changed ? (
              <Field data-invalid={Boolean(reasonError)}>
                <FieldLabel htmlFor="registration-policy-reason">
                  变更理由
                </FieldLabel>
                <Textarea
                  id="registration-policy-reason"
                  value={reason}
                  onChange={(event) => {
                    setReason(event.target.value)
                    setReasonError("")
                  }}
                  rows={3}
                  maxLength={500}
                  disabled={disabled}
                  aria-invalid={Boolean(reasonError)}
                  placeholder="可留空；系统会自动记录变更理由"
                />
                <FieldDescription>
                  理由用于操作复核；审计事件只保存不可逆摘要。
                </FieldDescription>
                <FieldError
                  errors={reasonError ? [{ message: reasonError }] : []}
                />
              </Field>
            ) : null}

            <Alert>
              <UserRoundPlusIcon />
              <AlertTitle>不回改现有账户和历史数据</AlertTitle>
              <AlertDescription>
                账户准入只影响后续新注册；登录期限只影响新登录。现有用户、种子和历史数据均不会改变。
              </AlertDescription>
            </Alert>

            <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
              <span>当前第 {baseline.version} 版</span>
              <span>最近更新于 {formatDateTime(baseline.updated_at)}</span>
            </div>
          </FieldGroup>
        </form>
      </RegistrationSettingsCard>

      <AlertDialog
        open={confirmationOpen}
        onOpenChange={(open) => {
          if (!open && mutation.isPending) {
            return
          }
          setConfirmationOpen(open)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <ClipboardCheckIcon />
            </AlertDialogMedia>
            <AlertDialogTitle>确认注册设置变更</AlertDialogTitle>
            <AlertDialogDescription>
              将基于第 {baseline.version} 版创建第 {baseline.version + 1} 版
              ，成功后新的注册请求和邀请码签发立即按新策略处理。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-2 rounded-lg border bg-muted/30 p-4 text-sm sm:grid-cols-[7rem_1fr]">
            <span className="font-medium">注册模式</span>
            <span>
              {registrationModeLabel(baseline.mode)} →{" "}
              {registrationModeLabel(mode)}
            </span>
            <span className="font-medium">用户名规则</span>
            <span>
              {usernameMinCharacters}–{usernameMaxCharacters} 个字符 · 保留{" "}
              {policyEntries(reservedUsernames).length} 个
            </span>
            <span className="font-medium">邮箱准入</span>
            <span>
              {emailDomainModeLabel(emailDomainMode)} · 域名{" "}
              {policyEntries(emailDomains).length} 个
            </span>
            <span className="font-medium">登录会话</span>
            <span>
              普通 {sessionValidHours} 小时 · 记住登录{" "}
              {rememberSessionValidHours} 小时
            </span>
            <span className="font-medium">人机验证</span>
            <span>
              {humanVerificationProviderLabel(humanVerificationProvider)}
              {humanVerificationProvider === "turnstile"
                ? ` · ${[
                    humanVerificationRegistrationEnabled ? "注册" : "",
                    humanVerificationLoginEnabled ? "登录" : "",
                    humanVerificationPasswordRecoveryEnabled ? "找回密码" : "",
                  ]
                    .filter(Boolean)
                    .join("、")}`
                : ""}
            </span>
            <span className="font-medium">成员签发</span>
            <span>
              {memberInvitesEnabled ? "开启" : "关闭"} · 有效 {inviteValidDays}
              天 · 名额 {maxInvitesPerMember}
            </span>
            <span className="font-medium">签发门槛</span>
            <span>
              注册满 {minimumInviteAccountAgeDays} 天且达到 Lv.
              {minimumInviteLevel}
            </span>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={mutation.isPending}>
              返回修改
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={mutation.isPending}
              onClick={() => void handleConfirm()}
            >
              {mutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <SaveIcon data-icon="inline-start" />
              )}
              {mutation.isPending ? "正在提交" : "确认并立即生效"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={blocker.state === "blocked"}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <TriangleAlertIcon />
            </AlertDialogMedia>
            <AlertDialogTitle>有未保存的注册设置</AlertDialogTitle>
            <AlertDialogDescription>
              离开后当前选择和变更理由会丢失，已生效的策略不会改变。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => blocker.reset?.()}>
              继续编辑
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => blocker.proceed?.()}
            >
              放弃并离开
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function HumanVerificationFlowSwitch({
  id,
  title,
  description,
  checked,
  onCheckedChange,
  disabled,
}: {
  id: string
  title: string
  description: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled: boolean
}) {
  return (
    <Field
      orientation="horizontal"
      className="rounded-lg border p-3"
      data-disabled={disabled}
    >
      <div className="min-w-0 flex-1">
        <FieldLabel htmlFor={id}>{title}</FieldLabel>
        <FieldDescription>{description}</FieldDescription>
      </div>
      <Switch
        id={id}
        checked={checked}
        onCheckedChange={onCheckedChange}
        disabled={disabled}
      />
    </Field>
  )
}

function RegistrationSettingsCard({
  saveAction,
  children,
}: {
  saveAction?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <Card className="gap-0 py-0 [--card-spacing:--spacing(6)]">
      <CardHeader className="min-h-[88px] content-center items-center py-6">
        <div className="flex min-w-0 flex-col gap-1">
          <CardTitle className="flex items-center gap-2 text-xl leading-6 font-semibold">
            <UserRoundPlusIcon />
            <h1>注册设置</h1>
          </CardTitle>
          <CardDescription>
            统一管理新用户注册、账户准入、登录期限和成员邀请。
          </CardDescription>
        </div>
        {saveAction ? <CardAction>{saveAction}</CardAction> : null}
      </CardHeader>
      <CardContent className="pb-6">{children}</CardContent>
    </Card>
  )
}

function mutationErrorTitle(error: Error) {
  if (
    error instanceof ApiProblemError &&
    error.code === "registration_policy_settings_version_conflict"
  ) {
    return "注册设置已被其他管理员更新"
  }
  if (error instanceof ApiProblemError && error.status === 403) {
    return "当前后台权限不能保存注册设置"
  }
  return "注册设置保存失败"
}

function mutationErrorDescription(error: Error) {
  if (
    error instanceof ApiProblemError &&
    error.code === "registration_policy_settings_version_conflict"
  ) {
    return "页面正在获取最新版本，请重新核对注册模式后再提交。"
  }
  if (error instanceof ApiProblemError && error.requestId) {
    return `服务器拒绝了本次变更。反馈时请附上请求编号：${error.requestId}`
  }
  return "注册策略没有改变，请核对后台登录状态后重试。"
}

function policyEntries(value: string) {
  return Array.from(
    new Set(
      value
        .split(/[\s,，]+/u)
        .map((entry) => entry.trim().toLowerCase())
        .filter(Boolean)
    )
  ).sort()
}
