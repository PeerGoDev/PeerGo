import { useMemo, useState } from "react"
import { ArrowUpRightIcon, SearchIcon, Settings2Icon } from "lucide-react"
import { Link } from "react-router"

import { Badge } from "~/components/ui/badge"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Field, FieldLabel } from "~/components/ui/field"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "~/components/ui/input-group"
import { useStaffPendingOverview } from "~/features/staff/api/staff-pending-overview.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffPageHeader } from "~/features/staff/components/staff-page-header"
import { hasCapability } from "~/features/staff/model/capability"
import {
  staffSettingsNavigationGroups,
  type StaffSettingsNavigationGroup,
} from "~/features/staff/model/staff-settings-navigation"
import type { components } from "~/generated/api"
import { cn } from "~/lib/utils"

type CapabilityList = components["schemas"]["CapabilityList"]

export function StaffSettingsOverviewPage() {
  return (
    <StaffAccessGate
      pageHeader={{
        title: "设置中心",
        description: "按业务域查找设置，避免在一个长页面里反复翻找。",
      }}
    >
      {({ capabilities }) => (
        <SettingsOverviewContent capabilities={capabilities} />
      )}
    </StaffAccessGate>
  )
}

function SettingsOverviewContent({
  capabilities,
}: {
  capabilities: CapabilityList
}) {
  const [query, setQuery] = useState("")
  const pending = useStaffPendingOverview(capabilities)
  const availableGroups = useMemo(
    () =>
      staffSettingsNavigationGroups
        .map((group) => ({
          ...group,
          items: group.items.filter((item) =>
            hasCapability(capabilities, item.action)
          ),
        }))
        .filter((group) => group.items.length > 0),
    [capabilities]
  )
  const normalizedQuery = query.trim().toLocaleLowerCase()
  const visibleGroups = availableGroups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) =>
        `${item.label} ${item.description} ${group.label}`
          .toLocaleLowerCase()
          .includes(normalizedQuery)
      ),
    }))
    .filter((group) => group.items.length > 0)
  const availableCount = availableGroups.reduce(
    (total, group) => total + group.items.length,
    0
  )

  return (
    <StaffPageFrame className="gap-6">
      <StaffPageHeader
        title="设置中心"
        description="设置已按职责拆开；进入对应模块后只处理同一类配置。"
        icon={Settings2Icon}
        meta={<Badge variant="secondary">{availableCount} 个可用模块</Badge>}
      />

      <Field className="max-w-xl">
        <FieldLabel htmlFor="staff-settings-search" className="sr-only">
          搜索设置
        </FieldLabel>
        <InputGroup className="h-10 bg-background">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput
            id="staff-settings-search"
            value={query}
            onChange={(event) => setQuery(event.currentTarget.value)}
            placeholder="搜索站点名称、Tracker、魔力值……"
            autoComplete="off"
          />
        </InputGroup>
      </Field>

      {visibleGroups.length > 0 ? (
        <div className="grid items-start gap-4 xl:grid-cols-3">
          {visibleGroups.map((group) => (
            <SettingsGroupCard
              key={group.id}
              group={group}
              badges={pending.byRoute}
            />
          ))}
        </div>
      ) : (
        <Empty className="min-h-72 border bg-card">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchIcon />
            </EmptyMedia>
            <EmptyTitle>没有匹配的设置</EmptyTitle>
            <EmptyDescription>
              请换一个关键词；这里只展示当前管理员有权查看的模块。
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
    </StaffPageFrame>
  )
}

function SettingsGroupCard({
  group,
  badges,
}: {
  group: StaffSettingsNavigationGroup
  badges: Record<string, number>
}) {
  const GroupIcon = group.icon

  return (
    <Card className="gap-0 py-0">
      <CardHeader className="border-b p-5">
        <CardTitle className="flex items-center gap-2">
          <span className="flex size-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <GroupIcon className="size-[18px]" />
          </span>
          {group.label}
        </CardTitle>
        <CardDescription className="pr-1">{group.description}</CardDescription>
        <CardAction>
          <Badge variant="outline">{group.items.length}</Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="divide-y p-0">
        {group.items.map((item) => {
          const ItemIcon = item.icon
          const pendingCount = badges[item.to] ?? 0
          return (
            <Link
              key={item.to}
              to={item.to}
              prefetch="intent"
              className={cn(
                "group flex min-h-20 items-center gap-3 px-5 py-3 transition-colors outline-none",
                "hover:bg-muted/60 focus-visible:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
              )}
              aria-label={`打开${item.label}`}
            >
              <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-background text-muted-foreground transition-colors group-hover:text-foreground">
                <ItemIcon className="size-[18px]" />
              </span>
              <span className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="flex items-center gap-2 font-medium">
                  {item.label}
                  {pendingCount > 0 ? (
                    <Badge variant="destructive">{pendingCount} 项待处理</Badge>
                  ) : null}
                </span>
                <span className="text-sm leading-5 text-muted-foreground">
                  {item.description}
                </span>
              </span>
              <ArrowUpRightIcon className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5 group-hover:text-foreground" />
            </Link>
          )
        })}
      </CardContent>
    </Card>
  )
}
