import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../api/client'
import type { User } from '../api/types'
import { AuthContext, type AuthState } from './useAuth'

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [setupNeeded, setSetupNeeded] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const setup = await api<{ needed: boolean }>('/setup')
      setSetupNeeded(setup.needed)
      if (setup.needed) {
        setUser(null)
        return
      }
      setUser(await api<User>('/auth/me'))
    } catch (err) {
      // A 401 just means nobody is signed in. Other failures leave the user signed out too.
      if (!(err instanceof ApiError)) console.error(err)
      setUser(null)
    } finally {
      setLoading(false)
    }
  }, [])

  const logout = useCallback(async () => {
    try {
      await api('/auth/logout', { method: 'POST' })
    } finally {
      setUser(null)
      queryClient.clear()
    }
  }, [queryClient])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    const onUnauthorized = () => {
      setUser(null)
      queryClient.clear()
    }
    window.addEventListener('softwarr:unauthorized', onUnauthorized)
    return () => window.removeEventListener('softwarr:unauthorized', onUnauthorized)
  }, [queryClient])

  const value = useMemo<AuthState>(
    () => ({ user, loading, setupNeeded, refresh, logout }),
    [user, loading, setupNeeded, refresh, logout],
  )
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
