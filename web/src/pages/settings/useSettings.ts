import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../api/client'
import type { Settings } from '../../api/types'

export function useSettings() {
  return useQuery({ queryKey: ['settings'], queryFn: () => api<Settings>('/settings') })
}

// PUT /settings replaces the whole object, so fetch the latest copy, merge
// this section's changes into it, and send all of it.
export function useSaveSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (change: (current: Settings) => Settings) => {
      const current = await api<Settings>('/settings')
      return api<Settings>('/settings', { method: 'PUT', json: change(current) })
    },
    onSuccess: (saved) => {
      queryClient.setQueryData(['settings'], saved)
      return queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
  })
}
