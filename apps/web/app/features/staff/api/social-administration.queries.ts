import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type ManagedSocialBoard = components["schemas"]["ManagedSocialBoard"]
export type CreateManagedSocialBoardRequest =
  components["schemas"]["CreateManagedSocialBoardRequest"]
export type UpdateManagedSocialBoardRequest =
  components["schemas"]["UpdateManagedSocialBoardRequest"]
export type ModerateSocialPostRequest =
  components["schemas"]["ModerateSocialPostRequest"]

export const socialAdministrationKeys = {
  all: ["staff", "social"] as const,
  boards: () => [...socialAdministrationKeys.all, "boards"] as const,
  posts: (boardId: string, offset: number) =>
    [...socialAdministrationKeys.all, "posts", { boardId, offset }] as const,
}

export const managedSocialBoardsQueryOptions = queryOptions({
  queryKey: socialAdministrationKeys.boards(),
  queryFn: async (): Promise<ManagedSocialBoard[]> => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/admin/social/boards"
    )
    if (!response.ok || !data) throw new ApiProblemError(response.status, error)
    return data
  },
  staleTime: 10_000,
  retry: false,
})

export function managedSocialPostsQueryOptions(
  boardId: string,
  offset: number
) {
  return queryOptions({
    queryKey: socialAdministrationKeys.posts(boardId, offset),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/social/posts",
        {
          params: {
            query: { board_id: boardId || undefined, limit: 20, offset },
          },
        }
      )
      if (!response.ok || !data)
        throw new ApiProblemError(response.status, error)
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export function useCreateManagedSocialBoard() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: CreateManagedSocialBoardRequest
    }) => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/social/boards",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: input.body,
        }
      )
      if (!response.ok || !data)
        throw new ApiProblemError(response.status, error)
      return data
    },
    onSuccess: async () =>
      queryClient.invalidateQueries({ queryKey: socialAdministrationKeys.all }),
  })
}

export function useUpdateManagedSocialBoard() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      boardId: string
      body: UpdateManagedSocialBoardRequest
    }) => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/social/boards/{board_id}",
        {
          params: {
            path: { board_id: input.boardId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data)
        throw new ApiProblemError(response.status, error)
      return data
    },
    onSuccess: async () =>
      queryClient.invalidateQueries({ queryKey: socialAdministrationKeys.all }),
  })
}

export function useModerateSocialPost() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      postId: string
      body: ModerateSocialPostRequest
    }) => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/social/posts/{post_id}",
        {
          params: {
            path: { post_id: input.postId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data)
        throw new ApiProblemError(response.status, error)
      return data
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: socialAdministrationKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: ["social", "posts"] }),
      ])
    },
  })
}
