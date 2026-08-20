import type { LucideIcon } from "lucide-react"
import {
  AppWindowIcon,
  BookOpenIcon,
  CameraIcon,
  FilmIcon,
  FolderIcon,
  Gamepad2Icon,
  MicIcon,
  MusicIcon,
  SparklesIcon,
  TvIcon,
  ZapIcon,
} from "lucide-react"

/**
 * Maps stable category identities to the familiar PtYes/Rousi visual language.
 * The category id remains authoritative; the display name is only a fallback
 * for migrated or locally configured identities that predate the canonical ids.
 */
export function torrentCategoryIcon(
  categoryId: string,
  categoryName: string
): LucideIcon {
  const identity = `${categoryId} ${categoryName}`.toLowerCase()
  if (identity.includes("movie") || identity.includes("电影")) return FilmIcon
  if (identity.includes("tv") || identity.includes("剧")) return TvIcon
  if (identity.includes("documentary") || identity.includes("纪录"))
    return CameraIcon
  if (
    identity.includes("anime") ||
    identity.includes("animation") ||
    identity.includes("动漫")
  )
    return SparklesIcon
  if (identity.includes("music") || identity.includes("音乐")) return MusicIcon
  if (identity.includes("variety") || identity.includes("综艺")) return MicIcon
  if (identity.includes("sports") || identity.includes("体育")) return ZapIcon
  if (identity.includes("book") || identity.includes("书")) return BookOpenIcon
  if (identity.includes("software") || identity.includes("软件"))
    return AppWindowIcon
  if (identity.includes("game") || identity.includes("游戏"))
    return Gamepad2Icon
  return FolderIcon
}
