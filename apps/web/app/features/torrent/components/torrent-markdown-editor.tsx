import * as React from "react"
import MDEditor from "@uiw/react-md-editor/nohighlight"
import { Code2Icon, EyeIcon, PencilIcon } from "lucide-react"

import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
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
      <ToggleGroup
        value={[mode]}
        onValueChange={(next) => {
          const selected = next[0] as EditorMode | undefined
          if (selected) setMode(selected)
        }}
        variant="segment"
        size="sm"
        spacing={0.5}
        aria-label="编辑器显示模式"
      >
        <ToggleGroupItem value="edit" disabled={disabled}>
          <PencilIcon data-icon="inline-start" />
          编辑
        </ToggleGroupItem>
        <ToggleGroupItem value="live" disabled={disabled}>
          <Code2Icon data-icon="inline-start" />
          实时预览
        </ToggleGroupItem>
        <ToggleGroupItem value="preview" disabled={disabled}>
          <EyeIcon data-icon="inline-start" />
          预览
        </ToggleGroupItem>
      </ToggleGroup>
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
