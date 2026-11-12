import * as React from "react"

const storageKey = "peergo-theme"

type Theme = "light" | "dark"

// Matches --background so mobile browser chrome blends into the app header.
const browserChromeColors: Record<Theme, string> = {
  light: "#f6f7f8",
  dark: "#0f1115",
}

function preferredTheme(): Theme {
  const savedTheme = window.localStorage.getItem(storageKey)
  if (savedTheme === "dark" || savedTheme === "light") {
    return savedTheme
  }
  return typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light"
}

export function useTheme() {
  // PtYes resolves the persisted/system theme before the first client render.
  // Keeping that behavior avoids a light-frame flash before the shell mounts.
  const [theme, setTheme] = React.useState<Theme>(preferredTheme)

  React.useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark")
    document.documentElement.style.colorScheme = theme
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute("content", browserChromeColors[theme])
    window.localStorage.setItem(storageKey, theme)
  }, [theme])

  return {
    theme,
    toggleTheme: () =>
      setTheme((value) => (value === "dark" ? "light" : "dark")),
  }
}
