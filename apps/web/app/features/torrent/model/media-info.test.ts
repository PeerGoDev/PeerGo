import { describe, expect, it } from "vitest"

import {
  hasMediaInfoSummary,
  summarizeMediaInfo,
} from "~/features/torrent/model/media-info"

describe("summarizeMediaInfo", () => {
  it("extracts a bounded display summary without rewriting the source", () => {
    const summary = summarizeMediaInfo(`General
Duration : 1 h 53 min
Overall bit rate : 40.2 Mb/s
Video
Format : AVC
Format profile : High@L4.1
Width : 1 920 pixels
Height : 1 080 pixels
Display aspect ratio : 16:9
Frame rate : 23.976 (24000/1001) FPS
Bit depth : 8 bits
Audio
Format : MLP FBA 16-ch
Commercial name : Dolby TrueHD with Dolby Atmos
Bit rate : 4 346 kb/s
Channel(s) : 8 channels
Title : English
Language : English
Text #1
Format : PGS
Title : chs&eng
Language : Chinese (Simplified)`)

    expect(summary).toEqual({
      duration: "1 h 53 min",
      resolution: "1080p (16:9)",
      overallBitRate: "40.2 Mb/s",
      bitDepth: "8 bits",
      frameRate: "23.976 (24000/1001) FPS",
      profile: "High@L4.1",
      videoFormat: "AVC",
      audioTracks: [
        "English / Dolby TrueHD with Dolby Atmos / 8 channels / @4 346 kb/s",
      ],
      subtitleTracks: ["chs&eng / PGS"],
    })
    expect(hasMediaInfoSummary(summary)).toBe(true)
  })

  it("returns an empty summary for unsupported text", () => {
    const summary = summarizeMediaInfo("custom release notes only")
    expect(hasMediaInfoSummary(summary)).toBe(false)
  })
})
