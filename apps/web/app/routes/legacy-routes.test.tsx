import { render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter, Route, Routes, useLocation } from "react-router"
import { describe, expect, it } from "vitest"

import LegacyAdminRoute from "~/routes/legacy-admin"
import LegacyListRoute from "~/routes/legacy-list"
import LegacyMessagesRoute from "~/routes/legacy-messages"
import LegacyTorrentRoute from "~/routes/legacy-torrent.$torrentId"
import LegacyContentPromotionsRoute from "~/routes/staff.content.promotions"

describe("PtYes compatibility routes", () => {
  it.each([
    {
      source: "/list?category=movies&q=echo",
      route: "/list",
      element: <LegacyListRoute />,
      expected: "/torrents?category=movies&q=echo",
    },
    {
      source: "/torrent/019fcd83-57de-7240-a0d3-95908cdb4501?from=legacy",
      route: "/torrent/:torrentId",
      element: <LegacyTorrentRoute />,
      expected: "/torrents/019fcd83-57de-7240-a0d3-95908cdb4501?from=legacy",
    },
    {
      source: "/messages?filter=unread",
      route: "/messages",
      element: <LegacyMessagesRoute />,
      expected: "/notifications?filter=unread",
    },
    {
      source: "/admin?section=users",
      route: "/admin",
      element: <LegacyAdminRoute />,
      expected: "/staff?section=users",
    },
    {
      source: "/staff/content/promotions",
      route: "/staff/content/promotions",
      element: <LegacyContentPromotionsRoute />,
      expected: "/staff/settings/promotions",
    },
  ])("redirects $source to $expected", async (example) => {
    render(
      <MemoryRouter initialEntries={[example.source]}>
        <Routes>
          <Route path={example.route} element={example.element} />
          <Route path="*" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>
    )

    await waitFor(() =>
      expect(screen.getByTestId("location")).toHaveTextContent(example.expected)
    )
  })
})

function LocationProbe() {
  const location = useLocation()
  return (
    <output data-testid="location">
      {location.pathname}
      {location.search}
    </output>
  )
}
