import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

import { Button } from "~/components/ui/button"
import { useSidebar } from "~/components/ui/sidebar"

export function SidebarCollapseControl() {
  const { isMobile, state, toggleSidebar } = useSidebar()
  const collapsed = state === "collapsed" && !isMobile
  const label = isMobile ? "收起导航" : collapsed ? "展开侧栏" : "收起侧栏"

  return (
    <Button
      variant="ghost"
      size="xs"
      onClick={toggleSidebar}
      className="h-7 w-full justify-start gap-2 px-2 font-normal text-faint group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0 hover:text-foreground"
      aria-label={label}
      title={collapsed ? label : undefined}
    >
      {collapsed ? (
        <ChevronRightIcon className="size-3.5" />
      ) : (
        <ChevronLeftIcon className="size-3.5" />
      )}
      <span className="group-data-[collapsible=icon]:hidden">{label}</span>
    </Button>
  )
}
