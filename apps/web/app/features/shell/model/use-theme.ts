import * as React from "react"

const storageKey = "peergo-theme"

type Theme = "light" | "dark"

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
    window.localStorage.setItem(storageKey, theme)
  }, [theme])

  return {
    theme,
    toggleTheme: () =>
      setTheme((value) => (value === "dark" ? "light" : "dark")),
  }
}
