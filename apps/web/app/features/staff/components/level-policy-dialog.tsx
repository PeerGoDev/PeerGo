import * as React from "react"
import {
  CircleAlertIcon,
  PlusIcon,
  Settings2Icon,
  Trash2Icon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "~/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Spinner } from "~/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { Textarea } from "~/components/ui/textarea"
import type { components } from "~/generated/api"
import {
  type LevelPolicyList,
  useIssueLevelPolicy,
} from "~/features/staff/api/seeding-reward-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"

type LevelRuleInput = components["schemas"]["LevelPolicyRuleInput"]

type EditableLevel = {
  minimumExperience: string
  karmaBonusPercent: string
  seedingCountBonus: string
}

export function LevelPolicyDialog({
  policies,
  csrfToken,
  disabled,
}: {
  policies: LevelPolicyList
  csrfToken: string
  disabled: boolean
}) {
  const [open, setOpen] = React.useState(false)
  const latest = policies.items.reduce((current, item) =>
    item.sequence > current.sequence ? item : current
  )
  const [levels, setLevels] = React.useState<EditableLevel[]>(() =>
    latest.levels.map(toEditableLevel)
  )
  const [effectiveAt, setEffectiveAt] = React.useState(() =>
    defaultEffectiveAt(policies)
  )
  const [reason, setReason] = React.useState("")
  const requestId = React.useRef<string | undefined>(undefined)
  const mutation = useIssueLevelPolicy()
  const validation = validateLevelPolicy(levels, effectiveAt, reason, policies)

  function updateLevel(index: number, patch: Partial<EditableLevel>) {
    setLevels((current) =>
      current.map((level, position) =>
        position === index ? { ...level, ...patch } : level
      )
    )
    requestId.current = undefined
    mutation.reset()
  }

  function appendLevel() {
    setLevels((current) => {
      const previous = current.at(-1)
      const previousMinimum = previous?.minimumExperience ?? "0"
      const nextMinimum = /^\d+$/.test(previousMinimum)
        ? (BigInt(previousMinimum) * 2n || 1000n).toString()
        : ""
      return [
        ...current,
        {
          minimumExperience: nextMinimum,
          karmaBonusPercent: previous?.karmaBonusPercent ?? "0",
          seedingCountBonus: previous?.seedingCountBonus ?? "0",
        },
      ]
    })
    requestId.current = undefined
    mutation.reset()
  }

  async function submit() {
    if (!validation.valid) return
    requestId.current ??= globalThis.crypto.randomUUID()
    await mutation.mutateAsync({
      csrfToken,
      idempotencyKey: requestId.current,
      body: {
        expected_sequence: latest.sequence,
        effective_at: new Date(effectiveAt).toISOString(),
        levels: levels.map(
          (level, index): LevelRuleInput => ({
            level: index + 1,
            minimum_experience: level.minimumExperience,
            karma_bonus_bps: percentToBasisPoints(level.karmaBonusPercent) ?? 0,
            seeding_count_bonus: Number(level.seedingCountBonus),
          })
        ),
        reason: reason.trim(),
      },
    })
    setOpen(false)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !mutation.isPending && setOpen(next)}
    >
      <DialogTrigger
        render={<Button type="button" size="sm" disabled={disabled} />}
      >
        <Settings2Icon data-icon="inline-start" />
        调整等级规则
      </DialogTrigger>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>签发等级规则版本</DialogTitle>
          <DialogDescription>
            从最新完整版本复制后调整。生效时会按用户现有经验统一重算等级，不改动任何经验账本。
          </DialogDescription>
        </DialogHeader>

        {mutation.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>等级规则未签发</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(
                mutation.error,
                "请重新加载页面并核对生效时间和等级门槛。"
              )}
            </AlertDescription>
          </Alert>
        ) : null}

        <Alert>
          <Settings2Icon />
          <AlertTitle>只重算等级，不补发升级奖励</AlertTitle>
          <AlertDescription>
            调整门槛可能使部分用户升降级；做种权益随新等级切换，但不会伪造经验或重复发放旧站升级奖励。
          </AlertDescription>
        </Alert>

        <FieldGroup>
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader className="bg-muted/50">
                <TableRow>
                  <TableHead>等级</TableHead>
                  <TableHead className="min-w-48">最低经验</TableHead>
                  <TableHead className="min-w-40">做种魔力加成（%）</TableHead>
                  <TableHead className="min-w-40">额外计奖种子</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {levels.map((level, index) => (
                  <TableRow key={index}>
                    <TableCell className="font-medium">
                      Lv.{index + 1}
                    </TableCell>
                    <TableCell>
                      <Field data-invalid={validation.invalidRows.has(index)}>
                        <FieldLabel
                          className="sr-only"
                          htmlFor={`level-${index}-experience`}
                        >
                          Lv.{index + 1} 最低经验
                        </FieldLabel>
                        <Input
                          id={`level-${index}-experience`}
                          inputMode="numeric"
                          value={level.minimumExperience}
                          disabled={index === 0}
                          aria-invalid={validation.invalidRows.has(index)}
                          onChange={(event) =>
                            updateLevel(index, {
                              minimumExperience: event.target.value,
                            })
                          }
                        />
                      </Field>
                    </TableCell>
                    <TableCell>
                      <Field data-invalid={validation.invalidRows.has(index)}>
                        <FieldLabel
                          className="sr-only"
                          htmlFor={`level-${index}-karma`}
                        >
                          Lv.{index + 1} 做种魔力加成
                        </FieldLabel>
                        <Input
                          id={`level-${index}-karma`}
                          inputMode="decimal"
                          value={level.karmaBonusPercent}
                          aria-invalid={validation.invalidRows.has(index)}
                          onChange={(event) =>
                            updateLevel(index, {
                              karmaBonusPercent: event.target.value,
                            })
                          }
                        />
                      </Field>
                    </TableCell>
                    <TableCell>
                      <Field data-invalid={validation.invalidRows.has(index)}>
                        <FieldLabel
                          className="sr-only"
                          htmlFor={`level-${index}-seeding`}
                        >
                          Lv.{index + 1} 额外计奖种子
                        </FieldLabel>
                        <Input
                          id={`level-${index}-seeding`}
                          inputMode="numeric"
                          value={level.seedingCountBonus}
                          aria-invalid={validation.invalidRows.has(index)}
                          onChange={(event) =>
                            updateLevel(index, {
                              seedingCountBonus: event.target.value,
                            })
                          }
                        />
                      </Field>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`删除 Lv.${index + 1}`}
                        disabled={
                          index !== levels.length - 1 || levels.length <= 2
                        }
                        onClick={() => {
                          setLevels((current) => current.slice(0, -1))
                          requestId.current = undefined
                          mutation.reset()
                        }}
                      >
                        <Trash2Icon data-icon="icon" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          <Button
            type="button"
            variant="outline"
            className="w-fit"
            disabled={levels.length >= 99}
            onClick={appendLevel}
          >
            <PlusIcon data-icon="inline-start" />
            增加一级
          </Button>

          <Field data-invalid={validation.timeError !== null}>
            <FieldLabel htmlFor="level-policy-effective-at">
              生效时间
            </FieldLabel>
            <Input
              id="level-policy-effective-at"
              type="datetime-local"
              value={effectiveAt}
              aria-invalid={validation.timeError !== null}
              onChange={(event) => {
                setEffectiveAt(event.target.value)
                requestId.current = undefined
                mutation.reset()
              }}
            />
            <FieldDescription>
              必须为整点，且不早于服务器给出的最早时间；已有定时版本时还需排在其后。
            </FieldDescription>
            {validation.timeError ? (
              <FieldError>{validation.timeError}</FieldError>
            ) : null}
          </Field>

          <Field data-invalid={validation.reasonError !== null}>
            <FieldLabel htmlFor="level-policy-reason">修改说明</FieldLabel>
            <Textarea
              id="level-policy-reason"
              rows={3}
              minLength={10}
              maxLength={1000}
              value={reason}
              aria-invalid={validation.reasonError !== null}
              onChange={(event) => {
                setReason(event.target.value)
                requestId.current = undefined
                mutation.reset()
              }}
            />
            <FieldDescription>
              {Array.from(reason.trim()).length} / 1000，至少 10 个字符。
            </FieldDescription>
            {validation.reasonError ? (
              <FieldError>{validation.reasonError}</FieldError>
            ) : null}
          </Field>
        </FieldGroup>

        {!validation.valid && validation.rowError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>等级阶梯需要修正</AlertTitle>
            <AlertDescription>{validation.rowError}</AlertDescription>
          </Alert>
        ) : null}

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={mutation.isPending}
            onClick={() => setOpen(false)}
          >
            取消
          </Button>
          <Button
            type="button"
            disabled={!validation.valid || mutation.isPending}
            onClick={() => void submit()}
          >
            {mutation.isPending ? <Spinner data-icon="inline-start" /> : null}
            签发定时版本
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function toEditableLevel(
  level: components["schemas"]["LevelDefinition"]
): EditableLevel {
  return {
    minimumExperience: level.minimum_experience,
    karmaBonusPercent: basisPointsToPercent(level.karma_bonus_bps),
    seedingCountBonus: String(level.seeding_count_bonus),
  }
}

function validateLevelPolicy(
  levels: EditableLevel[],
  effectiveAt: string,
  reason: string,
  policies: LevelPolicyList
) {
  const invalidRows = new Set<number>()
  let previousMinimum = -1n
  let previousKarma = -1
  let previousSeeding = -1
  for (const [index, level] of levels.entries()) {
    const minimum = /^\d{1,18}$/.test(level.minimumExperience)
      ? BigInt(level.minimumExperience)
      : null
    const karma = percentToBasisPoints(level.karmaBonusPercent)
    const seeding = /^\d{1,4}$/.test(level.seedingCountBonus)
      ? Number(level.seedingCountBonus)
      : null
    if (
      minimum === null ||
      (index === 0 ? minimum !== 0n : minimum <= previousMinimum) ||
      karma === null ||
      karma < previousKarma ||
      seeding === null ||
      seeding > 1000 ||
      seeding < previousSeeding
    ) {
      invalidRows.add(index)
    }
    if (minimum !== null) previousMinimum = minimum
    if (karma !== null) previousKarma = karma
    if (seeding !== null) previousSeeding = seeding
  }

  const effectiveDate = new Date(effectiveAt)
  const minimumDate = new Date(policies.minimum_effective_at)
  const lastScheduled = policies.items.reduce(
    (latest, item) => Math.max(latest, new Date(item.effective_at).getTime()),
    0
  )
  let timeError: string | null = null
  if (
    Number.isNaN(effectiveDate.getTime()) ||
    effectiveDate.getUTCMinutes() !== 0 ||
    effectiveDate.getUTCSeconds() !== 0
  ) {
    timeError = "生效时间必须选择整点。"
  } else if (
    effectiveDate < minimumDate ||
    effectiveDate.getTime() <= lastScheduled
  ) {
    timeError = "生效时间过早，或没有排在现有版本之后。"
  }

  const reasonLength = Array.from(reason.trim()).length
  const reasonError =
    reasonLength > 0 && reasonLength < 10
      ? "修改说明至少需要 10 个字符。"
      : reasonLength > 1000
        ? "修改说明不能超过 1000 个字符。"
        : null
  const rowError = invalidRows.size
    ? "经验门槛必须从 0 开始并逐级增加；做种加成和额外计奖数量不能在更高等级降低。"
    : null
  return {
    invalidRows,
    timeError,
    reasonError,
    rowError,
    valid:
      levels.length >= 2 &&
      levels.length <= 99 &&
      invalidRows.size === 0 &&
      timeError === null &&
      reasonLength >= 10 &&
      reasonLength <= 1000,
  }
}

function percentToBasisPoints(value: string) {
  const match = /^(\d{1,3})(?:\.(\d{1,2}))?$/.exec(value.trim())
  if (!match) return null
  const whole = Number(match[1])
  const fraction = Number((match[2] ?? "").padEnd(2, "0"))
  const result = whole * 100 + fraction
  return result <= 10_000 ? result : null
}

function basisPointsToPercent(value: number) {
  const whole = Math.floor(value / 100)
  const fraction = String(value % 100)
    .padStart(2, "0")
    .replace(/0+$/, "")
  return fraction ? `${whole}.${fraction}` : String(whole)
}

function defaultEffectiveAt(policies: LevelPolicyList) {
  const minimum = new Date(policies.minimum_effective_at).getTime()
  const latest = policies.items.reduce(
    (value, item) => Math.max(value, new Date(item.effective_at).getTime()),
    0
  )
  const selected = new Date(Math.max(minimum, latest + 60 * 60 * 1000))
  const local = new Date(
    selected.getTime() - selected.getTimezoneOffset() * 60_000
  )
  return local.toISOString().slice(0, 16)
}
