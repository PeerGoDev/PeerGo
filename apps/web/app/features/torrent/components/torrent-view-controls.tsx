import * as React from "react"
import {
  EyeIcon,
  EyeOffIcon,
  Grid2X2Icon,
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
  AlertDialogTitle,
} from "~/components/ui/alert-dialog"
import { Button } from "~/components/ui/button"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import type { TorrentView } from "~/features/torrent/model/torrent-view"

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

  return (
    <div className="flex shrink-0 items-center gap-2">
      <Button
        type="button"
        variant="outline"
        size="sm"
        aria-pressed={adultCoversVisible}
        title={
          adultCoversVisible ? "隐藏成人内容封面" : "显示成人内容封面（需确认）"
        }
        className="h-[26px] w-[57px] gap-1 rounded-[6px] px-2 py-1 text-xs aria-pressed:border-primary/40 aria-pressed:bg-primary/10 aria-pressed:text-primary max-sm:h-6 max-sm:w-8 max-sm:px-1.5"
        onClick={() => {
          if (adultCoversVisible) {
            onAdultCoversVisibleChange(false)
          } else {
            setConfirmationOpen(true)
          }
        }}
      >
        {adultCoversVisible ? (
          <EyeIcon data-icon="inline-start" />
        ) : (
          <EyeOffIcon data-icon="inline-start" />
        )}
        <span className="max-sm:sr-only">18+</span>
      </Button>

      <ToggleGroup
        value={[value]}
        onValueChange={(values) => {
          const nextView = values[0]
          if (nextView === "list" || nextView === "poster") {
            onValueChange(nextView)
          }
        }}
        variant="outline"
        size="sm"
        spacing={0}
        aria-label="种子显示方式"
        className="rounded-md border"
      >
        <ToggleGroupItem
          value="list"
          aria-label="列表视图"
          className="h-6 w-[58px] rounded-none border-0 px-2 py-1 text-sm aria-pressed:bg-primary aria-pressed:text-primary-foreground max-sm:w-[30px] max-sm:px-2"
        >
          <ListIcon data-icon="inline-start" />
          <span className="hidden sm:inline">列表</span>
        </ToggleGroupItem>
        <ToggleGroupItem
          value="poster"
          aria-label="海报视图"
          className="h-6 w-[59px] rounded-none border-0 border-l! px-2 py-1 text-sm aria-pressed:bg-primary aria-pressed:text-primary-foreground max-sm:w-[31px] max-sm:px-2"
        >
          <Grid2X2Icon data-icon="inline-start" />
          <span className="hidden sm:inline">海报</span>
        </ToggleGroupItem>
      </ToggleGroup>

      <AlertDialog open={confirmationOpen} onOpenChange={setConfirmationOpen}>
        <AlertDialogContent className="gap-4 p-6 sm:max-w-[425px]!">
          <AlertDialogHeader className="place-items-start text-left">
            <AlertDialogTitle className="flex items-center gap-2 text-lg leading-none font-semibold">
              <TriangleAlertIcon className="size-5 text-warning" />
              显示成人内容？
            </AlertDialogTitle>
            <AlertDialogDescription className="pt-2 text-left">
              种子可能包含 18+ 不适宜内容。开启后封面将正常显示，请确认您已年满
              18 周岁并自愿查看此类内容。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter className="m-0! gap-2 border-0! bg-transparent! p-0! sm:gap-2">
            <AlertDialogCancel className="w-[62px]">取消</AlertDialogCancel>
            <AlertDialogAction
              className="w-[209px]"
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
