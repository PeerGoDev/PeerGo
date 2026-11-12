type ClassifiedCategory = {
  id: string
  nsfw: boolean
}

const noCategories: ReadonlySet<string> = new Set()

export function nsfwCategoryIds(
  categories: readonly ClassifiedCategory[] | undefined
): ReadonlySet<string> {
  if (!categories?.length) return noCategories
  return new Set(
    categories
      .filter((category) => category.nsfw)
      .map((category) => category.id)
  )
}
