import { Box, Group, NavLink, Stack, Title } from '@mantine/core'
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router'
import { AuthSettings } from './AuthSettings'
import { DownloadClientSettings } from './DownloadClientSettings'
import { GeneralSettings } from './GeneralSettings'
import { IndexerSettings } from './IndexerSettings'
import { LibrarySettings } from './LibrarySettings'
import { NotificationSettings } from './NotificationSettings'
import { PathMappingSettings } from './PathMappingSettings'
import { RequestSettings } from './RequestSettings'
import { UsersSettings } from './UsersSettings'

const sections = [
  { path: 'general', label: 'General', element: <GeneralSettings /> },
  { path: 'auth', label: 'Authentication', element: <AuthSettings /> },
  { path: 'indexers', label: 'Indexers', element: <IndexerSettings /> },
  { path: 'requests', label: 'Requests', element: <RequestSettings /> },
  { path: 'downloadclients', label: 'Download clients', element: <DownloadClientSettings /> },
  { path: 'pathmappings', label: 'Remote path mappings', element: <PathMappingSettings /> },
  { path: 'library', label: 'Library', element: <LibrarySettings /> },
  { path: 'notifications', label: 'Notifications', element: <NotificationSettings /> },
  { path: 'users', label: 'Users', element: <UsersSettings /> },
]

export function SettingsLayout() {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  return (
    <Stack gap="md">
      <Title order={3}>Settings</Title>
      <Group align="flex-start" wrap="nowrap" gap="lg">
        <Box component="nav" aria-label="Settings sections" w={190} style={{ flexShrink: 0 }}>
          {sections.map((s) => (
            <NavLink
              key={s.path}
              label={s.label}
              active={pathname.endsWith(`/settings/${s.path}`)}
              onClick={() => navigate(`/settings/${s.path}`)}
            />
          ))}
        </Box>
        <Box style={{ flex: 1, minWidth: 0 }}>
          <Routes>
            <Route index element={<Navigate to="general" replace />} />
            {sections.map((s) => (
              <Route key={s.path} path={s.path} element={s.element} />
            ))}
            <Route path="*" element={<Navigate to="general" replace />} />
          </Routes>
        </Box>
      </Group>
    </Stack>
  )
}
