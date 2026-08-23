const mediaInfoSummaryInputLimit = 1024 * 1024
const mediaInfoSummaryLineLimit = 20_000
const mediaInfoTrackLimit = 32

type MediaInfoSection = "general" | "video" | "audio" | "text" | "other"
type BdInfoSection =
  | "disc"
  | "playlist"
  | "video"
  | "audio"
  | "subtitle"
  | "other"

type RawTrack = {
  format?: string
  commercialName?: string
  title?: string
  language?: string
  channels?: string
  bitRate?: string
}

export type MediaInfoSummary = {
  duration?: string
  resolution?: string
  overallBitRate?: string
  videoBitRate?: string
  bitDepth?: string
  hdr?: string
  frameRate?: string
  profile?: string
  videoFormat?: string
  audioTracks: string[]
  subtitleTracks: string[]
}

// MediaInfo and BDInfo are user-supplied diagnostic text, not a stable API.
// Only bounded, well-known summary fields are parsed; the original text stays
// available unchanged. Bounding bytes, lines, and tracks prevents malformed
// legacy values from monopolizing the browser main thread.
export function summarizeMediaInfo(raw: string): MediaInfoSummary {
  const source = raw
    .slice(0, mediaInfoSummaryInputLimit)
    .replace(/\u00a0/g, " ")
  const lines = source.split(/\r\n|\n|\r/, mediaInfoSummaryLineLimit)

  return looksLikeBdInfo(lines)
    ? summarizeBdInfo(lines)
    : summarizeStandardMediaInfo(lines)
}

export function hasMediaInfoSummary(summary: MediaInfoSummary) {
  return Boolean(
    summary.duration ||
    summary.resolution ||
    summary.overallBitRate ||
    summary.videoBitRate ||
    summary.bitDepth ||
    summary.hdr ||
    summary.frameRate ||
    summary.profile ||
    summary.videoFormat ||
    summary.audioTracks.length ||
    summary.subtitleTracks.length
  )
}

function summarizeStandardMediaInfo(lines: string[]): MediaInfoSummary {
  const summary = emptyMediaInfoSummary()
  let section: MediaInfoSection = "other"
  let track: RawTrack | undefined
  let videoWidth: string | undefined
  let videoHeight: string | undefined
  let aspectRatio: string | undefined
  let legacyAspectRatio: string | undefined

  const finishTrack = () => {
    if (!track) return
    if (
      section === "audio" &&
      summary.audioTracks.length < mediaInfoTrackLimit
    ) {
      const label = [
        track.title || track.language,
        track.commercialName || track.format,
        track.channels,
        track.bitRate ? `@${track.bitRate}` : undefined,
      ]
        .filter(Boolean)
        .join(" / ")
      if (label) summary.audioTracks.push(label)
    }
    if (
      section === "text" &&
      summary.subtitleTracks.length < mediaInfoTrackLimit
    ) {
      const label = [track.title || track.language, track.format]
        .filter(Boolean)
        .join(" ")
      if (label) summary.subtitleTracks.push(label)
    }
    track = undefined
  }

  for (const rawLine of lines) {
    const line = rawLine.trim()
    const nextSection = mediaInfoSection(line)
    if (nextSection) {
      finishTrack()
      section = nextSection
      track = section === "audio" || section === "text" ? {} : undefined
      continue
    }

    const field = mediaInfoField(line)
    if (!field) continue
    const { key, value } = field

    const legacyField = applyLegacyReleaseField(summary, key, value)
    if (legacyField.handled) {
      if (legacyField.aspectRatio) {
        legacyAspectRatio = legacyField.aspectRatio
      }
      continue
    }

    switch (section) {
      case "general":
        if (key === "duration" && !summary.duration) summary.duration = value
        if (key === "overall bit rate" && !summary.overallBitRate) {
          summary.overallBitRate = value
        }
        break
      case "video":
        if (key === "format" && !summary.videoFormat) {
          summary.videoFormat = value
        } else if (key === "format profile" && !summary.profile) {
          summary.profile = value
        } else if (key === "width" && !videoWidth) {
          videoWidth = numericMediaDimension(value)
        } else if (key === "height" && !videoHeight) {
          videoHeight = numericMediaDimension(value)
        } else if (key === "display aspect ratio" && !aspectRatio) {
          aspectRatio = value
        } else if (key === "bit rate" && !summary.videoBitRate) {
          summary.videoBitRate = value
        } else if (key === "frame rate" && !summary.frameRate) {
          summary.frameRate = value
        } else if (key === "bit depth" && !summary.bitDepth) {
          summary.bitDepth = value
        } else if (key === "hdr format" && !summary.hdr) {
          summary.hdr = hdrLabel(value)
        } else if (key === "transfer characteristics" && !summary.hdr) {
          summary.hdr = hdrLabel(value)
        }
        break
      case "audio":
      case "text":
        if (!track) track = {}
        if (key === "format" && !track.format) track.format = value
        if (key === "commercial name" && !track.commercialName) {
          track.commercialName = value
        }
        if (key === "title" && !track.title) track.title = value
        if (key === "language" && !track.language) track.language = value
        if (key === "channel(s)" && !track.channels) track.channels = value
        if (key === "bit rate" && !track.bitRate) track.bitRate = value
        break
    }
  }
  finishTrack()

  const resolvedAspectRatio = aspectRatio || legacyAspectRatio
  if (videoHeight) {
    summary.resolution = `${videoHeight}p${resolvedAspectRatio ? ` (${resolvedAspectRatio})` : ""}`
  } else if (videoWidth) {
    summary.resolution = `${videoWidth}px${resolvedAspectRatio ? ` (${resolvedAspectRatio})` : ""}`
  } else if (
    summary.resolution &&
    resolvedAspectRatio &&
    !summary.resolution.includes(resolvedAspectRatio)
  ) {
    summary.resolution = `${summary.resolution} (${resolvedAspectRatio})`
  }
  return summary
}

function mediaInfoField(line: string) {
  const asciiSeparator = line.indexOf(":")
  const fullWidthSeparator = line.indexOf("：")
  const separator =
    asciiSeparator < 0
      ? fullWidthSeparator
      : fullWidthSeparator < 0
        ? asciiSeparator
        : Math.min(asciiSeparator, fullWidthSeparator)
  if (separator < 1) return undefined

  const key = line.slice(0, separator).trim().toLowerCase()
  const value = line.slice(separator + 1).trim()
  return value ? { key, value } : undefined
}

function applyLegacyReleaseField(
  summary: MediaInfoSummary,
  rawKey: string,
  value: string
): { handled: boolean; aspectRatio?: string } {
  const key = rawKey.replace(/[\s\u3000._…·]+/g, "").toLowerCase()

  if (key === "duration" || key === "时长") {
    if (!summary.duration) summary.duration = value
    return { handled: true }
  }
  if (key === "overallbitrate" || key === "总码率") {
    if (!summary.overallBitRate) summary.overallBitRate = value
    return { handled: true }
  }
  if (key === "videobitrate" || key === "视频码率") {
    if (!summary.videoBitRate) summary.videoBitRate = value
    return { handled: true }
  }
  if (key === "framerate" || key === "帧率") {
    if (!summary.frameRate) summary.frameRate = value
    return { handled: true }
  }
  if (key === "resolution" || key === "分辨率") {
    if (!summary.resolution) summary.resolution = value
    return { handled: true }
  }
  if (key === "displayaspectratio" || key === "长宽比") {
    return { handled: true, aspectRatio: value }
  }
  if (key === "videocodec" || key === "视频信息") {
    applyLegacyVideoDescription(summary, value)
    return { handled: true }
  }
  if (/^audio\d*$/.test(key) || key === "音轨") {
    if (summary.audioTracks.length < mediaInfoTrackLimit) {
      summary.audioTracks.push(value)
    }
    return { handled: true }
  }
  if (/^subtitles?\d*$/.test(key) || /^字幕\d*$/.test(key)) {
    if (summary.subtitleTracks.length < mediaInfoTrackLimit) {
      summary.subtitleTracks.push(value)
    }
    return { handled: true }
  }
  return { handled: false }
}

function applyLegacyVideoDescription(summary: MediaInfoSummary, value: string) {
  if (!summary.videoFormat) {
    summary.videoFormat = value.match(
      /\b(?:MPEG-H HEVC Video|HEVC|AVC|AV1|VP9|H\.26[45]|x26[45])\b/i
    )?.[0]
  }
  if (!summary.profile) {
    summary.profile = value.match(
      /\b(?:Baseline|Main|High)(?:\s*10)?@L[\d.]+\b/i
    )?.[0]
  }
  if (!summary.videoBitRate) {
    summary.videoBitRate = value
      .match(/(?:^|@|\s)([\d,.][\d\s,.]*\s*[kmg]?b(?:it)?\/?s)\b/i)?.[1]
      ?.trim()
  }
}

function summarizeBdInfo(lines: string[]): MediaInfoSummary {
  const summary = emptyMediaInfoSummary()
  let section: BdInfoSection = "other"

  for (const rawLine of lines) {
    const line = rawLine.trim()
    if (!line) continue

    const nextSection = bdInfoSection(line)
    if (nextSection) {
      section = nextSection
      continue
    }

    if (parseCompactBdInfoLine(line, summary)) continue

    if (section === "playlist") {
      const duration = labeledValue(line, "Length")
      if (duration && !summary.duration) summary.duration = duration
      const bitRate = labeledValue(line, "Total Bitrate")
      if (bitRate && !summary.overallBitRate) summary.overallBitRate = bitRate
      continue
    }

    if (isBdInfoTableDecoration(line)) continue
    const columns = bdInfoColumns(rawLine)

    if (section === "video" && columns.length >= 3) {
      const [codec, bitRate, ...descriptionColumns] = columns
      const description = descriptionColumns.join("  ")
      if (!looksLikeBitRate(bitRate)) continue
      applyBdInfoVideo(summary, codec, bitRate, description)
      continue
    }

    if (
      section === "audio" &&
      columns.length >= 4 &&
      summary.audioTracks.length < mediaInfoTrackLimit
    ) {
      const [codec, language, bitRate, ...descriptionColumns] = columns
      if (!looksLikeBitRate(bitRate)) continue
      const channels = audioChannels(descriptionColumns.join("  "))
      const label = [language, cleanBdInfoCodec(codec), channels, `@${bitRate}`]
        .filter(Boolean)
        .join(" / ")
      if (label) summary.audioTracks.push(label)
      continue
    }

    if (
      section === "subtitle" &&
      columns.length >= 3 &&
      summary.subtitleTracks.length < mediaInfoTrackLimit
    ) {
      const [format, language, bitRate] = columns
      if (!language || !looksLikeBitRate(bitRate)) continue
      summary.subtitleTracks.push(
        [language, cleanBdInfoCodec(format)].filter(Boolean).join(" ")
      )
    }
  }

  return summary
}

function parseCompactBdInfoLine(
  line: string,
  summary: MediaInfoSummary
): boolean {
  const duration = labeledValue(line, "Length")
  if (duration) {
    if (!summary.duration) summary.duration = duration
    return true
  }

  const totalBitRate = labeledValue(line, "Total Bitrate")
  if (totalBitRate) {
    if (!summary.overallBitRate) summary.overallBitRate = totalBitRate
    return true
  }

  const video = labeledValue(line, "Video")
  if (video) {
    const parts = video.split(/\s+\/\s+/).map((part) => part.trim())
    const bitRateIndex = parts.findIndex(looksLikeBitRate)
    if (bitRateIndex > 0) {
      const codec = parts.slice(0, bitRateIndex).join(" / ")
      const description = parts.slice(bitRateIndex + 1).join(" / ")
      applyBdInfoVideo(summary, codec, parts[bitRateIndex] ?? "", description)
    }
    return true
  }

  const audio = labeledValue(line, "Audio")
  if (audio) {
    if (summary.audioTracks.length >= mediaInfoTrackLimit) return true
    const parts = audio.split(/\s+\/\s+/).map((part) => part.trim())
    const bitRate = parts.find(looksLikeBitRate)
    const label = [
      parts[0],
      parts[1],
      parts[2],
      bitRate ? `@${bitRate}` : undefined,
    ]
      .filter(Boolean)
      .join(" / ")
    if (label) summary.audioTracks.push(label)
    return true
  }

  const subtitle =
    labeledValue(line, "Subtitle") ?? labeledValue(line, "Subtitles")
  if (subtitle) {
    if (summary.subtitleTracks.length >= mediaInfoTrackLimit) return true
    const parts = subtitle.split(/\s+\/\s+/).map((part) => part.trim())
    const label = parts.filter(Boolean).slice(0, 2).join(" ")
    if (label) summary.subtitleTracks.push(label)
    return true
  }

  return false
}

function applyBdInfoVideo(
  summary: MediaInfoSummary,
  codec: string,
  bitRate: string,
  description: string
) {
  if (!summary.videoFormat) summary.videoFormat = cleanBdInfoCodec(codec)
  if (!summary.videoBitRate) summary.videoBitRate = bitRate

  const resolution = description.match(/\b(\d{3,4}[pi])\b/i)?.[1]
  if (resolution && !summary.resolution) summary.resolution = resolution

  const frameRate = description.match(/\b(\d+(?:\.\d+)?)\s*fps\b/i)?.[1]
  if (frameRate && !summary.frameRate) {
    summary.frameRate = `${frameRate} fps`
  }

  const bitDepth = description.match(/\b(\d+)\s*(?:bits?|bit)\b/i)?.[1]
  if (bitDepth && !summary.bitDepth) summary.bitDepth = `${bitDepth} bits`

  if (!summary.hdr) summary.hdr = hdrLabel(description)

  if (!summary.profile) {
    summary.profile = description
      .split(/\s+\/\s+/)
      .map((part) => part.trim())
      .find((part) => /\b(?:profile|main|high|baseline|level)\b/i.test(part))
  }
}

function emptyMediaInfoSummary(): MediaInfoSummary {
  return { audioTracks: [], subtitleTracks: [] }
}

function looksLikeBdInfo(lines: string[]) {
  let sectionCount = 0
  let hasBdInfoMarker = false
  let hasCompactVideo = false
  let hasCompactAudio = false

  for (const rawLine of lines) {
    const line = rawLine.trim()
    if (
      /^(?:DISC INFO|PLAYLIST REPORT):?$/i.test(line) ||
      /^(?:VIDEO|AUDIO|SUBTITLES?):$/i.test(line)
    ) {
      sectionCount += 1
    }
    if (/^(?:BDInfo|Disc Title|Disc Label)\s*:/i.test(line)) {
      hasBdInfoMarker = true
    }
    if (/^Video\s*:.+\/(?:.+\/)*\s*[\d,.]+\s*[kmg]?bps\b/i.test(line)) {
      hasCompactVideo = true
    }
    if (/^Audio\s*:.+\//i.test(line)) hasCompactAudio = true
  }

  return (
    sectionCount >= 2 ||
    (sectionCount >= 1 && hasBdInfoMarker) ||
    (hasCompactVideo && hasCompactAudio)
  )
}

function mediaInfoSection(line: string): MediaInfoSection | undefined {
  const heading = line
    .replace(/^[★☆=*\-\s]+/, "")
    .replace(/[★☆=*\-\s]+$/, "")
    .trim()
  const match =
    /^(General(?:\s+Information)?|Video(?:\s+Information)?|Audio(?:\s+Information)?|Text|Subtitle)(?:\s*#\d+)?$/i.exec(
      heading
    )
  if (!match) return undefined
  switch (match[1]?.toLowerCase()) {
    case "general":
    case "general information":
      return "general"
    case "video":
    case "video information":
      return "video"
    case "audio":
    case "audio information":
      return "audio"
    case "text":
    case "subtitle":
      return "text"
    default:
      return "other"
  }
}

function bdInfoSection(line: string): BdInfoSection | undefined {
  const heading = line.replace(/:\s*$/, "").trim().toUpperCase()
  switch (heading) {
    case "DISC INFO":
      return "disc"
    case "PLAYLIST REPORT":
      return "playlist"
    case "VIDEO":
      return "video"
    case "AUDIO":
      return "audio"
    case "SUBTITLE":
    case "SUBTITLES":
      return "subtitle"
    case "CHAPTERS":
    case "FILES":
    case "STREAM DIAGNOSTICS":
    case "QUICK SUMMARY":
      return "other"
    default:
      return undefined
  }
}

function labeledValue(line: string, label: string) {
  const match = new RegExp(`^${label}\\s*:\\s*(.+)$`, "i").exec(line)
  return match?.[1]?.trim() || undefined
}

function isBdInfoTableDecoration(line: string) {
  return /^(?:Codec\s+|[-\s]+$)/i.test(line)
}

function bdInfoColumns(line: string) {
  return line
    .trim()
    .split(/(?:\t+|\s{2,})/)
    .map((column) => column.trim())
    .filter(Boolean)
}

function cleanBdInfoCodec(value: string) {
  return value.replace(/^\*\s*/, "").trim()
}

function looksLikeBitRate(value: string | undefined) {
  return Boolean(value && /^[\d,.]+\s*[kmg]?b(?:it)?ps\b/i.test(value.trim()))
}

function audioChannels(description: string) {
  return description.match(/\b(\d+(?:\.\d+)?)(?:\+\d+\s*objects?)?\b/i)?.[1]
}

function hdrLabel(value: string) {
  const labels = [
    [/Dolby Vision/i, "Dolby Vision"],
    [/HDR10\+/i, "HDR10+"],
    [/\bHDR10\b/i, "HDR10"],
    [/\bHLG\b/i, "HLG"],
  ] as const
  const matches = labels
    .filter(([pattern]) => pattern.test(value))
    .map(([, label]) => label)
  if (matches.length === 0 && /(?:SMPTE ST 2084|\bPQ\b)/i.test(value)) {
    matches.push("HDR10")
  }
  return [...new Set(matches)].join(" / ") || undefined
}

function numericMediaDimension(value: string) {
  const digits = value.match(/[0-9][0-9\s,.]*/)?.[0]?.replace(/[^0-9]/g, "")
  return digits || undefined
}
