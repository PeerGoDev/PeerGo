import { clsx, type ClassValue } from "clsx"
import { extendTailwindMerge } from "tailwind-merge"

// Teach the merger the theme scales declared in app.css so custom tokens such
// as `shadow-card` replace, rather than accumulate with, other shadow utilities.
const twMerge = extendTailwindMerge({
  extend: {
    theme: {
      shadow: ["card", "popover", "dialog", "button"],
      tracking: ["title"],
    },
  },
})

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
