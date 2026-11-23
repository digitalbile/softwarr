import { useState } from 'react'
import { Alert, Badge, Button, Group, Loader, Modal, Progress, Stack, Table, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { api, isConflict } from '../../api/client'
import type { Activity } from '../../api/types'
import { StatusBadge } from '../../components/StatusBadge'
import { useRequestAction } from './useRequestAction'

export function ActivityPage() {
  const { perform, conflict } = useRequestAction()
  const [abandoning, setAbandoning] = useState<Activity | null>(null)
  const [actionError, setActionError] = useState('')
  const list = useQuery({ queryKey: ['activity'], queryFn: () => api<Activity[]>('/activity') })

  const run = async (id: number, action: string) => {
    setActionError('')
    try {
      await perform(id, action)
    } catch (err) {
      // Conflicts are shown through `conflict`.
      if (!isConflict(err)) setActionError((err as Error).message)
    }
  }

  const confirmAbandon = async () => {
    const a = abandoning
    setAbandoning(null)
    if (a) await run(a.requestId, 'abandon')
  }

  const rows = list.data ?? []
  return (
    <Stack gap="md">
      <Title order={3}>Activity</Title>
      {(conflict || actionError) && (
        <Alert color="red" role="alert">
          {conflict || actionError}
        </Alert>
      )}
      {list.error && <Alert color="red">{list.error.message}</Alert>}
      {list.isLoading && <Loader size="sm" />}
      {list.data && rows.length === 0 && <Text c="dimmed">Nothing is downloading right now.</Text>}
      {rows.length > 0 && (
        <Table.ScrollContainer minWidth={700}>
          <Table verticalSpacing={4} fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Title</Table.Th>
                <Table.Th>Requester</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th w={200}>Progress</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {rows.map((a) => {
                const pct = Math.round(a.progress * 100)
                return (
                  <Table.Tr key={a.requestId}>
                    <Table.Td>
                      {a.title}
                      {a.error && (
                        <Text size="xs" c="red">
                          {a.error}
                        </Text>
                      )}
                    </Table.Td>
                    <Table.Td>{a.requester}</Table.Td>
                    <Table.Td>
                      <Group gap={4} wrap="nowrap">
                        <StatusBadge status={a.status} />
                        {a.stalled && (
                          <Badge size="sm" variant="light" color="orange">
                            Stalled
                          </Badge>
                        )}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Progress size="sm" value={pct} aria-label={`Progress ${pct}%`} />
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" wrap="nowrap">
                        {a.status === 'failed' && (
                          <Button size="compact-xs" onClick={() => run(a.requestId, 'retry')} aria-label={`Retry ${a.title}`}>
                            Retry
                          </Button>
                        )}
                        {['approved', 'downloading', 'failed'].includes(a.status) && (
                          <Button size="compact-xs" variant="default" onClick={() => setAbandoning(a)} aria-label={`Abandon ${a.title}`}>
                            Abandon
                          </Button>
                        )}
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                )
              })}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
      <Modal opened={abandoning !== null} onClose={() => setAbandoning(null)} title="Abandon this download" centered>
        <Stack gap="sm">
          <Text>Stop tracking this download? The torrent stays in the download client.</Text>
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setAbandoning(null)}>
              Cancel
            </Button>
            <Button color="red" onClick={confirmAbandon}>
              Abandon
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  )
}
