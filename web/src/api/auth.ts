import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from './client'

export interface User {
  id: string
  email: string
}

interface SetupStatus {
  setup_complete: boolean
}

const queryKeys = {
  setupStatus: ['setup-status'] as const,
  currentUser: ['current-user'] as const,
}

export function useSetupStatus() {
  return useQuery({
    queryKey: queryKeys.setupStatus,
    queryFn: () => api.get<SetupStatus>('/setup/status'),
  })
}

export function useCreateAdmin() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      api.post<User>('/setup/admin', input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.setupStatus })
    },
  })
}

/**
 * Current session's user, or null if not logged in. 401 is an expected,
 * non-error outcome here (not logged in yet) — it resolves to null rather
 * than rejecting, so callers don't need to special-case it.
 *
 * `enabled` should be false until first-run setup is known to be
 * complete: before that, /auth/me would just 503 ("setup_required")
 * rather than 401, which this hook doesn't treat as "logged out" — it's
 * a distinct state the caller should check separately.
 */
export function useCurrentUser(enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.currentUser,
    enabled,
    queryFn: async () => {
      try {
        return await api.get<User>('/auth/me')
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) return null
        throw err
      }
    },
  })
}

export function useLogin() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      api.post<User>('/auth/login', input),
    onSuccess: (user) => {
      queryClient.setQueryData(queryKeys.currentUser, user)
    },
  })
}

export function useLogout() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => api.post<void>('/auth/logout'),
    onSuccess: () => {
      queryClient.setQueryData(queryKeys.currentUser, null)
    },
  })
}
