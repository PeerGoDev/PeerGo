import { z } from "zod"

import {
  runeLength,
  torrentEditableMetadataSchema,
  zodFieldErrors,
} from "~/features/torrent/model/torrent-upload-form"

export const torrentResubmissionFormSchema =
  torrentEditableMetadataSchema.extend({
    correctionNote: z
      .string()
      .trim()
      .refine(
        (value) => runeLength(value) <= 1_000,
        "整改说明不能超过 1000 个字符"
      ),
  })

export type TorrentResubmissionFormValues = z.output<
  typeof torrentResubmissionFormSchema
>
export type TorrentResubmissionFormField = keyof z.input<
  typeof torrentResubmissionFormSchema
>
export type TorrentResubmissionFormErrors = Partial<
  Record<TorrentResubmissionFormField, string>
>

export function torrentResubmissionFieldErrors(
  error: z.ZodError
): TorrentResubmissionFormErrors {
  return zodFieldErrors<TorrentResubmissionFormField>(error)
}

export function torrentMetadataChanged(
  before: Pick<
    TorrentResubmissionFormValues,
    "categoryId" | "title" | "subtitle"
  >,
  after: TorrentResubmissionFormValues
) {
  return (
    before.categoryId !== after.categoryId ||
    before.title !== after.title ||
    before.subtitle !== after.subtitle
  )
}
