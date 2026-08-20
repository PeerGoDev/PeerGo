const mediaInfoSummaryInputLimit = 1024 * 1024
const mediaInfoSummaryLineLimit = 20_000
const mediaInfoTrackLimit = 32

type Section = "general" | "video" | "audio" | "text" | "other"

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
  bitDepth?: string
  frameRate?: string
  profile?: string
  videoFormat?: string
  audioTracks: string[]
  subtitleTracks: string[]
}

// MediaInfo and BDInfo are user-supplied diagnostic text, not a stable API.
// This parser intentionally recognizes only bounded, well-known summary keys;
// the original content remains available unchanged whenever a variant is not
// understood. Bounding both bytes and lines keeps a legacy 16 MiB value from
// monopolizing the browser main thread.
export function summarizeMediaInfo(raw: string): MediaInfoSummary {
  const summary: MediaInfoSummary = { audioTracks: [], subtitleTracks: [] }
  const source = raw.slice(0, mediaInfoSummaryInputLimit)
  const lines = source.split(/\r?\n/, mediaInfoSummaryLineLimit)
  let section: Section = "other"
  let track: RawTrack | undefined
  let videoWidth: string | undefined
  let videoHeight: string | undefined
  let aspectRatio: string | undefined

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
        .join(" / ")
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

    const separator = line.indexOf(":")
    if (separator < 1) continue
    const key = line.slice(0, separator).trim().toLowerCase()
    const value = line.slice(separator + 1).trim()
    if (!value) continue

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
        } else if (key === "frame rate" && !summary.frameRate) {
          summary.frameRate = value
        } else if (key === "bit depth" && !summary.bitDepth) {
          summary.bitDepth = value
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

  if (videoHeight) {
    summary.resolution = `${videoHeight}p${aspectRatio ? ` (${aspectRatio})` : ""}`
  } else if (videoWidth) {
    summary.resolution = `${videoWidth}px${aspectRatio ? ` (${aspectRatio})` : ""}`
  }
  return summary
}

export function hasMediaInfoSummary(summary: MediaInfoSummary) {
  return Boolean(
    summary.duration ||
    summary.resolution ||
    summary.overallBitRate ||
    summary.bitDepth ||
    summary.frameRate ||
    summary.profile ||
    summary.videoFormat ||
    summary.audioTracks.length ||
    summary.subtitleTracks.length
  )
}

function mediaInfoSection(line: string): Section | undefined {
  const match = /^(General|Video|Audio|Text)(?:\s*#\d+)?$/i.exec(line)
  if (!match) return undefined
  switch (match[1]?.toLowerCase()) {
    case "general":
      return "general"
    case "video":
      return "video"
    case "audio":
      return "audio"
    case "text":
      return "text"
    default:
      return "other"
  }
}

function numericMediaDimension(value: string) {
  const digits = value.match(/[0-9][0-9\s,.]*/)?.[0]?.replace(/[^0-9]/g, "")
  return digits || undefined
}
