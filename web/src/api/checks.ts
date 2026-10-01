import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from './client'

export type CheckStatus = 'queued' | 'running' | 'clean' | 'drifted' | 'failed'
export type DriftAction = 'no-op' | 'create' | 'update' | 'delete' | 'replace'

export interface DriftSummary {
  added: number
  changed: number
  destroyed: number
  unchanged: number
}

export interface ResourceDrift {
  address: string
  type: string
  module_address: string | null
  action: DriftAction
  before: Record<string, unknown> | null
  after: Record<string, unknown> | null
  has_sensitive: boolean
  has_unknown: boolean
}

export interface DriftCheck {
  id: string
  workspace_id: string
  status: CheckStatus
  started_at: string
  finished_at: string | null
  duration_ms: number | null
  summary: DriftSummary
  error_message: string | null
  triggered_by: 'manual' | 'schedule' | 'api'
  resources?: ResourceDrift[]
}

const queryKeys = {
  history: (workspaceId: string) => ['workspaces', workspaceId, 'checks'] as const,
  detail: (checkId: string) => ['checks', checkId] as const,
}

export function useWorkspaceChecks(workspaceId: string) {
  return useQuery({
    queryKey: queryKeys.history(workspaceId),
    queryFn: () => api.get<DriftCheck[]>(`/workspaces/${workspaceId}/checks`),
  })
}

export function useCheckDetail(checkId: string) {
  return useQuery({
    queryKey: queryKeys.detail(checkId),
    queryFn: () => api.get<DriftCheck>(`/checks/${checkId}`),
  })
}

/** A human-readable reason a "Check now" request was rejected. */
export function describeCheckError(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'check_in_progress':
        return 'A check is already running for this workspace.'
      case 'workspace_not_found':
        return 'This workspace no longer exists.'
      case 'unauthorized':
        return 'Your session has expired. Sign in again.'
    }
  }
  return 'The check could not be run. Please try again.'
}

export function useRunCheck(workspaceId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => api.post<DriftCheck>(`/workspaces/${workspaceId}/check`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['workspaces'] })
      queryClient.invalidateQueries({ queryKey: queryKeys.history(workspaceId) })
    },
  })
}
