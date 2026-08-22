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
      subtitleTracks: ["chs&eng PGS"],
    })
    expect(hasMediaInfoSummary(summary)).toBe(true)
  })

  it("parses the BDInfo table layout used by migrated PtYes torrents", () => {
    const summary = summarizeMediaInfo(`DISC INFO:

Disc Title:     MULAN_-_ULTRA_HD
Disc Size:      62,270,249,859 bytes
BDInfo:         0.7.5.9

PLAYLIST REPORT:

Name:                   00800.MPLS
Length:                 1:55:11.237 (h:m:s.ms)
Total Bitrate:          70.68 Mbps

VIDEO:

Codec                   Bitrate             Description
-----                   -------             -----------
MPEG-H HEVC Video       54752 kbps          2160p / 23.976 fps / 16:9 / Main 10 @ Level 5.1 @ High / 4:2:0 / 10 bits / 1000nits / HDR10 / BT.2020

AUDIO:

Codec                           Language        Bitrate         Description
-----                           --------        -------         -----------
Dolby TrueHD/Atmos Audio        English         5248 kbps       7.1+13 objects / 48 kHz / 4608 kbps / 24-bit
Dolby Digital Audio             English         320 kbps        2.0 / 48 kHz / 320 kbps

SUBTITLES:

Codec                           Language        Bitrate         Description
-----                           --------        -------         -----------
Presentation Graphics           English         23.249 kbps
Presentation Graphics           French          17.173 kbps`)

    expect(summary).toEqual({
      duration: "1:55:11.237 (h:m:s.ms)",
      resolution: "2160p",
      overallBitRate: "70.68 Mbps",
      videoBitRate: "54752 kbps",
      bitDepth: "10 bits",
      hdr: "HDR10",
      frameRate: "23.976 fps",
      profile: "Main 10 @ Level 5.1 @ High",
      videoFormat: "MPEG-H HEVC Video",
      audioTracks: [
        "English / Dolby TrueHD/Atmos Audio / 7.1 / @5248 kbps",
        "English / Dolby Digital Audio / 2.0 / @320 kbps",
      ],
      subtitleTracks: [
        "English Presentation Graphics",
        "French Presentation Graphics",
      ],
    })
    expect(hasMediaInfoSummary(summary)).toBe(true)
  })

  it("caps parsed tracks from untrusted input", () => {
    const audioRows = Array.from(
      { length: 40 },
      (_, index) =>
        `Dolby Digital Audio             Language${index}      640 kbps        5.1 / 48 kHz / 640 kbps`
    ).join("\n")

    const summary = summarizeMediaInfo(`DISC INFO:
Disc Title: TEST
AUDIO:
Codec                           Language        Bitrate         Description
-----                           --------        -------         -----------
${audioRows}`)

    expect(summary.audioTracks).toHaveLength(32)
  })

  it("returns an empty summary for unsupported text", () => {
    const summary = summarizeMediaInfo("custom release notes only")
    expect(hasMediaInfoSummary(summary)).toBe(false)
  })
})
