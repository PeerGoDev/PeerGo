import {
  type QueryClient,
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import { siteKeys } from "~/features/site/api/site.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type ManagedAnnouncement = components["schemas"]["ManagedAnnouncement"]
export type ManagedAnnouncementSummary =
  components["schemas"]["ManagedAnnouncementSummary"]
export type ManagedAnnouncementPage =
  components["schemas"]["ManagedAnnouncementPage"]
export type AnnouncementRevisionPage =
  components["schemas"]["AnnouncementRevisionPage"]
export type CreateManagedAnnouncementRequest =
  components["schemas"]["CreateManagedAnnouncementRequest"]
export type UpdateManagedAnnouncementDraftRequest =
  components["schemas"]["UpdateManagedAnnouncementDraftRequest"]
export type ChangeAnnouncementPublicationRequest =
  components["schemas"]["ChangeAnnouncementPublicationRequest"]
export type AnnouncementPublicationAction =
  components["schemas"]["AnnouncementPublicationAction"]

export const announcementAdministrationKeys = {
  all: ["staff", "catalog", "announcements"] as const,
  lists: () => [...announcementAdministrationKeys.all, "list"] as const,
  list: (limit: number, offset: number) =>
    [...announcementAdministrationKeys.lists(), { limit, offset }] as const,
  details: () => [...announcementAdministrationKeys.all, "detail"] as const,
  detail: (announcementId: string) =>
    [...announcementAdministrationKeys.details(), announcementId] as const,
  revisions: (announcementId: string) =>
    [
      ...announcementAdministrationKeys.detail(announcementId),
      "revisions",
    ] as const,
}

export function managedAnnouncementListQueryOptions(limit = 50, offset = 0) {
  return queryOptions({
    queryKey: announcementAdministrationKeys.list(limit, offset),
    queryFn: async (): Promise<ManagedAnnouncementPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/catalog/announcements",
        { params: { query: { limit, offset } } }
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

export function managedAnnouncementQueryOptions(
  announcementId: string,
  enabled = true
) {
  return queryOptions({
    queryKey: announcementAdministrationKeys.detail(announcementId),
    queryFn: async (): Promise<ManagedAnnouncement> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/catalog/announcements/{announcement_id}",
        { params: { path: { announcement_id: announcementId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: enabled && announcementId.length > 0,
    staleTime: 10_000,
    retry: false,
  })
}

export function announcementRevisionQueryOptions(
  announcementId: string,
  enabled = true
) {
  return queryOptions({
    queryKey: announcementAdministrationKeys.revisions(announcementId),
    queryFn: async (): Promise<AnnouncementRevisionPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/catalog/announcements/{announcement_id}/revisions",
        {
          params: {
            path: { announcement_id: announcementId },
            query: { limit: 50, offset: 0 },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: enabled && announcementId.length > 0,
    staleTime: 10_000,
    retry: false,
  })
}

export function useCreateManagedAnnouncement() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: CreateManagedAnnouncementRequest
    }): Promise<ManagedAnnouncement> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/catalog/announcements",
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
    onSuccess: invalidateAnnouncementQueries(queryClient),
  })
}

export function useUpdateManagedAnnouncementDraft() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      announcementId: string
      body: UpdateManagedAnnouncementDraftRequest
    }): Promise<ManagedAnnouncement> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/catalog/announcements/{announcement_id}",
        {
          params: {
            header: { "X-CSRF-Token": input.csrfToken },
            path: { announcement_id: input.announcementId },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidateAnnouncementQueries(queryClient),
    onError: invalidateManagedAnnouncementQueries(queryClient),
  })
}

export function useChangeManagedAnnouncementPublication() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      announcementId: string
      body: ChangeAnnouncementPublicationRequest
    }): Promise<ManagedAnnouncement> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/catalog/announcements/{announcement_id}/publication",
        {
          params: {
            header: { "X-CSRF-Token": input.csrfToken },
            path: { announcement_id: input.announcementId },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidateAnnouncementQueries(queryClient),
    onError: invalidateManagedAnnouncementQueries(queryClient),
  })
}

function invalidateAnnouncementQueries(queryClient: QueryClient) {
  return async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: announcementAdministrationKeys.all,
      }),
      queryClient.invalidateQueries({ queryKey: siteKeys.all }),
    ])
  }
}

function invalidateManagedAnnouncementQueries(queryClient: QueryClient) {
  return async () => {
    await queryClient.invalidateQueries({
      queryKey: announcementAdministrationKeys.all,
    })
  }
}
