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
import { describe, expect, it } from "vitest"

import { torrentCategoryIcon } from "~/features/torrent/model/category-icon"

describe("torrentCategoryIcon", () => {
  it.each([
    ["movie", "电影", FilmIcon],
    ["tv", "电视剧", TvIcon],
    ["documentary", "纪录片", CameraIcon],
    ["animation", "动漫", SparklesIcon],
    ["music", "音乐", MusicIcon],
    ["variety", "综艺", MicIcon],
    ["sports", "体育", ZapIcon],
    ["ebook", "电子书", BookOpenIcon],
    ["software", "软件", AppWindowIcon],
    ["game", "游戏", Gamepad2Icon],
    ["other", "其它", FolderIcon],
  ])("maps %s to the expected semantic icon", (id, name, icon) => {
    expect(torrentCategoryIcon(id, name)).toBe(icon)
  })
})
