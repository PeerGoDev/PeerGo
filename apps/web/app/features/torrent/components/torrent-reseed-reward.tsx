import type { components } from "~/generated/api"
import { formatInteger } from "~/shared/formatters/integer"

type ReseedReward = components["schemas"]["ReseedRewardOffer"]

const rewardStatusMessage: Record<ReseedReward["status"], string> = {
  upcoming: "发起补种后，奖励条件以恢复做种时的设置为准。",
  active: "本轮率先达到时长的一名补种者可领取。",
  awarded: "本轮奖励已发放。",
  ended: "本轮做种已结束。",
}

export function TorrentReseedReward({ reward }: { reward: ReseedReward }) {
  return (
    <div className="flex flex-col gap-1 rounded-lg border bg-muted/35 px-3 py-2.5">
      <p className="flex flex-wrap items-baseline gap-x-2 text-sm font-semibold">
        <span>
          {reward.status === "upcoming" ? "预计复活奖励" : "复活奖励"}
        </span>
        <span className="text-primary tabular-nums">
          {formatInteger(reward.magic_amount)} 魔力值
        </span>
      </p>
      <p className="text-xs text-muted-foreground">
        累计有效做种满 {formatInteger(reward.required_hours)} 小时。
        {rewardStatusMessage[reward.status]}
      </p>
    </div>
  )
}
