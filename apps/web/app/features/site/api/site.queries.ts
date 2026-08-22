import { queryOptions, useQuery } from "@tanstack/react-query"

import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

const announcementIdPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$/

export const siteKeys = {
  all: ["site"] as const,
  info: () => [...siteKeys.all, "info"] as const,
  announcements: () => [...siteKeys.all, "announcements"] as const,
  announcementPage: (limit: number, offset: number) =>
    [...siteKeys.announcements(), "list", { limit, offset }] as const,
  latestAnnouncement: () => [...siteKeys.announcements(), "latest"] as const,
  announcement: (announcementId: string) =>
    [...siteKeys.announcements(), "detail", announcementId] as const,
}

export const siteInfoQueryOptions = queryOptions({
  queryKey: siteKeys.info(),
  queryFn: async () => {
    const { data, error, response } = await apiClient.GET("/api/v1/site")
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 30_000,
  refetchInterval: 60_000,
})

export const latestAnnouncementQueryOptions = queryOptions({
  queryKey: siteKeys.latestAnnouncement(),
  queryFn: async () => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/announcements/latest"
    )
    if (!response.ok) {
      throw new ApiProblemError(response.status, error)
    }
    return data ?? null
  },
})

export function announcementPageQueryOptions(limit: number, offset: number) {
  return queryOptions({
    queryKey: siteKeys.announcementPage(limit, offset),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/announcements",
        { params: { query: { limit, offset } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 30_000,
  })
}

export function announcementQueryOptions(
  announcementId: string,
  enabled = true
) {
  return queryOptions({
    queryKey: siteKeys.announcement(announcementId),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/announcements/{announcement_id}",
        { params: { path: { announcement_id: announcementId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: enabled && isAnnouncementId(announcementId),
    staleTime: 30_000,
    retry: false,
  })
}

export function useSiteInfo() {
  return useQuery(siteInfoQueryOptions)
}

export function useLatestAnnouncement(enabled = true) {
  return useQuery({ ...latestAnnouncementQueryOptions, enabled })
}

export function useAnnouncementPage(limit: number, offset: number) {
  return useQuery(announcementPageQueryOptions(limit, offset))
}

export function useAnnouncement(announcementId: string, enabled = true) {
  return useQuery(announcementQueryOptions(announcementId, enabled))
}

export function isAnnouncementId(value: string) {
  return announcementIdPattern.test(value)
}
