type PollOptionCount = {
  id: string
  vote_count: number
}

export type PollOptionStatistic = {
  id: string
  voteCount: number
  percent: number
}

export type PollStatistics = {
  totalVotes: number
  options: PollOptionStatistic[]
}

function normalizedVoteCount(value: number) {
  return Number.isFinite(value) ? Math.max(0, Math.trunc(value)) : 0
}

// Round all options as a group so the displayed percentages always add up to
// exactly 100%. Independent Math.round calls can produce totals such as 99%
// or 101%, which makes an otherwise-correct poll look inconsistent.
export function calculatePollStatistics(
  options: readonly PollOptionCount[]
): PollStatistics {
  const voteCounts = options.map((option) =>
    normalizedVoteCount(option.vote_count)
  )
  const totalVotes = voteCounts.reduce((total, count) => total + count, 0)

  if (totalVotes === 0) {
    return {
      totalVotes,
      options: options.map((option) => ({
        id: option.id,
        voteCount: 0,
        percent: 0,
      })),
    }
  }

  const exactPercentages = voteCounts.map((count) => (count * 100) / totalVotes)
  const percentages = exactPercentages.map(Math.floor)
  let undistributed =
    100 - percentages.reduce((total, percent) => total + percent, 0)

  const remainderOrder = exactPercentages
    .map((percent, index) => ({
      index,
      remainder: percent - percentages[index],
    }))
    .sort(
      (left, right) =>
        right.remainder - left.remainder || left.index - right.index
    )

  for (const { index } of remainderOrder) {
    if (undistributed === 0) break
    percentages[index] += 1
    undistributed -= 1
  }

  return {
    totalVotes,
    options: options.map((option, index) => ({
      id: option.id,
      voteCount: voteCounts[index],
      percent: percentages[index],
    })),
  }
}
