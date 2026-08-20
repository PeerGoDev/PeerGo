import { z } from "zod"

export const maximumTorrentFileBytes = 16 * 1024 * 1024

const stableCategoryIdPattern = /^[a-z0-9][a-z0-9-]{0,63}$/

const torrentFileSchema = z
  .custom<File>(
    (value) => typeof File !== "undefined" && value instanceof File,
    "请选择一个 .torrent 文件"
  )
  .refine((file) => file.size > 0, "种子文件不能为空")
  .refine(
    (file) => file.size <= maximumTorrentFileBytes,
    "种子文件不能超过 16 MiB"
  )
  .refine(
    (file) => file.name.toLocaleLowerCase("en-US").endsWith(".torrent"),
    "请选择扩展名为 .torrent 的文件"
  )

export const torrentEditableMetadataSchema = z.object({
  categoryId: z
    .string()
    .trim()
    .regex(stableCategoryIdPattern, "请选择一个有效分类"),
  title: z
    .string()
    .trim()
    .refine((value) => runeLength(value) >= 1, "请输入标题")
    .refine((value) => runeLength(value) <= 240, "标题不能超过 240 个字符"),
  subtitle: z
    .string()
    .trim()
    .refine((value) => runeLength(value) <= 300, "副标题不能超过 300 个字符"),
})

const externalIdentifierSchema = (provider: "imdb" | "tmdb" | "douban") =>
  z
    .string()
    .trim()
    .max(300, "外部资料链接不能超过 300 个字符")
    .refine(
      (value) => !value || Boolean(parseExternalIdentifier(provider, value)),
      `请输入有效的${externalIdentifierLabel(provider)}链接或编号`
    )
    .transform((value) =>
      value ? (parseExternalIdentifier(provider, value) ?? "") : ""
    )

export const torrentUploadFormSchema = torrentEditableMetadataSchema.extend({
  torrentFile: torrentFileSchema,
  description: z
    .string()
    .trim()
    .min(1, "请输入种子描述")
    .max(4 * 1024 * 1024, "种子描述过长"),
  mediaInfo: z
    .string()
    .trim()
    .min(1, "请输入 MediaInfo 或 BDInfo")
    .max(16 * 1024 * 1024, "MediaInfo/BDInfo 过长"),
  anonymous: z.boolean(),
  imdbId: externalIdentifierSchema("imdb"),
  tmdbId: externalIdentifierSchema("tmdb"),
  doubanId: externalIdentifierSchema("douban"),
})

export type TorrentUploadFormValues = z.output<typeof torrentUploadFormSchema>
export type TorrentUploadFormField = keyof z.input<
  typeof torrentUploadFormSchema
>
export type TorrentUploadFormErrors = Partial<
  Record<TorrentUploadFormField, string>
>

export function torrentUploadFieldErrors(
  error: z.ZodError
): TorrentUploadFormErrors {
  return zodFieldErrors<TorrentUploadFormField>(error)
}

// Keep Zod issue-to-field mapping shared by the initial upload and the bounded
// resubmission form so both surfaces render validation consistently.
export function zodFieldErrors<TField extends string>(
  error: z.ZodError
): Partial<Record<TField, string>> {
  const errors: Partial<Record<TField, string>> = {}
  for (const issue of error.issues) {
    const field = issue.path[0]
    if (typeof field === "string" && !errors[field as TField]) {
      errors[field as TField] = issue.message
    }
  }
  return errors
}

export function runeLength(value: string) {
  return Array.from(value).length
}

export function parseExternalIdentifier(
  provider: "imdb" | "tmdb" | "douban",
  raw: string
) {
  const value = raw.trim()
  if (!value) return ""

  if (provider === "imdb") {
    const direct = /^tt[0-9]{7,10}$/i.exec(value)?.[0]
    if (direct) return direct.toLowerCase()
    return /(?:^|\/title\/)(tt[0-9]{7,10})(?:\/|$|[?#])/i
      .exec(value)?.[1]
      ?.toLowerCase()
  }

  const direct = /^[0-9]{1,20}$/.exec(value)?.[0]
  if (direct) return direct
  if (provider === "tmdb") {
    return /(?:themoviedb\.org\/(?:movie|tv)\/)([0-9]{1,20})(?:[-/?#]|$)/i.exec(
      value
    )?.[1]
  }
  return /(?:douban\.com\/subject\/)([0-9]{1,20})(?:\/|$|[?#])/i.exec(
    value
  )?.[1]
}

function externalIdentifierLabel(provider: "imdb" | "tmdb" | "douban") {
  if (provider === "imdb") return " IMDb "
  if (provider === "tmdb") return " TMDB "
  return "豆瓣"
}
