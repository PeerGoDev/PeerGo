import { z } from "zod"

export const emailAddressSchema = z
  .string()
  .trim()
  .email("请输入有效的邮箱地址")
  .max(254, "邮箱地址过长")
