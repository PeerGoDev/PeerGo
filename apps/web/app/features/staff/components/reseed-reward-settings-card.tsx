import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Field, FieldGroup, FieldLabel } from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { Switch } from "~/components/ui/switch"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"

type Settings = components["schemas"]["ReseedRewardSettings"]
type Update = components["schemas"]["UpdateReseedRewardSettingsRequest"]

const settingsKey = ["staff", "seeding-rewards", "reseed-reward"] as const
const settingsPath = "/api/v1/admin/settings/reseed-reward"

export function ReseedRewardSettingsCard({
  csrfToken,
  canIssue,
}: {
  csrfToken: string
  canIssue: boolean
}) {
  const queryClient = useQueryClient()
  const settings = useQuery({
    queryKey: settingsKey,
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(settingsPath)
      if (!response.ok || !data)
        throw new ApiProblemError(response.status, error)
      return data
    },
    retry: false,
  })
  const update = useMutation({
    mutationFn: async (body: Update) => {
      const { data, error, response } = await apiClient.PUT(settingsPath, {
        params: { header: { "X-CSRF-Token": csrfToken } },
        body,
      })
      if (!response.ok || !data)
        throw new ApiProblemError(response.status, error)
      return data
    },
    onSuccess: (value) => queryClient.setQueryData(settingsKey, value),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>种子复活奖励</CardTitle>
        <CardDescription>
          每轮补种奖励一名率先达到有效做种时长的会员。新设置用于保存后恢复的轮次。
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {settings.isPending ? <Skeleton className="h-20 w-full" /> : null}
        {settings.data ? (
          <ReseedRewardForm
            key={settings.data.revision}
            settings={settings.data}
            canIssue={canIssue}
            pending={update.isPending}
            save={(body) => update.mutate(body)}
          />
        ) : null}
        {settings.isError || update.isError ? (
          <div className="flex flex-wrap items-center gap-2">
            <p className="text-sm text-destructive" role="alert">
              {requestErrorDescription(
                update.error ?? settings.error,
                "复活奖励设置暂时不可用。"
              )}
            </p>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                update.reset()
                void settings.refetch()
              }}
            >
              重新载入
            </Button>
          </div>
        ) : null}
        {update.isSuccess ? (
          <p role="status" className="text-sm text-success-foreground">
            复活奖励设置已保存。
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}

function ReseedRewardForm({
  settings,
  canIssue,
  pending,
  save,
}: {
  settings: Settings
  canIssue: boolean
  pending: boolean
  save: (body: Update) => void
}) {
  const id = React.useId()
  const [enabled, setEnabled] = React.useState(settings.enabled)
  const [hours, setHours] = React.useState(String(settings.required_hours))
  const [amount, setAmount] = React.useState(String(settings.magic_amount))
  const disabled = !canIssue || pending

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault()
        if (disabled) return
        save({
          expected_revision: settings.revision,
          enabled,
          required_hours: Number(hours),
          magic_amount: Number(amount),
        })
      }}
    >
      <Field orientation="horizontal">
        <Switch
          id={`${id}-enabled`}
          checked={enabled}
          onCheckedChange={setEnabled}
          disabled={disabled}
        />
        <FieldLabel htmlFor={`${id}-enabled`}>启用复活奖励</FieldLabel>
      </Field>
      <FieldGroup className="grid gap-3 sm:grid-cols-2">
        <Field>
          <FieldLabel htmlFor={`${id}-hours`}>有效做种时长（小时）</FieldLabel>
          <Input
            id={`${id}-hours`}
            type="number"
            min={1}
            max={720}
            step={1}
            required
            value={hours}
            onChange={(event) => setHours(event.target.value)}
            disabled={disabled}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor={`${id}-amount`}>奖励金额（魔力值）</FieldLabel>
          <Input
            id={`${id}-amount`}
            type="number"
            min={1}
            max={1_000_000}
            step={1}
            required
            value={amount}
            onChange={(event) => setAmount(event.target.value)}
            disabled={disabled}
          />
        </Field>
      </FieldGroup>
      {canIssue ? (
        <Button className="self-start" type="submit" disabled={pending}>
          {pending ? "正在保存…" : "保存复活奖励设置"}
        </Button>
      ) : null}
    </form>
  )
}
