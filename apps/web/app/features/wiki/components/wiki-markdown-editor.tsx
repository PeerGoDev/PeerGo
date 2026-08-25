import * as React from "react"
import MDEditor from "@uiw/react-md-editor/nohighlight"
import { Code2Icon, EyeIcon, PencilIcon } from "lucide-react"

import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"

type EditorMode = "edit" | "live" | "preview"

export function WikiMarkdownEditor({
  id,
  value,
  onValueChange,
  invalid = false,
  disabled = false,
  minHeight = 440,
}: {
  id: string
  value: string
  onValueChange: (value: string) => void
  invalid?: boolean
  disabled?: boolean
  minHeight?: number
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
        variant="outline"
        size="sm"
        spacing={0}
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
        onChange={(nextValue) => onValueChange(nextValue ?? "")}
        preview={mode}
        height={minHeight}
        textareaProps={{
          id,
          "aria-invalid": invalid,
          disabled,
          placeholder: "使用 Markdown 编写 Wiki 正文",
        }}
      />
      <p className="text-xs text-muted-foreground">
        支持 Markdown；只有点击保存才会产生一个新版本，不记录自动保存草稿。
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
