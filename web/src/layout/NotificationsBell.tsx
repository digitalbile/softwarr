import { ActionIcon, Badge, Button, Group, Popover, Stack, Text } from '@mantine/core'
import { IconBell } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Notification } from '../api/types'

export function NotificationsBell() {
  const queryClient = useQueryClient()
  const unread = useQuery({
    queryKey: ['notifications', 'unread'],
    queryFn: () => api<Notification[]>('/notifications?unread=1'),
  })
  const latest = useQuery({
    queryKey: ['notifications', 'latest'],
    queryFn: () => api<Notification[]>('/notifications'),
  })
  const markAll = useMutation({
    mutationFn: () => api('/notifications/read', { method: 'POST', json: { all: true } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['notifications'] }),
  })

  const count = unread.data?.length ?? 0
  const items = (latest.data ?? []).slice(0, 10)

  return (
    <Popover position="bottom-end" width={340} shadow="md">
      <Popover.Target>
        <ActionIcon variant="subtle" color="gray" aria-label={count > 0 ? `Notifications, ${count} unread` : 'Notifications'}>
          <IconBell size={18} />
          {count > 0 && (
            <Badge size="xs" circle color="red" pos="absolute" top={-4} right={-4}>
              {count}
            </Badge>
          )}
        </ActionIcon>
      </Popover.Target>
      <Popover.Dropdown p="xs">
        <Group justify="space-between" mb="xs">
          <Text size="sm" fw={500}>
            Notifications
          </Text>
          <Button variant="subtle" disabled={count === 0} loading={markAll.isPending} onClick={() => markAll.mutate()}>
            Mark all as read
          </Button>
        </Group>
        {items.length === 0 ? (
          <Text size="sm" c="dimmed">
            Nothing yet.
          </Text>
        ) : (
          <Stack gap={6}>
            {items.map((n) => (
              <div key={n.id}>
                <Text size="sm" fw={n.read ? 400 : 600}>
                  {n.message}
                </Text>
                <Text size="xs" c="dimmed">
                  {new Date(n.createdAt * 1000).toLocaleString()}
                </Text>
              </div>
            ))}
          </Stack>
        )}
      </Popover.Dropdown>
    </Popover>
  )
}
