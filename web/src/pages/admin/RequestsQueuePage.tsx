import { useState } from 'react'
import { Alert, Button, Group, Loader, SegmentedControl, Stack, Table, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../api/client'
import type { Request } from '../../api/types'
import { ApproveDialog } from '../../components/ApproveDialog'
import { DenyDialog } from '../../components/DenyDialog'
import { StatusBadge } from '../../components/StatusBadge'
import { formatAge, formatBytes } from '../../format'
import { useRequestAction } from './useRequestAction'

const filters = [
  { value: 'all', label: 'All' },
  { value: 'pending', label: 'Pending' },
  { value: 'progress', label: 'In progress' },
  { value: 'done', label: 'Done' },
  { value: 'failed', label: 'Failed or denied' },
]

const groups: Record<string, string[]> = {
  pending: ['pending'],
  progress: ['approved', 'downloading', 'importing', 'scanning'],
  done: ['available'],
  failed: ['failed', 'denied', 'abandoned', 'cancelled'],
}

function sortQueue(list: Request[]) {
  return [...list].sort((a, b) => {
    const ap = a.status === 'pending' ? 0 : 1
    const bp = b.status === 'pending' ? 0 : 1
    if (ap !== bp) return ap - bp
    return (Date.parse(b.updatedAt) || 0) - (Date.parse(a.updatedAt) || 0)
  })
}

export function RequestsQueuePage() {
  const [filter, setFilter] = useState('all')
  const [approving, setApproving] = useState<Request | null>(null)
  const [denying, setDenying] = useState<Request | null>(null)
  const { perform, conflict } = useRequestAction()
  const list = useQuery({ queryKey: ['requests', 'all'], queryFn: () => api<Request[]>('/requests?all=1') })

  const rows = sortQueue(list.data ?? []).filter((r) => filter === 'all' || groups[filter].includes(r.status))

  return (
    <Stack gap="md">
      <Group justify="space-between">
        <Title order={3}>Requests</Title>
        <SegmentedControl aria-label="Filter by status" size="xs" data={filters} value={filter} onChange={setFilter} />
      </Group>
      {conflict && (
        <Alert color="red" role="alert">
          {conflict}
        </Alert>
      )}
      {list.error && <Alert color="red">{list.error.message}</Alert>}
      {list.isLoading && <Loader size="sm" />}
      {list.data && rows.length === 0 && <Text c="dimmed">No requests here.</Text>}
      {rows.length > 0 && (
        <Table.ScrollContainer minWidth={760}>
          <Table verticalSpacing={4} fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Release</Table.Th>
                <Table.Th>Size</Table.Th>
                <Table.Th>Indexer</Table.Th>
                <Table.Th>Requester</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th>Age</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {rows.map((r) => (
                <Table.Tr key={r.id}>
                  <Table.Td>
                    {r.displayTitle || r.release.title}
                    {r.status === 'pending' && r.comment && (
                      <Text size="xs" c="dimmed">
                        {r.comment}
                      </Text>
                    )}
                    {r.status === 'denied' && r.denyReason && (
                      <Text size="xs" c="dimmed">
                        Reason: {r.denyReason}
                      </Text>
                    )}
                    {r.error && (
                      <Text size="xs" c="red">
                        {r.error}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>{formatBytes(r.release.size)}</Table.Td>
                  <Table.Td>{r.release.indexer}</Table.Td>
                  <Table.Td>{r.requester}</Table.Td>
                  <Table.Td>
                    <StatusBadge status={r.status} />
                  </Table.Td>
                  <Table.Td>{formatAge(r.createdAt)}</Table.Td>
                  <Table.Td>
                    {r.status === 'pending' && (
                      <Group gap="xs" wrap="nowrap">
                        <Button size="compact-xs" onClick={() => setApproving(r)} aria-label={`Approve ${r.displayTitle || r.release.title}`}>
                          Approve
                        </Button>
                        <Button size="compact-xs" variant="default" onClick={() => setDenying(r)} aria-label={`Deny ${r.displayTitle || r.release.title}`}>
                          Deny
                        </Button>
                      </Group>
                    )}
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
      <ApproveDialog request={approving} onClose={() => setApproving(null)} onApprove={(id, body) => perform(id, 'approve', body)} />
      <DenyDialog request={denying} onClose={() => setDenying(null)} onDeny={(id, body) => perform(id, 'deny', body)} />
    </Stack>
  )
}
