import type { LotteryRound } from "~/features/economy/api/magic-store.queries"

type LotteryDraw = Pick<
  LotteryRound,
  "id" | "seed" | "seed_sha256" | "tickets_sold" | "tiers" | "winners"
>

export async function verifyLotteryDraw(round: LotteryDraw) {
  const roundHex = round.id.replaceAll("-", "")
  if (
    !round.seed ||
    !/^[0-9a-f]{64}$/.test(round.seed) ||
    !/^[0-9a-f]{32}$/.test(roundHex)
  ) {
    return false
  }

  const seed = hexBytes(round.seed)
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", seed))
  const digestHex = Array.from(digest, (byte) =>
    byte.toString(16).padStart(2, "0")
  ).join("")
  if (digestHex !== round.seed_sha256) return false

  const count = Math.min(
    round.tickets_sold,
    round.tiers.reduce((sum, tier) => sum + tier.winners, 0)
  )
  if (count !== round.winners.length) return false

  const key = await crypto.subtle.importKey(
    "raw",
    seed,
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"]
  )
  const message = new Uint8Array(24)
  message.set(hexBytes(roundHex))
  const counter = new DataView(message.buffer)
  const swapped = new Map<number, number>()
  const maxUint64 = (1n << 64n) - 1n

  // Reproduce Core's partial Fisher–Yates shuffle in draw rank order.
  for (let step = 0; step < count; step++) {
    const bound = BigInt(round.tickets_sold - step)
    const limit = maxUint64 - (maxUint64 % bound)
    counter.setUint32(16, step)
    let value: bigint
    let attempt = 0
    do {
      counter.setUint32(20, attempt++)
      const signature = await crypto.subtle.sign("HMAC", key, message)
      value = new DataView(signature).getBigUint64(0)
    } while (value >= limit)

    const target = step + Number(value % bound)
    const chosen = swapped.get(target) ?? target + 1
    swapped.set(target, swapped.get(step) ?? step + 1)
    swapped.set(step, chosen)

    const winner = round.winners[step]
    if (winner.draw_rank !== step + 1 || winner.ticket_number !== chosen) {
      return false
    }
  }
  return true
}

function hexBytes(value: string) {
  return Uint8Array.from(value.match(/../g) ?? [], (byte) => parseInt(byte, 16))
}
