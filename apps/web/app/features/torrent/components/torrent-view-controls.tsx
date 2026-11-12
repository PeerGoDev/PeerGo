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
  adultCoversVisible,
  onAdultCoversVisibleChange,
}: {
  value: TorrentView
  onValueChange: (value: TorrentView) => void
  adultCoversVisible: boolean
  onAdultCoversVisibleChange: (visible: boolean) => void
}) {
  const [confirmationOpen, setConfirmationOpen] = React.useState(false)
  const switchId = React.useId()

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

      <span className="flex items-center gap-1.5 text-xs text-faint">
        <label htmlFor={switchId} className="cursor-pointer select-none">
          <span className="sr-only">显示成人封面（18+）</span>
          <span aria-hidden="true" className="max-sm:hidden">
            成人封面
          </span>
          <span aria-hidden="true" className="sm:hidden">
            18+
          </span>
        </label>
        <Switch
          id={switchId}
          checked={adultCoversVisible}
          aria-label="显示成人封面（18+）"
          onCheckedChange={(checked) => {
            if (!checked) {
              onAdultCoversVisibleChange(false)
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
            <AlertDialogTitle>显示成人内容？</AlertDialogTitle>
            <AlertDialogDescription className="text-left">
              种子可能包含 18+ 不适宜内容。开启后封面将正常显示，请确认您已年满
              18 周岁并自愿查看此类内容。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                onAdultCoversVisibleChange(true)
                setConfirmationOpen(false)
              }}
            >
              我已年满 18 周岁，确认显示
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
