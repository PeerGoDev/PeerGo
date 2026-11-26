import * as React from "react"
import {
  Grid2X2Icon,
  LayoutDashboardIcon,
  ListIcon,
  TriangleAlertIcon,
} from "lucide-react"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog"
import { Switch } from "~/components/ui/switch"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  parseTorrentView,
  type TorrentView,
} from "~/features/torrent/model/torrent-view"

const viewOptions: Array<{
  value: TorrentView
  label: string
  icon: typeof ListIcon
}> = [
  { value: "list", label: "列表", icon: ListIcon },
  { value: "poster", label: "海报", icon: Grid2X2Icon },
  { value: "waterfall", label: "瀑布流", icon: LayoutDashboardIcon },
]

export function TorrentViewControls({
  value,
  onValueChange,
  hideNsfw,
  onHideNsfwChange,
}: {
  value: TorrentView
  onValueChange: (value: TorrentView) => void
  hideNsfw: boolean
  onHideNsfwChange: (hide: boolean) => void
}) {
  return (
    <div className="flex shrink-0 items-center gap-3">
      <ToggleGroup
        value={[value]}
        onValueChange={(values) => {
          const nextView = parseTorrentView(values[0])
          if (nextView) onValueChange(nextView)
        }}
        variant="segment"
        size="sm"
        spacing={0.5}
        aria-label="种子显示方式"
      >
        {viewOptions.map((option) => (
          <ToggleGroupItem
            key={option.value}
            value={option.value}
            aria-label={`${option.label}视图`}
            title={`${option.label}视图`}
            className="h-7 px-2.5 sm:h-6 sm:px-3"
          >
            <option.icon className="sm:hidden" />
            <span className="max-sm:sr-only">{option.label}</span>
          </ToggleGroupItem>
        ))}
      </ToggleGroup>

      <TorrentNsfwControl
        hideNsfw={hideNsfw}
        onHideNsfwChange={onHideNsfwChange}
      />
    </div>
  )
}

export function TorrentNsfwControl({
  hideNsfw,
  onHideNsfwChange,
}: {
  hideNsfw: boolean
  onHideNsfwChange: (hide: boolean) => void
}) {
  const [confirmationOpen, setConfirmationOpen] = React.useState(false)
  const switchId = React.useId()

  return (
    <>
      <span className="flex items-center gap-1.5 text-xs text-faint">
        <label
          htmlFor={switchId}
          className="cursor-pointer whitespace-nowrap select-none"
        >
          屏蔽 NSFW
        </label>
        <Switch
          id={switchId}
          checked={hideNsfw}
          aria-label="屏蔽 NSFW"
          onCheckedChange={(checked) => {
            if (checked) {
              onHideNsfwChange(true)
            } else {
              setConfirmationOpen(true)
            }
          }}
        />
      </span>

      <AlertDialog open={confirmationOpen} onOpenChange={setConfirmationOpen}>
        <AlertDialogContent>
          <AlertDialogHeader className="place-items-start text-left">
            <AlertDialogMedia className="text-warning-foreground">
              <TriangleAlertIcon />
            </AlertDialogMedia>
            <AlertDialogTitle>显示 NSFW 内容？</AlertDialogTitle>
            <AlertDialogDescription className="text-left">
              关闭屏蔽后，成人分类的种子和封面会正常显示，可能包含 18+
              不适宜内容。请确认您已年满 18 周岁并自愿查看此类内容。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                onHideNsfwChange(false)
                setConfirmationOpen(false)
              }}
            >
              我已年满 18 周岁，确认显示
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
