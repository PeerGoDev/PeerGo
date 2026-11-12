import { MoonIcon, SunIcon } from "lucide-react"

import { Button } from "~/components/ui/button"
import { useTheme } from "~/features/shell/model/use-theme"
import { cn } from "~/lib/utils"

export function ThemeToggleButton({ className }: { className?: string }) {
  const { theme, toggleTheme } = useTheme()

  return (
    <Button
      variant="ghost"
      size="icon-sm"
      onClick={toggleTheme}
      aria-label={theme === "dark" ? "切换到浅色模式" : "切换到深色模式"}
      className={cn("text-muted-foreground hover:text-foreground", className)}
    >
      {theme === "light" ? <SunIcon /> : <MoonIcon />}
    </Button>
  )
}
