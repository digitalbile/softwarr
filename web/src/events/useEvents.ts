import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { urlBase } from '../api/client'

const invalidations: Record<string, string[]> = {
  'request.updated': ['requests', 'activity'],
  notification: ['notifications'],
  // Not 'search': re-running a Prowlarr search on every import is costly.
  'library.updated': ['library'],
  activity: ['activity'],
  health: ['health'],
}

// Opens one event stream for the signed-in session. The browser reconnects on its own.
export function useEvents() {
  const queryClient = useQueryClient()
  useEffect(() => {
    const source = new EventSource(`${urlBase}/api/v1/events`)
    for (const [type, keys] of Object.entries(invalidations)) {
      source.addEventListener(type, () => {
        for (const key of keys) void queryClient.invalidateQueries({ queryKey: [key] })
      })
    }
    return () => source.close()
  }, [queryClient])
}
