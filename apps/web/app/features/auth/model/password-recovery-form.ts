import { z } from "zod"

import { newPasswordSchema } from "~/features/auth/model/new-password"

export const passwordRecoveryFormSchema = z
  .object({
    password: newPasswordSchema,
    confirmPassword: z.string(),
  })
  .superRefine((value, context) => {
    if (value.password !== value.confirmPassword) {
      context.addIssue({
        code: "custom",
        path: ["confirmPassword"],
        message: "两次输入的密码不一致",
      })
    }
  })

export type PasswordRecoveryFormField = "password" | "confirmPassword"
export type PasswordRecoveryFormErrors = Partial<
  Record<PasswordRecoveryFormField, string>
>

export function passwordRecoveryFieldErrors(
  error: z.ZodError<z.infer<typeof passwordRecoveryFormSchema>>
): PasswordRecoveryFormErrors {
  const fields = error.flatten().fieldErrors
  return {
    password: fields.password?.[0],
    confirmPassword: fields.confirmPassword?.[0],
  }
}
