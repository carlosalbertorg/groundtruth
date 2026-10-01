import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'

export interface APIToken {
  id: string
  name: string
  created_at: string
  last_used_at: string | null
}

export interface CreatedAPIToken extends APIToken {
  token: string
}

const queryKey = ['api-tokens'] as const

export function useAPITokens() {
  return useQuery({
    queryKey,
    queryFn: () => api.get<APIToken[]>('/api-tokens'),
  })
}

export function useCreateAPIToken() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => api.post<CreatedAPIToken>('/api-tokens', { name }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  })
}

export function useRevokeAPIToken() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/api-tokens/${id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  })
}
