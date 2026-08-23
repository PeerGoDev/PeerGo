import { queryOptions, useQuery } from "@tanstack/react-query"

import type { components, operations } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type TorrentCategory = components["schemas"]["CategoryWithCount"]
export type TorrentCategoryFacet = components["schemas"]["CategoryFacet"]
export type TorrentPublicDetail = components["schemas"]["TorrentPublicDetail"]
export type TorrentPublicContent = components["schemas"]["TorrentPublicContent"]
export type TorrentRelatedVersions =
  components["schemas"]["TorrentRelatedVersions"]
export type TorrentSwarmOverview = components["schemas"]["TorrentSwarmOverview"]
export type TorrentPeerList = components["schemas"]["TorrentPeerList"]
export type ManagedTorrentPeerList =
  components["schemas"]["ManagedTorrentPeerList"]
export type TorrentFilePage = components["schemas"]["TorrentFilePage"]
export type MyTorrentSubmissionPage =
  components["schemas"]["MyTorrentSubmissionPage"]
export type TorrentPromotion =
  components["schemas"]["TorrentSummary"]["promotion"]
export type TorrentID = components["schemas"]["TorrentSummary"]["id"]
type TorrentListQuery = NonNullable<
  operations["listTorrents"]["parameters"]["query"]
>
export type TorrentSearchScope = NonNullable<TorrentListQuery["search_scope"]>
export type TorrentSort = NonNullable<TorrentListQuery["sort"]>

export type TorrentListFilters = {
  query?: string
  searchScope?: TorrentSearchScope
  categoryId?: string
  promotion?: TorrentPromotion
  sort?: TorrentSort
  limit?: number
  offset?: number
}

export const torrentKeys = {
  all: ["torrents"] as const,
  categories: () => [...torrentKeys.all, "categories"] as const,
  categoryFacets: (categoryId: string) =>
    [...torrentKeys.categories(), categoryId, "facets"] as const,
  list: (filters: TorrentListFilters) =>
    [...torrentKeys.all, "list", filters] as const,
  detail: (torrentId: TorrentID) =>
    [...torrentKeys.all, "detail", torrentId] as const,
  content: (torrentId: TorrentID) =>
    [...torrentKeys.detail(torrentId), "content"] as const,
  related: (torrentId: TorrentID) =>
    [...torrentKeys.detail(torrentId), "related"] as const,
  swarm: (torrentId: TorrentID) =>
    [...torrentKeys.detail(torrentId), "swarm"] as const,
  peers: (torrentId: TorrentID) =>
    [...torrentKeys.detail(torrentId), "peers"] as const,
  managedPeers: (torrentId: TorrentID) =>
    [...torrentKeys.detail(torrentId), "managed-peers"] as const,
  files: (torrentId: TorrentID, limit: number, offset: number) =>
    [...torrentKeys.detail(torrentId), "files", { limit, offset }] as const,
  mySubmissions: (userId: string, limit: number) =>
    [
      ...torrentKeys.all,
      "current-user",
      userId,
      "submissions",
      { limit },
    ] as const,
}

export function torrentSwarmQueryOptions(torrentId: TorrentID) {
  return queryOptions({
    queryKey: torrentKeys.swarm(torrentId),
    queryFn: async (): Promise<TorrentSwarmOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/torrents/{torrent_id}/swarm",
        { params: { path: { torrent_id: torrentId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 30_000,
    refetchInterval: 60_000,
    retry: false,
  })
}

export function managedTorrentPeersQueryOptions(torrentId: TorrentID) {
  return queryOptions({
    queryKey: torrentKeys.managedPeers(torrentId),
    queryFn: async (): Promise<ManagedTorrentPeerList> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/torrents/{torrent_id}/peers",
        { params: { path: { torrent_id: torrentId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    refetchInterval: 30_000,
    retry: false,
  })
}

export function torrentPeersQueryOptions(torrentId: TorrentID) {
  return queryOptions({
    queryKey: torrentKeys.peers(torrentId),
    queryFn: async (): Promise<TorrentPeerList> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/torrents/{torrent_id}/peers",
        { params: { path: { torrent_id: torrentId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    refetchInterval: 30_000,
    retry: false,
  })
}

export const categoryListQueryOptions = queryOptions({
  queryKey: torrentKeys.categories(),
  queryFn: async (): Promise<TorrentCategory[]> => {
    const { data, error, response } = await apiClient.GET("/api/v1/categories")
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 60_000,
})

export function categoryFacetsQueryOptions(categoryId: string) {
  return queryOptions({
    queryKey: torrentKeys.categoryFacets(categoryId),
    queryFn: async (): Promise<TorrentCategoryFacet[]> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/categories/{category_id}/facets",
        { params: { path: { category_id: categoryId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 60_000,
  })
}

export function torrentListQueryOptions(filters: TorrentListFilters = {}) {
  const normalizedFilters = {
    query: filters.query?.trim() ?? "",
    searchScope: filters.searchScope,
    categoryId: filters.categoryId ?? "",
    promotion: filters.promotion,
    sort: filters.sort,
    limit: filters.limit ?? 20,
    offset: filters.offset ?? 0,
  }
  return queryOptions({
    queryKey: torrentKeys.list(normalizedFilters),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/torrents",
        {
          params: {
            query: {
              limit: normalizedFilters.limit,
              offset: normalizedFilters.offset,
              ...(normalizedFilters.query
                ? { query: normalizedFilters.query }
                : {}),
              ...(normalizedFilters.searchScope
                ? { search_scope: normalizedFilters.searchScope }
                : {}),
              ...(normalizedFilters.categoryId
                ? { category_id: normalizedFilters.categoryId }
                : {}),
              ...(normalizedFilters.promotion
                ? { promotion: normalizedFilters.promotion }
                : {}),
              ...(normalizedFilters.sort
                ? { sort: normalizedFilters.sort }
                : {}),
            },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

export function useTorrentList(
  filters: TorrentListFilters = {},
  enabled = true
) {
  return useQuery({ ...torrentListQueryOptions(filters), enabled })
}

export function useCategoryList(enabled = true) {
  return useQuery({ ...categoryListQueryOptions, enabled })
}

export function useCategoryFacets(categoryId: string, enabled = true) {
  return useQuery({ ...categoryFacetsQueryOptions(categoryId), enabled })
}

export function torrentDetailQueryOptions(torrentId: TorrentID) {
  return queryOptions({
    queryKey: torrentKeys.detail(torrentId),
    queryFn: async (): Promise<TorrentPublicDetail> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/torrents/{torrent_id}",
        { params: { path: { torrent_id: torrentId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 60_000,
    retry: false,
  })
}

export function torrentContentQueryOptions(torrentId: TorrentID) {
  return queryOptions({
    queryKey: torrentKeys.content(torrentId),
    queryFn: async (): Promise<TorrentPublicContent> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/torrents/{torrent_id}/content",
        { params: { path: { torrent_id: torrentId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: Number.POSITIVE_INFINITY,
    retry: false,
  })
}

export function torrentRelatedVersionsQueryOptions(torrentId: TorrentID) {
  return queryOptions({
    queryKey: torrentKeys.related(torrentId),
    queryFn: async (): Promise<TorrentRelatedVersions> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/torrents/{torrent_id}/related",
        { params: { path: { torrent_id: torrentId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 60_000,
    retry: false,
  })
}

export function torrentFilesQueryOptions(
  torrentId: TorrentID,
  limit: number,
  offset: number
) {
  return queryOptions({
    queryKey: torrentKeys.files(torrentId, limit, offset),
    queryFn: async (): Promise<TorrentFilePage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/torrents/{torrent_id}/files",
        {
          params: {
            path: { torrent_id: torrentId },
            query: { limit, offset },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: Number.POSITIVE_INFINITY,
    retry: false,
  })
}

export function myTorrentSubmissionsQueryOptions(userId: string, limit = 20) {
  return queryOptions({
    queryKey: torrentKeys.mySubmissions(userId, limit),
    queryFn: async (): Promise<MyTorrentSubmissionPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/torrent-submissions",
        { params: { query: { limit } } }
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

export function useTorrentDetail(torrentId: TorrentID, enabled = true) {
  return useQuery({
    ...torrentDetailQueryOptions(torrentId),
    enabled,
  })
}

export function useTorrentContent(torrentId: TorrentID, enabled = true) {
  return useQuery({
    ...torrentContentQueryOptions(torrentId),
    enabled,
  })
}

export function useTorrentRelatedVersions(
  torrentId: TorrentID,
  enabled = true
) {
  return useQuery({
    ...torrentRelatedVersionsQueryOptions(torrentId),
    enabled,
  })
}

export function useTorrentSwarm(torrentId: TorrentID, enabled = true) {
  return useQuery({
    ...torrentSwarmQueryOptions(torrentId),
    enabled,
  })
}

export function useManagedTorrentPeers(torrentId: TorrentID, enabled = true) {
  return useQuery({
    ...managedTorrentPeersQueryOptions(torrentId),
    enabled,
  })
}

export function useTorrentPeers(torrentId: TorrentID, enabled = true) {
  return useQuery({
    ...torrentPeersQueryOptions(torrentId),
    enabled,
  })
}

export function useTorrentFiles(
  torrentId: TorrentID,
  limit: number,
  offset: number,
  enabled = true
) {
  return useQuery({
    ...torrentFilesQueryOptions(torrentId, limit, offset),
    enabled,
  })
}

export function useMyTorrentSubmissions(
  userId: string | undefined,
  enabled = true
) {
  return useQuery({
    ...myTorrentSubmissionsQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId) && enabled,
  })
}
