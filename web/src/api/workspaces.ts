import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'

export type BinaryKind = 'terraform' | 'tofu'

export interface Workspace {
  id: string
  name: string
  description: string | null
  source_path: string
  working_subdirectory: string | null
  binary_kind: BinaryKind
  binary_version: string | null
  credential_env_file: string | null
  check_interval_minutes: number
  check_timeout_seconds: number
  is_enabled: boolean
  last_check_id: string | null
  last_check_status: string | null
  created_at: string
  updated_at: string
}

export interface WorkspaceInput {
  name: string
  description: string | null
  source_path: string
  working_subdirectory: string | null
  binary_kind: BinaryKind
  binary_version: string | null
  credential_env_file: string | null
  check_interval_minutes: number
  check_timeout_seconds: number
  is_enabled: boolean
}

const queryKeys = {
  workspaces: ['workspaces'] as const,
  workspace: (id: string) => ['workspaces', id] as const,
}

export function useWorkspaces() {
  return useQuery({
    queryKey: queryKeys.workspaces,
    queryFn: () => api.get<Workspace[]>('/workspaces'),
  })
}

export function useCreateWorkspace() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: WorkspaceInput) => api.post<Workspace>('/workspaces', input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.workspaces })
    },
  })
}

export function useUpdateWorkspace() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: WorkspaceInput }) =>
      api.patch<Workspace>(`/workspaces/${id}`, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.workspaces })
    },
  })
}

export function useDeleteWorkspace() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/workspaces/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.workspaces })
    },
  })
}
