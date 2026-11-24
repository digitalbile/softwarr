import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../api/client'
import type { User } from '../api/types'
import { AuthContext, type AuthState } from './useAuth'

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [setupNeeded, setSetupNeeded] = useState(false)
  const userRef = useRef<User | null>(null)
  useEffect(() => {
    userRef.current = user
  }, [user])

  const refresh = useCallback(async () => {
    try {
      // The setup check only decides whether to show the setup page. If it
      // fails, carry on and ask who is signed in rather than signing out.
      let needed = false
      try {
        needed = (await api<{ needed: boolean }>('/setup')).needed
      } catch (err) {
        console.error(err)
      }
      setSetupNeeded(needed)
      if (needed) {
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
    // An admin reset the password mid-session: reload the user so the guard
    // sends them to the change password page. Only once, while the app still
    // thinks the password is fine, so a refresh that hits the gate can't loop.
    const onPasswordChangeRequired = () => {
      if (userRef.current && !userRef.current.mustChangePassword) void refresh()
    }
    window.addEventListener('softwarr:unauthorized', onUnauthorized)
    window.addEventListener('softwarr:password-change-required', onPasswordChangeRequired)
    return () => {
      window.removeEventListener('softwarr:unauthorized', onUnauthorized)
      window.removeEventListener('softwarr:password-change-required', onPasswordChangeRequired)
    }
  }, [queryClient, refresh])

  const value = useMemo<AuthState>(
    () => ({ user, loading, setupNeeded, refresh, logout }),
    [user, loading, setupNeeded, refresh, logout],
  )
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
