import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { api } from '../api'
import type { MeResponse } from '../types'

export function useMe() {
  return useQuery({
    queryKey: ['me'],
    queryFn: () => api<MeResponse>('/api/me'),
    retry: false,
    refetchInterval: 60_000,
  })
}

/** Ends the session. An admin goes back to the sign-in form, a client to the "need a link" page. */
export function useLogout(redirectTo = '/no-session') {
  const qc = useQueryClient()
  const navigate = useNavigate()
  return useMutation({
    mutationFn: () => api<void>('/api/logout', { method: 'POST' }),
    onSettled: () => {
      qc.clear()
      void navigate(redirectTo, { replace: true })
    },
  })
}
