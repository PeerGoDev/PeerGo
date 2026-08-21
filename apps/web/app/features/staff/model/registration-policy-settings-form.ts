import { z } from "zod"

const usernamePattern = /^[a-z0-9][a-z0-9_-]{2,31}$/
const emailDomainPattern =
  /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$/

export const registrationPolicyReasonLimits = {
  minimum: 5,
  maximum: 500,
} as const

function policyEntryList(
  itemSchema: z.ZodString,
  maximum: number,
  maximumMessage: string
) {
  return z
    .string()
    .transform((value) =>
      Array.from(
        new Set(
          value
            .split(/[\s,，]+/u)
            .map((entry) => entry.trim().toLowerCase())
            .filter(Boolean)
        )
      ).sort()
    )
    .pipe(z.array(itemSchema).max(maximum, maximumMessage))
}

export const registrationPolicySettingsFormSchema = z
  .object({
    mode: z.enum(["open", "invite", "closed"]),
    memberInvitesEnabled: z.boolean(),
    inviteValidDays: z.number().int().min(1).max(90),
    maxInvitesPerMember: z.number().int().min(0).max(100),
    minimumInviteAccountAgeDays: z.number().int().min(0).max(3650),
    minimumInviteLevel: z.number().int().min(1).max(99),
    usernameMinCharacters: z.number().int().min(3).max(32),
    usernameMaxCharacters: z.number().int().min(3).max(32),
    reservedUsernames: policyEntryList(
      z.string().regex(usernamePattern, "保留用户名格式无效"),
      200,
      "保留用户名不能超过 200 个"
    ),
    emailDomainMode: z.enum(["any", "allowlist", "blocklist"]),
    emailDomains: policyEntryList(
      z.string().max(253).regex(emailDomainPattern, "邮箱域名格式无效"),
      100,
      "邮箱域名不能超过 100 个"
    ),
    sessionValidHours: z.number().int().min(1).max(720),
    rememberSessionValidHours: z.number().int().min(24).max(2160),
    humanVerificationProvider: z.enum(["disabled", "turnstile"]),
    humanVerificationSiteKey: z.string().trim().max(128),
    humanVerificationRegistrationEnabled: z.boolean(),
    humanVerificationLoginEnabled: z.boolean(),
    humanVerificationPasswordRecoveryEnabled: z.boolean(),
    humanVerificationSecretConfigured: z.boolean(),
    reason: z
      .string()
      .trim()
      .refine(
        (value) =>
          Array.from(value).length >= registrationPolicyReasonLimits.minimum,
        `请填写至少 ${registrationPolicyReasonLimits.minimum} 个字符的变更理由`
      )
      .refine(
        (value) =>
          Array.from(value).length <= registrationPolicyReasonLimits.maximum,
        `变更理由不能超过 ${registrationPolicyReasonLimits.maximum} 个字符`
      ),
  })
  .superRefine((value, context) => {
    if (value.usernameMinCharacters > value.usernameMaxCharacters) {
      context.addIssue({
        code: "custom",
        path: ["usernameMaxCharacters"],
        message: "用户名最大长度不能小于最小长度",
      })
    }
    if (value.emailDomainMode !== "any" && value.emailDomains.length === 0) {
      context.addIssue({
        code: "custom",
        path: ["emailDomains"],
        message: "启用邮箱白名单或黑名单时必须填写至少一个域名",
      })
    }
    if (value.sessionValidHours > value.rememberSessionValidHours) {
      context.addIssue({
        code: "custom",
        path: ["rememberSessionValidHours"],
        message: "记住登录有效期不能短于普通登录有效期",
      })
    }
    const anyHumanVerificationFlow =
      value.humanVerificationRegistrationEnabled ||
      value.humanVerificationLoginEnabled ||
      value.humanVerificationPasswordRecoveryEnabled
    if (
      value.humanVerificationProvider === "disabled" &&
      (value.humanVerificationSiteKey !== "" || anyHumanVerificationFlow)
    ) {
      context.addIssue({
        code: "custom",
        path: ["humanVerificationProvider"],
        message: "关闭人机验证时不能保留启用页面或 site key",
      })
    }
    if (
      value.humanVerificationProvider === "turnstile" &&
      !value.humanVerificationSecretConfigured
    ) {
      context.addIssue({
        code: "custom",
        path: ["humanVerificationProvider"],
        message: "请先在 Core 部署密钥中配置 Turnstile secret",
      })
    }
    if (
      value.humanVerificationProvider === "turnstile" &&
      value.humanVerificationSiteKey === ""
    ) {
      context.addIssue({
        code: "custom",
        path: ["humanVerificationSiteKey"],
        message: "启用 Turnstile 时必须填写 site key",
      })
    }
    if (
      value.humanVerificationProvider === "turnstile" &&
      !anyHumanVerificationFlow
    ) {
      context.addIssue({
        code: "custom",
        path: ["humanVerificationProvider"],
        message: "请至少选择一个需要人机验证的页面",
      })
    }
  })

export type RegistrationPolicySettingsFormValues = z.output<
  typeof registrationPolicySettingsFormSchema
>

export function registrationModeLabel(mode: "open" | "invite" | "closed") {
  switch (mode) {
    case "open":
      return "开放注册"
    case "invite":
      return "邀请注册"
    case "closed":
      return "关闭注册"
  }
}

export function humanVerificationProviderLabel(
  provider: "disabled" | "turnstile"
) {
  return provider === "turnstile" ? "Cloudflare Turnstile" : "关闭"
}

export function emailDomainModeLabel(mode: "any" | "allowlist" | "blocklist") {
  switch (mode) {
    case "any":
      return "不限制"
    case "allowlist":
      return "仅白名单"
    case "blocklist":
      return "排除黑名单"
  }
}
