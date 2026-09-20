import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { AdminStats, RenewalRequest } from '../types'

export function useAdminStats() {
  return useQuery({ queryKey: ['admin', 'stats'], queryFn: () => api<AdminStats>('/api/admin/stats'), retry: false })
}

export function useAdminRenewals(status: string) {
  return useQuery({
    queryKey: ['admin', 'renewals', status],
    queryFn: () => api<{ requests: RenewalRequest[] }>(`/api/admin/renewals?status=${encodeURIComponent(status)}&limit=100`),
    retry: false,
    refetchInterval: 30_000,
  })
}

export function useResolveRenewal() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, action }: { id: number; action: 'confirm' | 'reject' }) =>
      api<{ request: RenewalRequest }>(`/api/admin/renewals/${id}/${action}`, { method: 'POST' }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['admin'] }),
  })
}
