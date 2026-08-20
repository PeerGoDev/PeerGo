type TorrentCategory = {
  id: string
  name: string
}

const adultCategoryKeys = new Set(["9kg"])

/**
 * Keeps the migrated Rousi category key as the compatibility boundary. Cover
 * privacy is deliberately derived here instead of adding a second adult flag
 * that could drift away from the catalog category stored by Core.
 */
export function torrentCoverRequiresAdultConfirmation(
  category: TorrentCategory
) {
  return (
    adultCategoryKeys.has(category.id.trim().toLowerCase()) ||
    adultCategoryKeys.has(category.name.trim().toLowerCase())
  )
}
