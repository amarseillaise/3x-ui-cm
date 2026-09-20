import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { PlansResponse, RenewResponse, RenewalRequest } from '../types'

export function usePlans() {
  return useQuery({ queryKey: ['plans'], queryFn: () => api<PlansResponse>('/api/plans'), retry: false })
}

export function useMyRenewals() {
  return useQuery({ queryKey: ['renewals'], queryFn: () => api<{ requests: RenewalRequest[] }>('/api/renewals'), retry: false })
}

export function useRenew() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (planId: string) => api<RenewResponse>('/api/renew', { method: 'POST', body: JSON.stringify({ planId }) }),
    onSettled: () => Promise.all([qc.invalidateQueries({ queryKey: ['me'] }), qc.invalidateQueries({ queryKey: ['plans'] }), qc.invalidateQueries({ queryKey: ['renewals'] })]),
  })
}
