import { cn } from "~/lib/utils"

/**
 * Renders announcement copy with prose-like paragraph rhythm while keeping the
 * public boundary text-only. Neither plain text nor imported legacy BBCode is
 * interpreted as HTML, Markdown, links, or executable markup.
 */
export function AnnouncementBody({
  body,
  legacy = false,
}: {
  body: string
  legacy?: boolean
}) {
  const paragraphs = body
    .trim()
    .split(/\n(?:[\t ]*\n)+/u)
    .filter((paragraph) => paragraph.length > 0)

  return (
    <div
      className={cn(
        "flex max-w-none flex-col gap-4 text-base leading-7 break-words",
        legacy && "font-mono text-[13px]"
      )}
    >
      {paragraphs.map((paragraph, index) => (
        <p
          key={`${index}:${paragraph.slice(0, 24)}`}
          className="whitespace-pre-wrap"
        >
          {paragraph}
        </p>
      ))}
    </div>
  )
}
