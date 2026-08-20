import { z } from "zod"

export const newPasswordSchema = z
  .string()
  .min(12, "密码至少需要 12 位")
  .max(1024, "密码过长")
