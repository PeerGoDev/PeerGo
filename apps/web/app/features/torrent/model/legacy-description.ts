const LEGACY_BBCODE_PATTERN =
  /\[(?:b|i|u|s|strike|url|img|quote|code|size|color|font|center|left|right|list|hr|br)(?:=[^\]]+)?\]/i

type LegacyDescriptionConversion = {
  markdown: string
  converted: boolean
  removedImages: boolean
}

/**
 * Converts the small, text-oriented BBCode subset accepted by PtYes into
 * Markdown. Image tags are deliberately removed: screenshots belong to the
 * structured upload field and must never be smuggled into the description as
 * remote embeds.
 */
export function convertLegacyDescription(
  input: string
): LegacyDescriptionConversion {
  if (!LEGACY_BBCODE_PATTERN.test(input)) {
    return { markdown: input, converted: false, removedImages: false }
  }

  let markdown = input
    .replace(
      /\[url=[^\]]*\]\s*\[img[^\]]*\][\s\S]*?\[\/img\]\s*\[\/url\]/gi,
      ""
    )
    .replace(/\[img[^\]]*\][\s\S]*?\[\/img\]/gi, "")
    .replace(/\[img=[^\]]+\]/gi, "")
  const removedImages = markdown !== input

  markdown = replacePairedTag(markdown, "b", (value) => `**${value}**`)
  markdown = replacePairedTag(markdown, "i", (value) => `*${value}*`)
  markdown = replacePairedTag(markdown, "u", (value) => value)
  markdown = replacePairedTag(markdown, "s", (value) => `~~${value}~~`)
  markdown = replacePairedTag(markdown, "strike", (value) => `~~${value}~~`)

  markdown = markdown.replace(
    /\[url=([^\]]+)\]([\s\S]*?)\[\/url\]/gi,
    (_, destination: string, label: string) =>
      safeLink(destination, label.trim())
  )
  markdown = markdown.replace(
    /\[url\]([\s\S]*?)\[\/url\]/gi,
    (_, destination: string) => safeLink(destination, destination.trim())
  )

  markdown = markdown.replace(
    /\[quote(?:=([^\]]+))?\]([\s\S]*?)\[\/quote\]/gi,
    (_, author: string | undefined, value: string) => {
      const body = value
        .trim()
        .split("\n")
        .map((line) => `> ${line}`)
        .join("\n")
      return author ? `> **${author.trim()}：**\n${body}` : body
    }
  )
  markdown = markdown.replace(
    /\[code(?:=([^\]]+))?\]([\s\S]*?)\[\/code\]/gi,
    (_, language: string | undefined, value: string) =>
      `\`\`\`${safeCodeLanguage(language)}\n${value.trim()}\n\`\`\``
  )

  for (const tag of ["size", "color", "font"] as const) {
    markdown = replacePairedTag(markdown, tag, (value) => value, true)
  }
  for (const tag of ["center", "left", "right"] as const) {
    markdown = replacePairedTag(markdown, tag, (value) => value)
  }

  markdown = markdown.replace(
    /\[list(?:=1)?\]([\s\S]*?)\[\/list\]/gi,
    (match: string, value: string) => {
      const ordered = /^\[list=1\]/i.test(match)
      const items = value
        .split(/\[\*\]/g)
        .map((item) => item.trim())
        .filter(Boolean)
      return items
        .map((item, index) => `${ordered ? `${index + 1}.` : "-"} ${item}`)
        .join("\n")
    }
  )
  markdown = markdown.replace(/\[hr\]/gi, "\n---\n")
  markdown = markdown.replace(/\[br\]/gi, "\n")

  const converted = markdown !== input
  return {
    markdown: converted ? markdown.replace(/\n{3,}/g, "\n\n").trim() : input,
    converted,
    removedImages,
  }
}

function replacePairedTag(
  input: string,
  tag: string,
  render: (value: string) => string,
  hasAttribute = false
) {
  const attribute = hasAttribute ? "=[^\\]]+" : ""
  const pattern = new RegExp(
    `\\[${tag}${attribute}\\]([\\s\\S]*?)\\[\\/${tag}\\]`,
    "gi"
  )
  return input.replace(pattern, (_, value: string) => render(value))
}

function safeLink(destination: string, label: string) {
  const normalized = destination.trim()
  try {
    const url = new URL(normalized)
    if (url.protocol !== "http:" && url.protocol !== "https:") return label
  } catch {
    return label
  }
  return `[${label.replaceAll("]", "\\]")}](${normalized.replaceAll(")", "%29")})`
}

function safeCodeLanguage(language: string | undefined) {
  return language?.trim().replace(/[^a-z0-9_+-]/gi, "") ?? ""
}
