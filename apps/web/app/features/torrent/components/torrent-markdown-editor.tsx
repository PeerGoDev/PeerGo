import * as React from "react"
import MDEditor from "@uiw/react-md-editor/nohighlight"
import { Code2Icon, EyeIcon, PencilIcon } from "lucide-react"

import { Button } from "~/components/ui/button"
import { convertLegacyDescription } from "~/features/torrent/model/legacy-description"

type EditorMode = "edit" | "live" | "preview"

export function TorrentMarkdownEditor({
  id,
  value,
  onValueChange,
  invalid,
  disabled,
}: {
  id: string
  value: string
  onValueChange: (value: string) => void
  invalid: boolean
  disabled: boolean
}) {
  const [mode, setMode] = React.useState<EditorMode>("live")
  const colorMode = useDocumentColorMode()

  return (
    <div className="flex flex-col gap-2" data-color-mode={colorMode}>
      <div className="flex flex-wrap items-center gap-1">
        <EditorModeButton
          active={mode === "edit"}
          disabled={disabled}
          icon={PencilIcon}
          label="编辑"
          onClick={() => setMode("edit")}
        />
        <EditorModeButton
          active={mode === "live"}
          disabled={disabled}
          icon={Code2Icon}
          label="实时预览"
          onClick={() => setMode("live")}
        />
        <EditorModeButton
          active={mode === "preview"}
          disabled={disabled}
          icon={EyeIcon}
          label="预览"
          onClick={() => setMode("preview")}
        />
      </div>
      <MDEditor
        value={value}
        onChange={(nextValue) => {
          const conversion = convertLegacyDescription(nextValue ?? "")
          onValueChange(conversion.markdown)
        }}
        preview={mode}
        height={250}
        textareaProps={{
          id,
          "aria-invalid": invalid,
          disabled,
          placeholder: "种子描述，支持 Markdown 语法，粘贴 BBCode 会自动转换",
        }}
      />
      <p className="text-xs text-muted-foreground">
        支持 Markdown 语法 | 粘贴 BBCode 内容会自动转换为 Markdown
      </p>
    </div>
  )
}

function EditorModeButton({
  active,
  disabled,
  icon: Icon,
  label,
  onClick,
}: {
  active: boolean
  disabled: boolean
  icon: React.ComponentType<{ className?: string }>
  label: string
  onClick: () => void
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant={active ? "default" : "ghost"}
      disabled={disabled}
      onClick={onClick}
    >
      <Icon data-icon="inline-start" />
      {label}
    </Button>
  )
}

function useDocumentColorMode() {
  const [colorMode, setColorMode] = React.useState<"light" | "dark">("light")

  React.useEffect(() => {
    const root = document.documentElement
    const sync = () =>
      setColorMode(root.classList.contains("dark") ? "dark" : "light")
    sync()
    const observer = new MutationObserver(sync)
    observer.observe(root, { attributes: true, attributeFilter: ["class"] })
    return () => observer.disconnect()
  }, [])

  return colorMode
}
