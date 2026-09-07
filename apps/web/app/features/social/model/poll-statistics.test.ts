import { describe, expect, it } from "vitest"

import { calculatePollStatistics } from "~/features/social/model/poll-statistics"

describe("calculatePollStatistics", () => {
  it("derives the participant total from option counts", () => {
    expect(
      calculatePollStatistics([
        { id: "a", vote_count: 17 },
        { id: "b", vote_count: 3 },
        { id: "c", vote_count: 2 },
      ])
    ).toMatchObject({
      totalVotes: 22,
      options: [
        { id: "a", voteCount: 17 },
        { id: "b", voteCount: 3 },
        { id: "c", voteCount: 2 },
      ],
    })
  })

  it("uses largest remainders so rounded percentages sum to 100", () => {
    const result = calculatePollStatistics([
      { id: "a", vote_count: 1 },
      { id: "b", vote_count: 1 },
      { id: "c", vote_count: 1 },
    ])

    expect(result.options.map((option) => option.percent)).toEqual([34, 33, 33])
    expect(
      result.options.reduce((total, option) => total + option.percent, 0)
    ).toBe(100)
  })

  it("renders an empty poll without invalid or negative statistics", () => {
    expect(
      calculatePollStatistics([
        { id: "a", vote_count: Number.NaN },
        { id: "b", vote_count: -2 },
      ])
    ).toEqual({
      totalVotes: 0,
      options: [
        { id: "a", voteCount: 0, percent: 0 },
        { id: "b", voteCount: 0, percent: 0 },
      ],
    })
  })
})
