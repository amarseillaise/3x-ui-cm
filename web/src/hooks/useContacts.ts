import { useQuery } from '@tanstack/react-query'
import { api } from '../api'
import type { Contact } from '../types'

/** Footer links. They change only when config.yaml does, so fetch once per visit. */
export function useContacts() {
  return useQuery({
    queryKey: ['contacts'],
    queryFn: () => api<{ contacts: Contact[] }>('/api/contacts').then((r) => r.contacts),
    staleTime: Infinity,
    retry: false,
  })
}
