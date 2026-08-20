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

export const socialPostKeys = {
  all: ["social", "posts"] as const,
  page: (
    sort: SocialPostSort,
    limit: number,
    offset: number,
    authorUsername?: string
  ) =>
    [
      ...socialPostKeys.all,
      "page",
      { sort, limit, offset, authorUsername: authorUsername ?? null },
    ] as const,
  detail: (postId: string) =>
    [...socialPostKeys.all, "detail", postId] as const,
  infinite: (sort: SocialPostSort, limit: number, authorUsername: string) =>
    [
      ...socialPostKeys.all,
      "infinite",
      { sort, limit, authorUsername },
    ] as const,
}

async function fetchSocialPosts(
  sort: SocialPostSort,
  limit: number,
  offset: number,
  authorUsername?: string
): Promise<SocialPostPage> {
  const { data, error, response } = await apiClient.GET(
    "/api/v1/social/posts",
    {
      params: {
        query: {
          sort,
          limit,
          offset,
          author_username: authorUsername,
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
  authorUsername?: string
) {
  return queryOptions({
    queryKey: socialPostKeys.page(sort, limit, offset, authorUsername),
    queryFn: () => fetchSocialPosts(sort, limit, offset, authorUsername),
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
  options: { authorUsername?: string; enabled?: boolean } = {}
) {
  return useQuery({
    ...socialPostsQueryOptions(sort, limit, offset, options.authorUsername),
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
      fetchSocialPosts(sort, limit, pageParam, authorUsername),
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
          body: { content: input.content },
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
