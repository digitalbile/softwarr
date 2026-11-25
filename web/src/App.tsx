import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { urlBase } from './api/client'
import { AuthProvider } from './auth/AuthProvider'
import { RequireAdmin, RequireAuth } from './auth/guards'
import { Shell } from './layout/Shell'
import { ActivityPage } from './pages/admin/ActivityPage'
import { RequestsQueuePage } from './pages/admin/RequestsQueuePage'
import { SystemPage } from './pages/admin/SystemPage'
import { ChangePasswordPage } from './pages/ChangePasswordPage'
import { LibraryItemPage } from './pages/LibraryItemPage'
import { LibraryPage } from './pages/LibraryPage'
import { LoginPage } from './pages/LoginPage'
import { MyRequestsPage } from './pages/MyRequestsPage'
import { ProfilePage } from './pages/ProfilePage'
import { SearchPage } from './pages/SearchPage'
import { SetupPage } from './pages/SetupPage'
import { SettingsLayout } from './pages/settings/SettingsLayout'

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/setup" element={<SetupPage />} />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/change-password" element={<ChangePasswordPage />} />
      <Route element={<RequireAuth />}>
        <Route element={<Shell />}>
          <Route path="/" element={<Navigate to="/search" replace />} />
          <Route path="/search" element={<SearchPage />} />
          <Route path="/library" element={<LibraryPage />} />
          <Route path="/library/:id" element={<LibraryItemPage />} />
          <Route path="/requests" element={<MyRequestsPage />} />
          <Route path="/profile" element={<ProfilePage />} />
          <Route element={<RequireAdmin />}>
            <Route path="/admin/requests" element={<RequestsQueuePage />} />
            <Route path="/activity" element={<ActivityPage />} />
            <Route path="/settings/*" element={<SettingsLayout />} />
            <Route path="/system" element={<SystemPage />} />
          </Route>
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

export default function App() {
  return (
    <BrowserRouter basename={urlBase}>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  )
}
