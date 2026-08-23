import {
  queryOptions,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type SocialPost = components["schemas"]["SocialPost"]
export type SocialPostPage = components["schemas"]["SocialPostPage"]
export type SocialPostSort = components["schemas"]["SocialPostSort"]
export type SocialFeedKind = components["schemas"]["SocialFeedKind"]
export type SocialBoard = components["schemas"]["SocialBoard"]
export type SocialCommunityOverview =
  components["schemas"]["SocialCommunityOverview"]
export type CreateSocialPollRequest =
  components["schemas"]["CreateSocialPollRequest"]
export type CreateSocialRedPacketRequest =
  components["schemas"]["CreateSocialRedPacketRequest"]

export type SocialFeedFilters = {
  authorUsername?: string
  feed?: SocialFeedKind
  boardId?: string
  featuredOnly?: boolean
  topic?: string
  enabled?: boolean
}

function normalizeSocialFilters(filters: SocialFeedFilters): SocialFeedFilters {
  return {
    ...(filters.authorUsername
      ? { authorUsername: filters.authorUsername }
      : {}),
    ...(filters.feed && filters.feed !== "discover"
      ? { feed: filters.feed }
      : {}),
    ...(filters.boardId ? { boardId: filters.boardId } : {}),
    ...(filters.featuredOnly ? { featuredOnly: true } : {}),
    ...(filters.topic ? { topic: filters.topic } : {}),
  }
}

export const socialPostKeys = {
  all: ["social", "posts"] as const,
  page: (
    sort: SocialPostSort,
    limit: number,
    offset: number,
    filtersOrAuthor: SocialFeedFilters | string = {}
  ) => {
    const rawFilters =
      typeof filtersOrAuthor === "string"
        ? { authorUsername: filtersOrAuthor }
        : filtersOrAuthor
    const filters = normalizeSocialFilters(rawFilters)
    return [
      ...socialPostKeys.all,
      "page",
      { sort, limit, offset, ...filters },
    ] as const
  },
  detail: (postId: string) =>
    [...socialPostKeys.all, "detail", postId] as const,
  infinite: (sort: SocialPostSort, limit: number, authorUsername: string) =>
    [
      ...socialPostKeys.all,
      "infinite",
      { sort, limit, authorUsername },
    ] as const,
  overview: () => ["social", "overview"] as const,
}

async function fetchSocialPosts(
  sort: SocialPostSort,
  limit: number,
  offset: number,
  filters: SocialFeedFilters = {}
): Promise<SocialPostPage> {
  filters = normalizeSocialFilters(filters)
  const { data, error, response } = await apiClient.GET(
    "/api/v1/social/posts",
    {
      params: {
        query: {
          sort,
          limit,
          offset,
          author_username: filters.authorUsername,
          feed: filters.feed,
          board_id: filters.boardId,
          featured_only: filters.featuredOnly,
          topic: filters.topic,
        },
      },
    }
  )
  if (!response.ok || !data) {
    throw new ApiProblemError(response.status, error)
  }
  return data
}

export function socialPostsQueryOptions(
  sort: SocialPostSort,
  limit: number,
  offset: number,
  filtersOrAuthor: SocialFeedFilters | string = {}
) {
  const rawFilters =
    typeof filtersOrAuthor === "string"
      ? { authorUsername: filtersOrAuthor }
      : filtersOrAuthor
  const filters = normalizeSocialFilters(rawFilters)
  return queryOptions({
    queryKey: socialPostKeys.page(sort, limit, offset, filters),
    queryFn: () => fetchSocialPosts(sort, limit, offset, filters),
    staleTime: 15_000,
    retry: false,
  })
}

export function socialPostQueryOptions(postId: string) {
  return queryOptions({
    queryKey: socialPostKeys.detail(postId),
    queryFn: async (): Promise<SocialPost> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/social/posts/{post_id}",
        { params: { path: { post_id: postId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 15_000,
    retry: false,
  })
}

export function useSocialPosts(
  sort: SocialPostSort,
  limit: number,
  offset: number,
  optionsOrAuthor: SocialFeedFilters | string = {}
) {
  const options =
    typeof optionsOrAuthor === "string"
      ? { authorUsername: optionsOrAuthor }
      : optionsOrAuthor
  return useQuery({
    ...socialPostsQueryOptions(sort, limit, offset, options),
    enabled: options.enabled ?? true,
  })
}

export function useSocialPost(postId: string) {
  return useQuery(socialPostQueryOptions(postId))
}

export function useInfiniteSocialPosts(
  sort: SocialPostSort,
  limit: number,
  authorUsername: string,
  enabled = true
) {
  return useInfiniteQuery({
    queryKey: socialPostKeys.infinite(sort, limit, authorUsername),
    queryFn: ({ pageParam }) =>
      fetchSocialPosts(sort, limit, pageParam, { authorUsername }),
    initialPageParam: 0,
    getNextPageParam: (lastPage) => {
      const nextOffset = lastPage.offset + lastPage.items.length
      return nextOffset < lastPage.total ? nextOffset : undefined
    },
    staleTime: 15_000,
    retry: false,
    enabled,
  })
}

export function useCreateSocialPost() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      content: string
      boardId: string
      mediaIds?: string[]
      poll?: CreateSocialPollRequest
      redPacket?: CreateSocialRedPacketRequest
      csrfToken: string
      idempotencyKey: string
    }): Promise<SocialPost> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/social/posts",
        {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            content: input.content,
            board_id: input.boardId,
            media_ids: input.mediaIds,
            poll: input.poll,
            red_packet: input.redPacket,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async (post) => {
      queryClient.setQueryData(socialPostKeys.detail(post.id), post)
      await queryClient.invalidateQueries({ queryKey: socialPostKeys.all })
    },
  })
}

export function useSocialCommunityOverview() {
  return useQuery({
    queryKey: socialPostKeys.overview(),
    queryFn: async (): Promise<SocialCommunityOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/social/overview"
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 30_000,
    retry: false,
  })
}

export function useUploadSocialMedia() {
  return useMutation({
    mutationFn: async (input: { file: File; csrfToken: string }) => {
      const body = new FormData()
      body.append("image", input.file)
      const { data, error, response } = await apiClient.POST(
        "/api/v1/social/media",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: { image: "" },
          bodySerializer: () => body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

function usePostInteraction(kind: "like" | "repost") {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      postId: string
      active: boolean
      csrfToken: string
    }) => {
      const path =
        kind === "like"
          ? "/api/v1/social/posts/{post_id}/like"
          : "/api/v1/social/posts/{post_id}/repost"
      const request = {
        params: {
          path: { post_id: input.postId },
          header: { "X-CSRF-Token": input.csrfToken },
        },
      } as const
      const result = input.active
        ? await apiClient.PUT(path, request)
        : await apiClient.DELETE(path, request)
      if (!result.response.ok || !result.data) {
        throw new ApiProblemError(result.response.status, result.error)
      }
      return result.data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: socialPostKeys.all })
    },
  })
}

export function useSocialPostLike() {
  return usePostInteraction("like")
}

export function useSocialPostRepost() {
  return usePostInteraction("repost")
}

export function useSocialFollow() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      username: string
      active: boolean
      csrfToken: string
    }) => {
      const request = {
        params: {
          path: { username: input.username },
          header: { "X-CSRF-Token": input.csrfToken },
        },
      } as const
      const result = input.active
        ? await apiClient.PUT("/api/v1/social/users/{username}/follow", request)
        : await apiClient.DELETE(
            "/api/v1/social/users/{username}/follow",
            request
          )
      if (!result.response.ok || !result.data) {
        throw new ApiProblemError(result.response.status, result.error)
      }
      return result.data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: socialPostKeys.all })
    },
  })
}

export function useSocialPollVote() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      postId: string
      optionId: string
      csrfToken: string
    }) => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/social/posts/{post_id}/poll-vote",
        {
          params: {
            path: { post_id: input.postId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: { option_id: input.optionId },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: socialPostKeys.all })
    },
  })
}

export function useClaimSocialRedPacket() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      postId: string
      csrfToken: string
      idempotencyKey: string
    }) => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/social/posts/{post_id}/red-packet/claims",
        {
          params: {
            path: { post_id: input.postId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: socialPostKeys.all })
    },
  })
}

export function useUpdateSocialPost(postId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      content: string
      expectedVersion: number
      csrfToken: string
    }): Promise<SocialPost> => {
      const { data, error, response } = await apiClient.PATCH(
        "/api/v1/social/posts/{post_id}",
        {
          params: {
            path: { post_id: postId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: {
            content: input.content,
            expected_version: input.expectedVersion,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async (post) => {
      queryClient.setQueryData(socialPostKeys.detail(postId), post)
      await queryClient.invalidateQueries({ queryKey: socialPostKeys.all })
    },
  })
}

export function useDeleteSocialPost() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      postId: string
      expectedVersion: number
      csrfToken: string
    }): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/social/posts/{post_id}",
        {
          params: {
            path: { post_id: input.postId },
            query: { expected_version: input.expectedVersion },
            header: { "X-CSRF-Token": input.csrfToken },
          },
        }
      )
      if (!response.ok) {
        throw new ApiProblemError(response.status, error)
      }
    },
    onSuccess: async (_, input) => {
      queryClient.removeQueries({
        queryKey: socialPostKeys.detail(input.postId),
      })
      await queryClient.invalidateQueries({ queryKey: socialPostKeys.all })
    },
  })
}
