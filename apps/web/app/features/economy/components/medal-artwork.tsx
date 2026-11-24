import { useState } from "react"

import medalAtlas from "~/features/economy/assets/community-medal-atlas.webp"
import { cn } from "~/lib/utils"

import "./medal-artwork.css"

const artworkByName = new Map([
  ["创始成就勋章", 0],
  ["2023七夕勋章", 1],
  ["2024龙年勋章", 2],
  ["2024劳动节勋章", 3],
  ["2024年端午勋章", 4],
  ["一周年纪念勋章", 5],
  ["一周年纪念高级勋章", 5],
  ["为爱发光", 6],
  ["2024年圣诞节勋章", 7],
  ["2025年蛇年勋章", 8],
  ["2025年劳动节勋章", 3],
  ["2025年劳动节发种勋章", 9],
  ["2025年端午节勋章", 4],
  ["二周年纪念勋章", 10],
  ["二周年纪念高级勋章", 10],
  ["转种组", 11],
  ["保种组", 12],
  ["官种组", 13],
  ["创始复刻勋章", 0],
  ["小氪怡情", 14],
])

export function MedalArtwork({
  name,
  image,
}: {
  name: string
  image?: string | null
}) {
  const [failedImage, setFailedImage] = useState<string>()
  if (image && failedImage !== image) {
    return (
      <img
        src={image}
        alt={name}
        loading="lazy"
        className="size-full object-contain"
        onError={() => setFailedImage(image)}
      />
    )
  }

  const tile = artworkByName.get(name) ?? 15
  const column = tile % 4
  const row = Math.floor(tile / 4)
  return (
    <span
      role="img"
      aria-label={name}
      className={cn(
        "block size-14 shrink-0 bg-no-repeat",
        name.endsWith("周年纪念高级勋章") &&
          "motion-safe:animate-[medal-glow_3s_ease-in-out_infinite]"
      )}
      style={{
        backgroundImage: `url(${medalAtlas})`,
        backgroundSize: "400% 400%",
        backgroundPosition: `${(column * 100) / 3}% ${(row * 100) / 3}%`,
      }}
    />
  )
}
