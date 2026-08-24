import { z } from "zod"

import type { ManagedCategory } from "~/features/staff/api/category-administration.queries"

const stableCategoryIdPattern = /^[a-z0-9][a-z0-9-]{0,63}$/

const displayOrderSchema = z
  .string()
  .trim()
  .regex(/^\d+$/, "请输入 0–1000000 的整数")
  .transform(Number)
  .pipe(z.number().int().min(0).max(1_000_000))

export const categoryFormSchema = z.object({
  id: z
    .string()
    .trim()
    .regex(
      stableCategoryIdPattern,
      "仅可使用小写字母、数字和连字符，最长 64 位"
    ),
  name: z
    .string()
    .trim()
    .refine((value) => runeLength(value) >= 1, "请输入分类名称")
    .refine((value) => runeLength(value) <= 40, "分类名称不能超过 40 个字符"),
  displayOrder: displayOrderSchema,
  enabled: z.boolean(),
  reason: z
    .string()
    .trim()
    .refine((value) => runeLength(value) <= 500, "变更理由不能超过 500 个字符"),
})

export type CategoryFormValues = z.output<typeof categoryFormSchema>
export type CategoryFormField = keyof z.input<typeof categoryFormSchema>

export function hasCategoryBusinessChanges(
  category: ManagedCategory,
  values: CategoryFormValues
) {
  return (
    category.name !== values.name ||
    category.display_order !== values.displayOrder ||
    category.enabled !== values.enabled
  )
}

function runeLength(value: string) {
  return Array.from(value).length
}
