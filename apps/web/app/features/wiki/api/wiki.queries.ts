import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type WikiPage = components["schemas"]["WikiPage"]
export type WikiPageList = components["schemas"]["WikiPageList"]
export type UpdateAssignedWikiPageRequest =
  components["schemas"]["UpdateAssignedWikiPageRequest"]

const wikiSlugPattern = /^[a-z0-9][a-z0-9-]{0,95}$/

export const wikiKeys = {
  all: ["wiki"] as const,
  lists: () => [...wikiKeys.all, "list"] as const,
  list: (query: string, limit: number, offset: number) =>
    [...wikiKeys.lists(), { query, limit, offset }] as const,
  details: () => [...wikiKeys.all, "detail"] as const,
  detail: (slug: string) => [...wikiKeys.details(), slug] as const,
}

export function wikiPageListQueryOptions(query = "", limit = 100, offset = 0) {
  return queryOptions({
    queryKey: wikiKeys.list(query, limit, offset),
    queryFn: async (): Promise<WikiPageList> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/wiki/pages",
        {
          params: {
            query: { query: query.trim() || undefined, limit, offset },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 30_000,
  })
}

export function wikiPageQueryOptions(slug: string, enabled = true) {
  return queryOptions({
    queryKey: wikiKeys.detail(slug),
    queryFn: async (): Promise<WikiPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/wiki/pages/{slug}",
        { params: { path: { slug } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: enabled && wikiSlugPattern.test(slug),
    staleTime: 30_000,
    retry: false,
  })
}

export function useWikiPageList(query = "", limit = 100, offset = 0) {
  return useQuery(wikiPageListQueryOptions(query, limit, offset))
}

export function useWikiPage(slug: string, enabled = true) {
  return useQuery(wikiPageQueryOptions(slug, enabled))
}

export function useUpdateAssignedWikiPage() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      pageId: string
      slug: string
      body: UpdateAssignedWikiPageRequest
    }): Promise<WikiPage> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/wiki/pages/{page_id}/content",
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
    onSuccess: async (page) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: wikiKeys.lists() }),
        queryClient.invalidateQueries({ queryKey: wikiKeys.detail(page.slug) }),
      ])
    },
  })
}

export function isWikiSlug(value: string) {
  return wikiSlugPattern.test(value)
}
