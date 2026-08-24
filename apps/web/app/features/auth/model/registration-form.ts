import { z } from "zod"

import type { components } from "~/generated/api"
import { emailAddressSchema } from "~/features/auth/model/email-address"
import { newPasswordSchema } from "~/features/auth/model/new-password"
import { invitationTokenPattern } from "~/features/auth/model/invitation-token"

export type RegistrationMode = components["schemas"]["RegistrationMode"]

const usernamePattern = /^[a-z0-9][a-z0-9_-]{2,31}$/

export type RegistrationFormPolicy = {
  mode: RegistrationMode
  usernameMinCharacters: number
  usernameMaxCharacters: number
}

export function registrationFormSchema(
  policy: RegistrationMode | RegistrationFormPolicy
) {
  const normalizedPolicy =
    typeof policy === "string"
      ? {
          mode: policy,
          usernameMinCharacters: 3,
          usernameMaxCharacters: 32,
        }
      : policy
  return z
    .object({
      username: z
        .string()
        .trim()
        .min(
          normalizedPolicy.usernameMinCharacters,
          `用户名至少需要 ${normalizedPolicy.usernameMinCharacters} 个字符`
        )
        .max(
          normalizedPolicy.usernameMaxCharacters,
          `用户名最多 ${normalizedPolicy.usernameMaxCharacters} 个字符`
        )
        .regex(usernamePattern, "仅可使用小写字母、数字、下划线和短横线"),
      displayName: z
        .string()
        .trim()
        .min(1, "请输入显示名称")
        .max(40, "显示名称最多 40 个字符"),
      email: emailAddressSchema,
      password: newPasswordSchema,
      confirmPassword: z.string(),
      invitationToken: z.string().trim(),
    })
    .superRefine((value, context) => {
      if (value.password !== value.confirmPassword) {
        context.addIssue({
          code: "custom",
          path: ["confirmPassword"],
          message: "两次输入的密码不一致",
        })
      }
      if (
        (normalizedPolicy.mode === "invite" ||
          value.invitationToken.length > 0) &&
        !invitationTokenPattern.test(value.invitationToken)
      ) {
        context.addIssue({
          code: "custom",
          path: ["invitationToken"],
          message: "请输入有效的邀请码",
        })
      }
    })
}

export type RegistrationFormField =
  | "username"
  | "displayName"
  | "email"
  | "password"
  | "confirmPassword"
  | "invitationToken"

export type RegistrationFormValues = z.infer<
  ReturnType<typeof registrationFormSchema>
>

export type RegistrationFormErrors = Partial<
  Record<RegistrationFormField, string>
>

export function registrationFieldErrors(
  error: z.ZodError<RegistrationFormValues>
): RegistrationFormErrors {
  const fields = error.flatten().fieldErrors
  return {
    username: fields.username?.[0],
    displayName: fields.displayName?.[0],
    email: fields.email?.[0],
    password: fields.password?.[0],
    confirmPassword: fields.confirmPassword?.[0],
    invitationToken: fields.invitationToken?.[0],
  }
}
