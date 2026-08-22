import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import { TorrentMediaInfoCard } from "~/features/torrent/components/torrent-rich-content"

const bdInfo = `DISC INFO:
Disc Title: TEST_DISC
PLAYLIST REPORT:
Length:                 1:55:11.237 (h:m:s.ms)
Total Bitrate:          70.68 Mbps
VIDEO:
Codec                   Bitrate             Description
-----                   -------             -----------
MPEG-H HEVC Video       54752 kbps          2160p / 23.976 fps / 16:9 / Main 10 @ Level 5.1 @ High / 10 bits / HDR10
AUDIO:
Codec                           Language        Bitrate         Description
-----                           --------        -------         -----------
Dolby TrueHD/Atmos Audio        English         5248 kbps       7.1+13 objects / 48 kHz / 4608 kbps
Dolby Digital Audio             French          640 kbps        5.1 / 48 kHz / 640 kbps
Dolby Digital Plus Audio        Spanish         1024 kbps       7.1 / 48 kHz / 1024 kbps
Dolby Digital Plus Audio        Italian         1024 kbps       7.1 / 48 kHz / 1024 kbps
Dolby Digital Plus Audio        German          1024 kbps       7.1 / 48 kHz / 1024 kbps
Dolby Digital Plus Audio        Japanese        1024 kbps       7.1 / 48 kHz / 1024 kbps
SUBTITLES:
Codec                           Language        Bitrate         Description
-----                           --------        -------         -----------
Presentation Graphics           English         23.249 kbps
Presentation Graphics           French          17.173 kbps
Presentation Graphics           Spanish         17.617 kbps
Presentation Graphics           Italian         17.102 kbps
Presentation Graphics           German          17.946 kbps
Presentation Graphics           Japanese        15.286 kbps`

describe("TorrentMediaInfoCard", () => {
  it("shows a compact BDInfo summary and expands track groups independently", async () => {
    const user = userEvent.setup()
    render(<TorrentMediaInfoCard mediaInfo={bdInfo} />)

    expect(screen.getByText("2160p")).toBeVisible()
    expect(screen.getByText("54752 kbps")).toBeVisible()
    expect(screen.getByText("HDR10 / 10 bits")).toBeVisible()
    expect(
      screen.getByText("English / Dolby TrueHD/Atmos Audio / 7.1 / @5248 kbps")
    ).toBeVisible()
    expect(
      screen.queryByText(
        "Japanese / Dolby Digital Plus Audio / 7.1 / @1024 kbps"
      )
    ).not.toBeInTheDocument()
    expect(
      screen.queryByText("Japanese Presentation Graphics")
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "展开更多音轨 (1)" }))
    expect(
      screen.getByText("Japanese / Dolby Digital Plus Audio / 7.1 / @1024 kbps")
    ).toBeVisible()
    expect(
      screen.queryByText("Japanese Presentation Graphics")
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "展开更多字幕 (1)" }))
    expect(screen.getByText("Japanese Presentation Graphics")).toBeVisible()
  })
})
