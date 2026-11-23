import { Center, Loader } from '@mantine/core'
import { Navigate, Outlet } from 'react-router'
import { useAuth } from './useAuth'

export function FullPageLoader() {
  return (
    <Center h="100vh">
      <Loader size="sm" />
    </Center>
  )
}

// Everything inside the shell needs a signed-in user who has a usable password.
export function RequireAuth() {
  const { user, loading, setupNeeded } = useAuth()
  if (loading) return <FullPageLoader />
  if (setupNeeded) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace />
  if (user.mustChangePassword) return <Navigate to="/change-password" replace />
  return <Outlet />
}

export function RequireAdmin() {
  const { user } = useAuth()
  if (!user?.isAdmin) return <Navigate to="/" replace />
  return <Outlet />
}
