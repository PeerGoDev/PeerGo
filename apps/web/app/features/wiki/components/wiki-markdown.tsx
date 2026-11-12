import * as React from "react"
import Markdown, { type Components } from "react-markdown"
import remarkGfm from "remark-gfm"

export type WikiHeading = {
  id: string
  text: string
  level: 1 | 2 | 3
}

export function extractWikiHeadings(body: string): WikiHeading[] {
  const counts = new Map<string, number>()
  const headings: WikiHeading[] = []

  for (const line of body.split("\n")) {
    const match = /^(#{1,3})\s+(.+?)\s*#*\s*$/u.exec(line.trim())
    if (!match) continue
    const text = match[2].trim()
    const base = wikiHeadingSlug(text)
    const occurrence = counts.get(base) ?? 0
    counts.set(base, occurrence + 1)
    headings.push({
      id: occurrence === 0 ? base : `${base}-${occurrence}`,
      text,
      level: match[1].length as 1 | 2 | 3,
    })
  }

  return headings
}

export function WikiMarkdown({ body }: { body: string }) {
  const headingCounts = new Map<string, number>()
  const heading = (level: 1 | 2 | 3) =>
    function WikiMarkdownHeading({ children }: { children?: React.ReactNode }) {
      const text = reactText(children)
      const base = wikiHeadingSlug(text)
      const occurrence = headingCounts.get(base) ?? 0
      headingCounts.set(base, occurrence + 1)
      const id = occurrence === 0 ? base : `${base}-${occurrence}`
      const Tag = `h${level}` as const
      return (
        <Tag
          id={id}
          className={
            level === 1
              ? "scroll-mt-[calc(var(--shell-header-height)+1rem)] border-b pb-3 font-heading text-xl font-semibold tracking-title"
              : level === 2
                ? "mt-2 scroll-mt-[calc(var(--shell-header-height)+1rem)] border-b pb-2 font-heading text-lg font-semibold tracking-title"
                : "mt-1 scroll-mt-[calc(var(--shell-header-height)+1rem)] font-heading text-base font-semibold tracking-title"
          }
        >
          {children}
        </Tag>
      )
    }

  const components: Components = {
    h1: heading(1),
    h2: heading(2),
    h3: heading(3),
    h4: ({ children }) => (
      <h4 className="font-heading text-md font-semibold">{children}</h4>
    ),
    p: ({ children }) => <p className="leading-7">{children}</p>,
    a: ({ href, children }) => {
      const external = Boolean(href?.startsWith("http"))
      return (
        <a
          href={href}
          target={external ? "_blank" : undefined}
          rel={external ? "noreferrer" : undefined}
          className="font-medium text-link underline decoration-link/40 underline-offset-4 hover:decoration-link"
        >
          {children}
        </a>
      )
    },
    ul: ({ children }) => (
      <ul className="flex list-disc flex-col gap-1.5 pl-6 marker:text-faint">
        {children}
      </ul>
    ),
    ol: ({ children }) => (
      <ol className="flex list-decimal flex-col gap-1.5 pl-6 marker:text-faint">
        {children}
      </ol>
    ),
    blockquote: ({ children }) => (
      <blockquote className="rounded-r-md border-l-2 border-primary/60 bg-muted px-4 py-3 text-muted-foreground">
        {children}
      </blockquote>
    ),
    code: ({ className, children }) => (
      <code
        className={
          className
            ? `${className} font-mono text-[0.9em]`
            : "rounded-sm border bg-muted px-1.5 py-0.5 font-mono text-[0.9em]"
        }
      >
        {children}
      </code>
    ),
    pre: ({ children }) => (
      <pre className="overflow-x-auto rounded-lg border bg-muted p-4 text-sm [&>code]:border-0 [&>code]:bg-transparent [&>code]:p-0">
        {children}
      </pre>
    ),
    img: ({ src, alt }) => (
      <img
        src={src}
        alt={alt ?? ""}
        loading="lazy"
        className="h-auto max-w-full rounded-lg border"
      />
    ),
    table: ({ children }) => (
      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full border-collapse text-sm [&_tbody_tr:last-child_td]:border-b-0">
          {children}
        </table>
      </div>
    ),
    th: ({ children }) => (
      <th className="border-b bg-muted px-3 py-2 text-left font-semibold">
        {children}
      </th>
    ),
    td: ({ children }) => <td className="border-b px-3 py-2">{children}</td>,
    hr: () => <hr className="border-border" />,
  }

  return (
    <article className="flex min-w-0 flex-col gap-4 text-md break-words [&_li]:leading-7 [&_strong]:font-semibold">
      <Markdown remarkPlugins={[remarkGfm]} components={components}>
        {body}
      </Markdown>
    </article>
  )
}

function wikiHeadingSlug(text: string) {
  const slug = text
    .toLocaleLowerCase("zh-CN")
    .replace(/[^\p{Letter}\p{Number}]+/gu, "-")
    .replace(/^-+|-+$/gu, "")
  return slug || "section"
}

function reactText(node: React.ReactNode): string {
  if (typeof node === "string" || typeof node === "number") {
    return String(node)
  }
  if (Array.isArray(node)) return node.map(reactText).join("")
  if (React.isValidElement<{ children?: React.ReactNode }>(node)) {
    return reactText(node.props.children)
  }
  return ""
}
