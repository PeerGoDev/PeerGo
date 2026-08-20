import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

import { Button } from "~/components/ui/button"
import { useSidebar } from "~/components/ui/sidebar"

export function SidebarCollapseControl() {
  const { isMobile, state, toggleSidebar } = useSidebar()
  const collapsed = state === "collapsed"
  const label = isMobile ? "收起导航" : collapsed ? "展开侧栏" : "收起侧栏"

  return (
    <Button
      variant="ghost"
      onClick={toggleSidebar}
      className="h-11 w-full justify-end rounded-none px-5 text-muted-foreground group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0"
      aria-label={label}
    >
      <span className="group-data-[collapsible=icon]:hidden">收起</span>
      {collapsed && !isMobile ? <ChevronRightIcon /> : <ChevronLeftIcon />}
    </Button>
  )
}
