import { webcrypto } from "node:crypto"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { verifyLotteryDraw } from "./lottery-verification"

const round = {
  id: "6b2f3d55-8c34-4a59-9a0b-6f3f3c1a2b10",
  seed: "3031323334353637383961626364656630313233343536373839616263646566",
  seed_sha256:
    "3eb1bd439947eb762998e566ccc2e099c791118b2f40579cc4f7da2b5061b7f9",
  tickets_sold: 50,
  tiers: [{ label: "测试奖级", winners: 14, share_bps: 10000 }],
  winners: [1, 7, 4, 22, 20, 47, 44, 12, 13, 14, 38, 26, 33, 11].map(
    (number, index) => ({
      draw_rank: index + 1,
      tier_index: 0,
      tier_label: "测试奖级",
      ticket_number: number,
      user_id: "winner",
      username: "winner",
      prize: 100,
    })
  ),
}

describe("verifyLotteryDraw", () => {
  beforeEach(() => vi.stubGlobal("crypto", webcrypto))
  afterEach(() => vi.unstubAllGlobals())

  it("reproduces the Core draw for the published seed", async () => {
    await expect(verifyLotteryDraw(round)).resolves.toBe(true)
  })

  it("rejects a mismatched commitment and incomplete winning tickets", async () => {
    await expect(
      verifyLotteryDraw({ ...round, seed_sha256: "0".repeat(64) })
    ).resolves.toBe(false)
    await expect(
      verifyLotteryDraw({ ...round, winners: round.winners.slice(1) })
    ).resolves.toBe(false)
  })

  it("rejects a substituted winning ticket", async () => {
    const winners = [
      { ...round.winners[0], ticket_number: 2 },
      ...round.winners.slice(1),
    ]
    await expect(verifyLotteryDraw({ ...round, winners })).resolves.toBe(false)
  })
})
