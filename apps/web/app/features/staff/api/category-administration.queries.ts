import {
  type QueryClient,
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type ManagedCategory = components["schemas"]["ManagedCategory"]
export type CreateManagedCategoryRequest =
  components["schemas"]["CreateManagedCategoryRequest"]
export type UpdateManagedCategoryRequest =
  components["schemas"]["UpdateManagedCategoryRequest"]
export type ManagedCategoryFacet = components["schemas"]["ManagedCategoryFacet"]
export type ManagedCategoryFacetOption =
  components["schemas"]["ManagedCategoryFacetOption"]
export type UpsertManagedCategoryFacetOptionRequest =
  components["schemas"]["UpsertManagedCategoryFacetOptionRequest"]

export const categoryAdministrationKeys = {
  all: ["staff", "catalog", "categories"] as const,
  list: () => [...categoryAdministrationKeys.all, "list"] as const,
}

export const managedCategoryListQueryOptions = queryOptions({
  queryKey: categoryAdministrationKeys.list(),
  queryFn: async (): Promise<ManagedCategory[]> => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/admin/catalog/categories"
    )
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 10_000,
  retry: false,
})

export function useCreateManagedCategory() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: CreateManagedCategoryRequest
    }): Promise<ManagedCategory> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/catalog/categories",
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
    onSuccess: invalidateCategoryQueries(queryClient),
  })
}

export function useUpdateManagedCategory() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      categoryId: string
      body: UpdateManagedCategoryRequest
    }): Promise<ManagedCategory> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/catalog/categories/{category_id}",
        {
          params: {
            header: { "X-CSRF-Token": input.csrfToken },
            path: { category_id: input.categoryId },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidateCategoryQueries(queryClient),
    onError: async () => {
      await queryClient.invalidateQueries({
        queryKey: categoryAdministrationKeys.all,
      })
    },
  })
}

export function useUpsertManagedCategoryFacetOption() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      categoryId: string
      facetId: string
      optionKey: string
      body: UpsertManagedCategoryFacetOptionRequest
    }): Promise<ManagedCategoryFacetOption> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/catalog/categories/{category_id}/facets/{facet_id}/options/{option_key}",
        {
          params: {
            header: { "X-CSRF-Token": input.csrfToken },
            path: {
              category_id: input.categoryId,
              facet_id: input.facetId,
              option_key: input.optionKey,
            },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidateCategoryQueries(queryClient),
    onError: async () => {
      await queryClient.invalidateQueries({
        queryKey: categoryAdministrationKeys.all,
      })
    },
  })
}

function invalidateCategoryQueries(queryClient: QueryClient) {
  return async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: categoryAdministrationKeys.all,
      }),
      queryClient.invalidateQueries({ queryKey: torrentKeys.all }),
    ])
  }
}
