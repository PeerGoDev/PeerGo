import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import { wikiKeys } from "~/features/wiki/api/wiki.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type WikiPage = components["schemas"]["WikiPage"]
export type WikiPageList = components["schemas"]["WikiPageList"]
export type WikiRevisionPage = components["schemas"]["WikiRevisionPage"]
export type CreateManagedWikiPageRequest =
  components["schemas"]["CreateManagedWikiPageRequest"]
export type UpdateManagedWikiPageRequest =
  components["schemas"]["UpdateManagedWikiPageRequest"]

export const wikiAdministrationKeys = {
  all: ["staff", "wiki"] as const,
  lists: () => [...wikiAdministrationKeys.all, "list"] as const,
  list: (query: string, limit: number, offset: number) =>
    [...wikiAdministrationKeys.lists(), { query, limit, offset }] as const,
  details: () => [...wikiAdministrationKeys.all, "detail"] as const,
  detail: (pageId: string) =>
    [...wikiAdministrationKeys.details(), pageId] as const,
  revisions: (pageId: string) =>
    [...wikiAdministrationKeys.detail(pageId), "revisions"] as const,
}

export function managedWikiPageListQueryOptions(
  query = "",
  limit = 100,
  offset = 0
) {
  return queryOptions({
    queryKey: wikiAdministrationKeys.list(query, limit, offset),
    queryFn: async (): Promise<WikiPageList> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/wiki/pages",
        {
          params: {
            query: {
              query: query.trim() || undefined,
              limit,
              offset,
              include_archived: true,
            },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export function managedWikiPageQueryOptions(pageId: string, enabled = true) {
  return queryOptions({
    queryKey: wikiAdministrationKeys.detail(pageId),
    queryFn: async (): Promise<WikiPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/wiki/pages/{page_id}",
        { params: { path: { page_id: pageId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: enabled && pageId.length > 0,
    staleTime: 10_000,
    retry: false,
  })
}

export function managedWikiRevisionsQueryOptions(
  pageId: string,
  enabled = true
) {
  return queryOptions({
    queryKey: wikiAdministrationKeys.revisions(pageId),
    queryFn: async (): Promise<WikiRevisionPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/wiki/pages/{page_id}/revisions",
        {
          params: {
            path: { page_id: pageId },
            query: { limit: 50, offset: 0 },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: enabled && pageId.length > 0,
    staleTime: 10_000,
    retry: false,
  })
}

export function useCreateManagedWikiPage() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: CreateManagedWikiPageRequest
    }): Promise<WikiPage> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/wiki/pages",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: () => invalidateWikiAdministration(queryClient),
  })
}

export function useUpdateManagedWikiPage() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      pageId: string
      body: UpdateManagedWikiPageRequest
    }): Promise<WikiPage> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/wiki/pages/{page_id}",
        {
          params: {
            path: { page_id: input.pageId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: () => invalidateWikiAdministration(queryClient),
  })
}

export function useRestoreManagedWikiRevision() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      pageId: string
      revisionNumber: number
      expectedVersion: number
      reason: string
    }): Promise<WikiPage> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/wiki/pages/{page_id}/revisions/{revision_number}/restore",
        {
          params: {
            path: {
              page_id: input.pageId,
              revision_number: input.revisionNumber,
            },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: {
            expected_version: input.expectedVersion,
            reason: input.reason,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: () => invalidateWikiAdministration(queryClient),
  })
}

async function invalidateWikiAdministration(
  queryClient: ReturnType<typeof useQueryClient>
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: wikiAdministrationKeys.all }),
    queryClient.invalidateQueries({ queryKey: wikiKeys.all }),
  ])
}
