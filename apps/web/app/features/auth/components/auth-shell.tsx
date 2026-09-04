import type { ReactNode } from "react"
import { MoonIcon, SunIcon } from "lucide-react"

import { Button } from "~/components/ui/button"
import { useTheme } from "~/features/shell/model/use-theme"

/**
 * Chrome-less shell for the public auth routes (login, register, password
 * recovery and email confirmation). Pages center themselves against the full
 * viewport; the only chrome is a theme toggle in the top-right corner.
 */
export function AuthShell({ children }: { children: ReactNode }) {
  const { theme, toggleTheme } = useTheme()

  return (
    <div className="relative min-h-svh bg-background">
      <Button
        variant="secondary"
        size="icon"
        onClick={toggleTheme}
        aria-label={theme === "dark" ? "切换到浅色模式" : "切换到深色模式"}
        className="absolute top-4 right-4 z-10 sm:top-6 sm:right-6"
      >
        {theme === "light" ? <SunIcon /> : <MoonIcon />}
      </Button>
      {children}
    </div>
  )
}
