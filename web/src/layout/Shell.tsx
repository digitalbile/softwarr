import { AppShell, Badge, Burger, Divider, Group, Menu, NavLink, Text, UnstyledButton } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import {
  IconActivity,
  IconChecklist,
  IconInbox,
  IconLibrary,
  IconSearch,
  IconServer,
  IconSettings,
  IconUser,
} from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { Link, Outlet, useLocation, useNavigate } from 'react-router'
import { api } from '../api/client'
import type { Request } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { useEvents } from '../events/useEvents'
import { NotificationsBell } from './NotificationsBell'

function usePendingCount(enabled: boolean) {
  const { data } = useQuery({
    queryKey: ['requests', 'pending'],
    queryFn: () => api<Request[]>('/requests?all=1'),
    enabled,
  })
  return data?.filter((r) => r.status === 'pending').length ?? 0
}

export function Shell() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const [opened, { toggle, close }] = useDisclosure()
  useEvents()
  const pending = usePendingCount(!!user?.isAdmin)

  const link = (to: string, label: string, icon: React.ReactNode, badge?: number) => (
    <NavLink
      component={Link}
      to={to}
      label={label}
      leftSection={icon}
      rightSection={
        badge ? (
          <Badge size="xs" circle>
            {badge}
          </Badge>
        ) : undefined
      }
      active={pathname === to || pathname.startsWith(`${to}/`)}
      onClick={close}
    />
  )

  return (
    <AppShell
      header={{ height: 48 }}
      navbar={{ width: 220, breakpoint: 'sm', collapsed: { mobile: !opened } }}
      padding="md"
    >
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Group gap="sm">
            <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" aria-label="Toggle navigation" />
            <Text fw={700}>softwarr</Text>
          </Group>
          <Group gap="xs">
            <NotificationsBell />
            <Menu position="bottom-end">
              <Menu.Target>
                <UnstyledButton aria-label="User menu">
                  <Group gap={6}>
                    <IconUser size={16} />
                    <Text size="sm">{user?.username}</Text>
                  </Group>
                </UnstyledButton>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Item component={Link} to="/profile">
                  Profile
                </Menu.Item>
                <Menu.Item
                  onClick={async () => {
                    await logout()
                    navigate('/login')
                  }}
                >
                  Sign out
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>
      <AppShell.Navbar p="xs">
        {link('/search', 'Search', <IconSearch size={16} />)}
        {link('/library', 'Library', <IconLibrary size={16} />)}
        {link('/requests', 'My requests', <IconInbox size={16} />)}
        {user?.isAdmin && (
          <>
            <Divider my="xs" />
            {link('/admin/requests', 'Requests', <IconChecklist size={16} />, pending)}
            {link('/activity', 'Activity', <IconActivity size={16} />)}
            {link('/settings', 'Settings', <IconSettings size={16} />)}
            {link('/system', 'System', <IconServer size={16} />)}
          </>
        )}
      </AppShell.Navbar>
      <AppShell.Main>
        <Outlet />
      </AppShell.Main>
    </AppShell>
  )
}
