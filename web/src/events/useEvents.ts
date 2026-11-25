import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { api, urlBase } from '../api/client'

const invalidations: Record<string, string[]> = {
  'request.updated': ['requests', 'activity'],
  notification: ['notifications'],
  // Not 'search': re-running a Prowlarr search on every import is costly.
  'library.updated': ['library'],
  activity: ['activity'],
  health: ['health'],
}

// Reopen delays for a stream the browser gave up on.
const reopenMinDelay = 2000
const reopenMaxDelay = 30000

// The browser's EventSource.CLOSED. Not read from the global, which tests replace.
const CLOSED = 2

// Opens one event stream for the signed-in session. The browser reconnects on
// its own after a dropped connection, but gives up for good if a reconnect is
// refused, for example by a 401 that raced a new session cookie. Then the
// stream is reopened here, with a backoff that resets once a stream opens.
export function useEvents() {
  const queryClient = useQueryClient()
  useEffect(() => {
    let source: EventSource | null = null
    let timer: ReturnType<typeof setTimeout> | undefined
    let delay = reopenMinDelay
    let stopped = false

    const open = () => {
      const es = new EventSource(`${urlBase}/api/v1/events`)
      source = es
      for (const [type, keys] of Object.entries(invalidations)) {
        es.addEventListener(type, () => {
          for (const key of keys) void queryClient.invalidateQueries({ queryKey: [key] })
        })
      }
      es.addEventListener('open', () => {
        delay = reopenMinDelay
      })
      es.addEventListener('error', () => {
        if (stopped || es !== source || es.readyState !== CLOSED) return
        es.close()
        // The stream may have closed because the session is gone. A 401 here
        // signs the app out, which unmounts this hook and stops the retries.
        void api('/auth/me').catch(() => {})
        timer = setTimeout(() => {
          if (!stopped) open()
        }, delay)
        delay = Math.min(delay * 2, reopenMaxDelay)
      })
    }

    open()
    return () => {
      stopped = true
      clearTimeout(timer)
      source?.close()
    }
  }, [queryClient])
}
