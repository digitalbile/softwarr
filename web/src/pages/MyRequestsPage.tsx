import { Alert, Button, Loader, Progress, Stack, Table, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import { api } from '../api/client'
import type { Request } from '../api/types'
import { StatusBadge } from '../components/StatusBadge'

export function MyRequestsPage() {
  const queryClient = useQueryClient()
  const list = useQuery({ queryKey: ['requests'], queryFn: () => api<Request[]>('/requests') })
  const cancel = useMutation({
    mutationFn: (id: number) => api(`/requests/${id}/cancel`, { method: 'POST' }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['requests'] }),
  })

  const requests = list.data ?? []
  return (
    <Stack gap="md">
      <Title order={3}>My requests</Title>
      {list.error && <Alert color="red">{list.error.message}</Alert>}
      {cancel.error && <Alert color="red">{cancel.error.message}</Alert>}
      {list.isLoading && <Loader size="sm" />}
      {list.data && requests.length === 0 && <Text c="dimmed">You haven't requested anything yet.</Text>}
      {requests.length > 0 && (
        <Table.ScrollContainer minWidth={640}>
          <Table verticalSpacing={4} fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Title</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th>Requested</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {requests.map((r) => (
                <Table.Tr key={r.id}>
                  <Table.Td>
                    {r.libraryItemId != null && r.status === 'available' ? (
                      <Text size="sm" component={Link} to={`/library/${r.libraryItemId}`}>
                        {r.displayTitle || r.release.title}
                      </Text>
                    ) : (
                      r.displayTitle || r.release.title
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
                  <Table.Td>
                    <StatusBadge status={r.status} />
                    {r.status === 'downloading' && (
                      <Progress
                        mt={4}
                        size="sm"
                        value={Math.round(r.progress * 100)}
                        aria-label={`Download progress ${Math.round(r.progress * 100)}%`}
                      />
                    )}
                  </Table.Td>
                  <Table.Td>{new Date(r.createdAt).toLocaleDateString()}</Table.Td>
                  <Table.Td>
                    {r.status === 'pending' && (
                      <Button size="compact-xs" variant="default" onClick={() => cancel.mutate(r.id)} loading={cancel.isPending && cancel.variables === r.id}>
                        Cancel
                      </Button>
                    )}
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
    </Stack>
  )
}
