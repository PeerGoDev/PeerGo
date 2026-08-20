import {
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type PointerEvent as ReactPointerEvent,
} from "react"
import {
  CameraIcon,
  ImageUpIcon,
  MinusIcon,
  PlusIcon,
  UploadIcon,
} from "lucide-react"

import { Alert, AlertDescription } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog"
import { Spinner } from "~/components/ui/spinner"
import { useUpdateMyAvatar } from "~/features/auth/api/avatar.mutations"
import { ApiProblemError } from "~/shared/api/problem"
import { UserAvatar } from "~/shared/components/user-avatar"

const sourceFileLimit = 5 << 20
const viewportSize = 280
const outputSize = 256
const acceptedTypes = new Set(["image/jpeg", "image/png", "image/webp"])

type ImageSize = { width: number; height: number }
type Offset = { x: number; y: number }

export function AvatarUpload({
  username,
  displayName,
  csrfToken,
}: {
  username: string
  displayName: string
  csrfToken: string
}) {
  const inputRef = useRef<HTMLInputElement>(null)
  const imageRef = useRef<HTMLImageElement>(null)
  const dragRef = useRef<{ x: number; y: number; origin: Offset } | null>(null)
  const [open, setOpen] = useState(false)
  const [sourceUrl, setSourceUrl] = useState("")
  const [imageSize, setImageSize] = useState<ImageSize | null>(null)
  const [offset, setOffset] = useState<Offset>({ x: 0, y: 0 })
  const [zoom, setZoom] = useState(1)
  const [error, setError] = useState("")
  const mutation = useUpdateMyAvatar(username)

  useEffect(
    () => () => {
      if (sourceUrl) URL.revokeObjectURL(sourceUrl)
    },
    [sourceUrl]
  )

  const chooseFile = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    event.target.value = ""
    if (!file) return
    if (!acceptedTypes.has(file.type) || file.size > sourceFileLimit) {
      setError("请选择 5MB 以内的 JPG、PNG 或 WebP 图片")
      return
    }
    if (sourceUrl) URL.revokeObjectURL(sourceUrl)
    setSourceUrl(URL.createObjectURL(file))
    setImageSize(null)
    setOffset({ x: 0, y: 0 })
    setZoom(1)
    setError("")
    mutation.reset()
    setOpen(true)
  }

  const displayedSize = imageSize
    ? scaledImageSize(imageSize, zoom)
    : { width: viewportSize, height: viewportSize }

  const updateZoom = (nextZoom: number) => {
    const normalized = Math.min(3, Math.max(1, nextZoom))
    setZoom(normalized)
    if (imageSize) {
      setOffset((current) =>
        clampOffset(current, scaledImageSize(imageSize, normalized))
      )
    }
  }

  const beginDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!imageSize) return
    event.currentTarget.setPointerCapture(event.pointerId)
    dragRef.current = { x: event.clientX, y: event.clientY, origin: offset }
  }

  const drag = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!dragRef.current || !imageSize) return
    setOffset(
      clampOffset(
        {
          x: dragRef.current.origin.x + event.clientX - dragRef.current.x,
          y: dragRef.current.origin.y + event.clientY - dragRef.current.y,
        },
        displayedSize
      )
    )
  }

  const endDrag = () => {
    dragRef.current = null
  }

  const upload = async () => {
    if (!imageRef.current || !imageSize) return
    setError("")
    try {
      const image = await cropToJPEG(
        imageRef.current,
        imageSize,
        displayedSize,
        offset
      )
      await mutation.mutateAsync({ image, csrfToken })
      setOpen(false)
    } catch (uploadError) {
      setError(
        uploadError instanceof ApiProblemError
          ? uploadError.message
          : "头像处理失败，请换一张图片重试"
      )
    }
  }

  return (
    <div className="flex items-center gap-4">
      <button
        type="button"
        className="group relative rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        aria-label="点击头像更换"
        onClick={() => inputRef.current?.click()}
      >
        <UserAvatar
          username={username}
          displayName={displayName}
          className="size-16"
          fallbackClassName="text-xl"
        />
        <span className="absolute inset-0 flex items-center justify-center rounded-full bg-black/45 text-white opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
          <CameraIcon className="size-5" />
        </span>
      </button>
      <div className="flex-1 text-sm text-muted-foreground">
        <p className="mb-2">点击头像更换，支持 JPG、PNG、WebP 格式，最大 5MB</p>
        <Button
          type="button"
          size="legacySm"
          variant="outline"
          className="font-medium"
          onClick={() => inputRef.current?.click()}
        >
          <UploadIcon data-icon="inline-start" />
          上传头像
        </Button>
        {error && !open ? <p className="text-destructive">{error}</p> : null}
      </div>
      <input
        ref={inputRef}
        type="file"
        accept="image/jpeg,image/png,image/webp"
        className="sr-only"
        tabIndex={-1}
        onChange={chooseFile}
      />

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>裁剪头像</DialogTitle>
            <DialogDescription>
              拖动图片调整位置，PeerGo 将保存为清晰的正方形头像。
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col items-center gap-3">
            <div
              className="relative cursor-move touch-none overflow-hidden rounded-md bg-muted ring-1 ring-border select-none"
              style={{ width: viewportSize, height: viewportSize }}
              onPointerDown={beginDrag}
              onPointerMove={drag}
              onPointerUp={endDrag}
              onPointerCancel={endDrag}
            >
              {sourceUrl ? (
                <img
                  ref={imageRef}
                  src={sourceUrl}
                  alt="头像裁剪预览"
                  draggable={false}
                  className="pointer-events-none absolute max-w-none"
                  style={{
                    width: displayedSize.width,
                    height: displayedSize.height,
                    left: (viewportSize - displayedSize.width) / 2 + offset.x,
                    top: (viewportSize - displayedSize.height) / 2 + offset.y,
                  }}
                  onLoad={(event) => {
                    const next = {
                      width: event.currentTarget.naturalWidth,
                      height: event.currentTarget.naturalHeight,
                    }
                    setImageSize(next)
                    setOffset({ x: 0, y: 0 })
                  }}
                />
              ) : null}
              <div className="pointer-events-none absolute inset-0 rounded-md ring-1 ring-white/50 ring-inset" />
            </div>
            <div className="flex items-center gap-2" aria-label="缩放头像">
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                aria-label="缩小"
                disabled={zoom <= 1}
                onClick={() => updateZoom(zoom - 0.2)}
              >
                <MinusIcon />
              </Button>
              <span className="w-16 text-center text-xs text-muted-foreground">
                {Math.round(zoom * 100)}%
              </span>
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                aria-label="放大"
                disabled={zoom >= 3}
                onClick={() => updateZoom(zoom + 0.2)}
              >
                <PlusIcon />
              </Button>
            </div>
          </div>
          {error ? (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={mutation.isPending}
              onClick={() => setOpen(false)}
            >
              取消
            </Button>
            <Button
              type="button"
              disabled={!imageSize || mutation.isPending}
              onClick={upload}
            >
              {mutation.isPending ? <Spinner /> : <ImageUpIcon />}
              {mutation.isPending ? "上传中…" : "确认上传"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function scaledImageSize(image: ImageSize, zoom: number) {
  const baseScale = Math.max(
    viewportSize / image.width,
    viewportSize / image.height
  )
  return {
    width: image.width * baseScale * zoom,
    height: image.height * baseScale * zoom,
  }
}

function clampOffset(offset: Offset, image: ImageSize): Offset {
  const maxX = Math.max(0, (image.width - viewportSize) / 2)
  const maxY = Math.max(0, (image.height - viewportSize) / 2)
  return {
    x: Math.min(maxX, Math.max(-maxX, offset.x)),
    y: Math.min(maxY, Math.max(-maxY, offset.y)),
  }
}

function cropToJPEG(
  image: HTMLImageElement,
  naturalSize: ImageSize,
  displayedSize: ImageSize,
  offset: Offset
) {
  const scale = displayedSize.width / naturalSize.width
  const left = (viewportSize - displayedSize.width) / 2 + offset.x
  const top = (viewportSize - displayedSize.height) / 2 + offset.y
  const sourceSize = viewportSize / scale
  const sourceX = -left / scale
  const sourceY = -top / scale
  const canvas = document.createElement("canvas")
  canvas.width = outputSize
  canvas.height = outputSize
  const context = canvas.getContext("2d")
  if (!context) return Promise.reject(new Error("canvas is unavailable"))
  context.drawImage(
    image,
    sourceX,
    sourceY,
    sourceSize,
    sourceSize,
    0,
    0,
    outputSize,
    outputSize
  )
  return new Promise<Blob>((resolve, reject) => {
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error("encode failed"))),
      "image/jpeg",
      0.9
    )
  })
}
