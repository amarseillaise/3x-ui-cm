import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, PANEL_WRITE_TIMEOUT_MS } from '../api'
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
    // Extends the subscription on the panel, so it gets the longer deadline.
    mutationFn: (planId: string) => api<RenewResponse>('/api/renew', { method: 'POST', body: JSON.stringify({ planId }) }, PANEL_WRITE_TIMEOUT_MS),
    onSettled: () => Promise.all([qc.invalidateQueries({ queryKey: ['me'] }), qc.invalidateQueries({ queryKey: ['plans'] }), qc.invalidateQueries({ queryKey: ['renewals'] })]),
  })
}
