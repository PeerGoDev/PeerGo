import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  MessageSquareTextIcon,
  PlusIcon,
  RefreshCwIcon,
  Settings2Icon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import { SocialPostCard } from "~/features/social/components/social-post-card"
import {
  type ManagedSocialBoard,
  managedSocialBoardsQueryOptions,
  managedSocialPostsQueryOptions,
  useCreateManagedSocialBoard,
  useModerateSocialPost,
  useUpdateManagedSocialBoard,
} from "~/features/staff/api/social-administration.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"
import { ApiProblemError } from "~/shared/api/problem"

type CapabilityList = components["schemas"]["CapabilityList"]
type SocialPost = components["schemas"]["SocialPost"]

export function StaffSocialAdministrationPage() {
  return (
    <StaffAccessGate
      requiredAction="social.board.manage.read"
      pageHeader={{
        title: "动态圈管理",
        description:
          "自定义板块、发布权限与排序，并统一管理动态的归属、置顶、精华和可见性。",
        descriptionClassName: "mt-3",
      }}
    >
      {({ session, capabilities }) => (
        <SocialAdministrationContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function SocialAdministrationContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const [section, setSection] = React.useState<"boards" | "posts">("boards")
  const boards = useQuery(managedSocialBoardsQueryOptions)
  return (
    <StaffPageFrame className="gap-4">
      <header className="flex items-center justify-between gap-4">
        <div>
          <h1 className="font-heading text-xl font-semibold">动态圈管理</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            轻社区信息流与板块配置
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void boards.refetch()}
          disabled={boards.isFetching}
        >
          <RefreshCwIcon data-icon="inline-start" />
          刷新
        </Button>
      </header>
      <div className="grid h-10 grid-cols-2 rounded-lg bg-muted p-1 text-sm">
        <button
          type="button"
          onClick={() => setSection("boards")}
          className={
            section === "boards"
              ? "rounded-md bg-background font-medium shadow-sm"
              : "rounded-md text-muted-foreground"
          }
        >
          <Settings2Icon className="mr-2 inline size-4" />
          板块设置
        </button>
        <button
          type="button"
          onClick={() => setSection("posts")}
          className={
            section === "posts"
              ? "rounded-md bg-background font-medium shadow-sm"
              : "rounded-md text-muted-foreground"
          }
        >
          <MessageSquareTextIcon className="mr-2 inline size-4" />
          内容管理
        </button>
      </div>
      {boards.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>动态圈配置暂时不可用</AlertTitle>
          <AlertDescription>请稍后重试。</AlertDescription>
        </Alert>
      ) : section === "boards" ? (
        <BoardsSection
          boards={boards.data ?? []}
          csrfToken={csrfToken}
          capabilities={capabilities}
        />
      ) : (
        <PostsSection
          boards={boards.data ?? []}
          csrfToken={csrfToken}
          capabilities={capabilities}
        />
      )}
    </StaffPageFrame>
  )
}

function BoardsSection({
  boards,
  csrfToken,
  capabilities,
}: {
  boards: ManagedSocialBoard[]
  csrfToken: string
  capabilities: CapabilityList
}) {
  const [editing, setEditing] = React.useState<ManagedSocialBoard | "new">()
  const canCreate = hasCapability(capabilities, "social.board.create")
  const canUpdate = hasCapability(capabilities, "social.board.update")
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          停用板块不会删除历史动态；关闭“成员可发布”可用于站务公告板块。
        </p>
        {canCreate ? (
          <Button size="sm" onClick={() => setEditing("new")}>
            <PlusIcon data-icon="inline-start" />
            新建板块
          </Button>
        ) : null}
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        {boards.map((board) => (
          <div key={board.id} className="rounded-lg border bg-card p-4">
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="flex items-center gap-2">
                  <h2 className="font-medium">{board.name}</h2>
                  <Badge variant={board.enabled ? "secondary" : "outline"}>
                    {board.enabled ? "启用" : "停用"}
                  </Badge>
                </div>
                <p className="mt-1 text-sm text-muted-foreground">
                  {board.description || "暂无说明"}
                </p>
              </div>
              {canUpdate ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setEditing(board)}
                >
                  编辑
                </Button>
              ) : null}
            </div>
            <div className="mt-4 grid grid-cols-3 gap-2 text-xs text-muted-foreground">
              <span>标识 {board.id}</span>
              <span>排序 {board.display_order}</span>
              <span>{board.post_count} 条动态</span>
            </div>
            <div className="mt-3 flex gap-2">
              <Badge variant="outline">{board.tone}</Badge>
              <Badge variant="outline">
                {board.allow_member_posts ? "成员可发布" : "仅管理发布"}
              </Badge>
            </div>
          </div>
        ))}
      </div>
      {editing ? (
        <BoardEditor
          key={editing === "new" ? "new" : `${editing.id}:${editing.version}`}
          board={editing === "new" ? undefined : editing}
          csrfToken={csrfToken}
          onClose={() => setEditing(undefined)}
        />
      ) : null}
    </div>
  )
}

const icons = [
  "messages-square",
  "coffee",
  "folder-open",
  "clapperboard",
  "megaphone",
  "sparkles",
  "gamepad-2",
  "circle-help",
] as const
const tones = ["coral", "green", "blue", "violet", "amber", "slate"] as const

function BoardEditor({
  board,
  csrfToken,
  onClose,
}: {
  board?: ManagedSocialBoard
  csrfToken: string
  onClose: () => void
}) {
  const create = useCreateManagedSocialBoard()
  const update = useUpdateManagedSocialBoard()
  const [id, setId] = React.useState(board?.id ?? "")
  const [name, setName] = React.useState(board?.name ?? "")
  const [description, setDescription] = React.useState(board?.description ?? "")
  const [icon, setIcon] = React.useState<(typeof icons)[number]>(
    board?.icon ?? "messages-square"
  )
  const [tone, setTone] = React.useState<(typeof tones)[number]>(
    board?.tone ?? "coral"
  )
  const [order, setOrder] = React.useState(String(board?.display_order ?? 100))
  const [enabled, setEnabled] = React.useState(board?.enabled ?? true)
  const [allowPosts, setAllowPosts] = React.useState(
    board?.allow_member_posts ?? true
  )
  const [reason, setReason] = React.useState("")
  const pending = create.isPending || update.isPending
  const error = create.error ?? update.error
  async function save() {
    const base = {
      name: name.trim(),
      description: description.trim(),
      icon,
      tone,
      display_order: Number(order),
      enabled,
      allow_member_posts: allowPosts,
      reason: reason.trim(),
    }
    if (board)
      await update.mutateAsync({
        csrfToken,
        boardId: board.id,
        body: { ...base, expected_version: board.version },
      })
    else
      await create.mutateAsync({ csrfToken, body: { ...base, id: id.trim() } })
    onClose()
  }
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{board ? "编辑板块" : "新建板块"}</DialogTitle>
          <DialogDescription>
            板块标识创建后不可修改；每次变更都必须填写审计理由。
          </DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="social-board-id">稳定标识</FieldLabel>
              <Input
                id="social-board-id"
                value={id}
                onChange={(e) => setId(e.target.value)}
                disabled={Boolean(board)}
                placeholder="games"
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="social-board-name">名称</FieldLabel>
              <Input
                id="social-board-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={40}
              />
            </Field>
          </div>
          <Field>
            <FieldLabel htmlFor="social-board-description">说明</FieldLabel>
            <Input
              id="social-board-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              maxLength={120}
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-3">
            <Field>
              <FieldLabel>图标</FieldLabel>
              <Select
                items={icons.map((value) => ({ label: value, value }))}
                value={icon}
                onValueChange={(value) => value && setIcon(value)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {icons.map((value) => (
                      <SelectItem key={value} value={value}>
                        {value}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel>色调</FieldLabel>
              <Select
                items={tones.map((value) => ({ label: value, value }))}
                value={tone}
                onValueChange={(value) => value && setTone(value)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {tones.map((value) => (
                      <SelectItem key={value} value={value}>
                        {value}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="social-board-order">排序</FieldLabel>
              <Input
                id="social-board-order"
                type="number"
                min={0}
                value={order}
                onChange={(e) => setOrder(e.target.value)}
              />
            </Field>
          </div>
          <Field orientation="horizontal">
            <div>
              <FieldLabel>启用板块</FieldLabel>
              <FieldDescription>停用后从前台隐藏。</FieldDescription>
            </div>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </Field>
          <Field orientation="horizontal">
            <div>
              <FieldLabel>允许成员发布</FieldLabel>
              <FieldDescription>关闭后仅后台人员可维护归属。</FieldDescription>
            </div>
            <Switch checked={allowPosts} onCheckedChange={setAllowPosts} />
          </Field>
          <Field>
            <FieldLabel htmlFor="social-board-reason">变更理由</FieldLabel>
            <Textarea
              id="social-board-reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              minLength={10}
              maxLength={500}
              placeholder="至少 10 个字符"
            />
          </Field>
        </FieldGroup>
        {error ? (
          <Alert variant="destructive">
            <AlertTitle>保存失败</AlertTitle>
            <AlertDescription>
              {error instanceof ApiProblemError
                ? error.message
                : "请稍后重试。"}
            </AlertDescription>
          </Alert>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button
            onClick={() => void save()}
            disabled={
              pending || !id.trim() || !name.trim() || reason.trim().length < 10
            }
          >
            {pending ? "保存中…" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PostsSection({
  boards,
  csrfToken,
  capabilities,
}: {
  boards: ManagedSocialBoard[]
  csrfToken: string
  capabilities: CapabilityList
}) {
  const [boardId, setBoardId] = React.useState("")
  const [offset, setOffset] = React.useState(0)
  const [editing, setEditing] = React.useState<SocialPost>()
  const posts = useQuery(managedSocialPostsQueryOptions(boardId, offset))
  const canModerate = hasCapability(capabilities, "social.post.moderate")
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <Select
          items={[
            { label: "全部板块", value: "all" },
            ...boards.map((b) => ({ label: b.name, value: b.id })),
          ]}
          value={boardId || "all"}
          onValueChange={(value) => {
            setBoardId(value === "all" ? "" : (value ?? ""))
            setOffset(0)
          }}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="all">全部板块</SelectItem>
              {boards.map((board) => (
                <SelectItem key={board.id} value={board.id}>
                  {board.name}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <span className="text-sm text-muted-foreground">
          {posts.data?.total ?? 0} 条
        </span>
      </div>
      {posts.isError ? (
        <Alert variant="destructive">
          <AlertTitle>动态列表不可用</AlertTitle>
        </Alert>
      ) : (
        <div className="space-y-3">
          {posts.data?.items.map((post) => (
            <div key={post.id} className="space-y-2">
              <SocialPostCard post={post} linkToDetail={false} compact />
              <div className="flex justify-end">
                {canModerate ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setEditing(post)}
                  >
                    管理状态
                  </Button>
                ) : null}
              </div>
            </div>
          ))}
        </div>
      )}
      <div className="flex justify-center gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={offset === 0}
          onClick={() => setOffset(Math.max(0, offset - 20))}
        >
          上一页
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={!posts.data || offset + 20 >= posts.data.total}
          onClick={() => setOffset(offset + 20)}
        >
          下一页
        </Button>
      </div>
      {editing ? (
        <PostModerationEditor
          post={editing}
          boards={boards}
          csrfToken={csrfToken}
          onClose={() => setEditing(undefined)}
        />
      ) : null}
    </div>
  )
}

function PostModerationEditor({
  post,
  boards,
  csrfToken,
  onClose,
}: {
  post: SocialPost
  boards: ManagedSocialBoard[]
  csrfToken: string
  onClose: () => void
}) {
  const mutate = useModerateSocialPost()
  const [boardId, setBoardId] = React.useState(post.board.id)
  const [pinned, setPinned] = React.useState(post.pinned)
  const [featured, setFeatured] = React.useState(post.featured)
  const [hidden, setHidden] = React.useState(post.hidden ?? false)
  const [reason, setReason] = React.useState("")
  async function save() {
    await mutate.mutateAsync({
      csrfToken,
      postId: post.id,
      body: {
        board_id: boardId,
        pinned,
        featured,
        hidden,
        expected_version: post.version,
        reason: reason.trim(),
      },
    })
    onClose()
  }
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>管理动态</DialogTitle>
          <DialogDescription>
            可移动板块、调整推荐状态或隐藏违规内容。
          </DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel>所属板块</FieldLabel>
            <Select
              items={boards.map((b) => ({ label: b.name, value: b.id }))}
              value={boardId}
              onValueChange={(value) => value && setBoardId(value)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {boards.map((board) => (
                    <SelectItem key={board.id} value={board.id}>
                      {board.name}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Field orientation="horizontal">
            <FieldLabel>置顶</FieldLabel>
            <Switch checked={pinned} onCheckedChange={setPinned} />
          </Field>
          <Field orientation="horizontal">
            <FieldLabel>精华</FieldLabel>
            <Switch checked={featured} onCheckedChange={setFeatured} />
          </Field>
          <Field orientation="horizontal">
            <div>
              <FieldLabel>隐藏动态</FieldLabel>
              <FieldDescription>
                隐藏后前台与评论入口立即不可见。
              </FieldDescription>
            </div>
            <Switch checked={hidden} onCheckedChange={setHidden} />
          </Field>
          <Field>
            <FieldLabel>管理理由</FieldLabel>
            <Textarea
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              minLength={10}
              maxLength={500}
            />
          </Field>
        </FieldGroup>
        {mutate.error ? (
          <Alert variant="destructive">
            <AlertTitle>保存失败</AlertTitle>
            <AlertDescription>
              {mutate.error instanceof ApiProblemError
                ? mutate.error.message
                : "请稍后重试。"}
            </AlertDescription>
          </Alert>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button
            onClick={() => void save()}
            disabled={mutate.isPending || reason.trim().length < 10}
          >
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
