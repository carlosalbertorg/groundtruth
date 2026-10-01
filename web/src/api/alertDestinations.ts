import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'

export type AlertDestinationKind = 'generic_webhook' | 'slack'

export interface AlertDestination {
  id: string
  workspace_id: string | null
  name: string
  kind: AlertDestinationKind
  url: string
  has_secret: boolean
  is_enabled: boolean
  created_at: string
}

export interface AlertDestinationInput {
  workspace_id: string | null
  name: string
  kind: AlertDestinationKind
  url: string
  shared_secret?: string
  is_enabled: boolean
}

const queryKey = ['alert-destinations'] as const

export function useAlertDestinations() {
  return useQuery({
    queryKey,
    queryFn: () => api.get<AlertDestination[]>('/alert-destinations'),
  })
}

export function useCreateAlertDestination() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: AlertDestinationInput) =>
      api.post<AlertDestination>('/alert-destinations', input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  })
}

export function useUpdateAlertDestination() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: AlertDestinationInput }) =>
      api.patch<AlertDestination>(`/alert-destinations/${id}`, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  })
}

export function useDeleteAlertDestination() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/alert-destinations/${id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  })
}
