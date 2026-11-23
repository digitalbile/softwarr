import { useState } from 'react'
import { Alert, Badge, Button, Checkbox, Group, Loader, Modal, PasswordInput, Stack, Switch, Table, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../api/client'
import type { User } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { messageOf } from '../../errors'

export function signInType(u: User) {
  if (u.hasPassword && u.hasOidc) return 'Local + SSO'
  if (u.hasOidc) return 'SSO'
  return 'Local'
}

const statusColor: Record<string, string> = { active: 'green', pending: 'yellow', disabled: 'gray' }

function CreateUserModal({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient()
  const [d, setD] = useState({ username: '', email: '', password: '', isAdmin: false })
  const [error, setError] = useState('')
  const create = useMutation({
    mutationFn: () => api('/users', { method: 'POST', json: { ...d, username: d.username.trim(), email: d.email.trim() } }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['users'] })
      onClose()
    },
    onError: (err) => setError(messageOf(err)),
  })
  return (
    <Modal opened onClose={onClose} title="Add local user">
      <form
        onSubmit={(e) => {
          e.preventDefault()
          setError('')
          create.mutate()
        }}
      >
        <Stack gap="sm">
          <TextInput label="Username" value={d.username} onChange={(e) => setD({ ...d, username: e.currentTarget.value })} />
          <TextInput label="Email" value={d.email} onChange={(e) => setD({ ...d, email: e.currentTarget.value })} />
          <PasswordInput label="Password" value={d.password} onChange={(e) => setD({ ...d, password: e.currentTarget.value })} />
          <Checkbox label="Admin" checked={d.isAdmin} onChange={(e) => setD({ ...d, isAdmin: e.currentTarget.checked })} />
          {error && <Alert color="red" role="alert">{error}</Alert>}
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>Cancel</Button>
            <Button type="submit" loading={create.isPending}>Create</Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  )
}

export function UsersSettings() {
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [deleting, setDeleting] = useState<User | null>(null)
  const [error, setError] = useState('')
  const list = useQuery({ queryKey: ['users'], queryFn: () => api<User[]>('/users') })

  // Runs a user action, showing the server's message (for example the owner
  // and has-requests conflicts) and refetching either way.
  const run = async (fn: () => Promise<unknown>) => {
    setError('')
    try {
      await fn()
    } catch (err) {
      setError(messageOf(err))
    } finally {
      await queryClient.invalidateQueries({ queryKey: ['users'] })
    }
  }
  const patch = (u: User, json: Record<string, unknown>) => run(() => api(`/users/${u.id}`, { method: 'PATCH', json }))

  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Title order={4}>Users</Title>
        <Button onClick={() => setCreating(true)}>Add user</Button>
      </Group>
      {error && <Alert color="red" role="alert">{error}</Alert>}
      {list.error && <Alert color="red">{list.error.message}</Alert>}
      {list.isLoading && <Loader size="sm" />}
      {!!list.data?.length && (
        <Table.ScrollContainer minWidth={760}>
          <Table>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Username</Table.Th>
                <Table.Th>Email</Table.Th>
                <Table.Th>Sign-in</Table.Th>
                <Table.Th>Admin</Table.Th>
                <Table.Th>Auto-approve</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {list.data.map((u) => (
                <Table.Tr key={u.id}>
                  <Table.Td>
                    <Group gap="xs">
                      <Text size="sm">{u.username}</Text>
                      {u.isOwner && <Badge variant="light">Owner</Badge>}
                    </Group>
                  </Table.Td>
                  <Table.Td>{u.email}</Table.Td>
                  <Table.Td>{signInType(u)}</Table.Td>
                  <Table.Td>
                    <Switch aria-label={`Admin ${u.username}`} checked={u.isAdmin} disabled={u.isOwner} onChange={(e) => patch(u, { isAdmin: e.currentTarget.checked })} />
                  </Table.Td>
                  <Table.Td>
                    <Switch aria-label={`Auto-approve ${u.username}`} checked={u.autoApprove} onChange={(e) => patch(u, { autoApprove: e.currentTarget.checked })} />
                  </Table.Td>
                  <Table.Td>
                    <Badge color={statusColor[u.status] ?? 'gray'} variant="light">{u.status}</Badge>
                  </Table.Td>
                  <Table.Td>
                    {!u.isOwner && (
                      <Group gap="xs" justify="flex-end" wrap="nowrap">
                        {u.status === 'pending' && (
                          <Button size="compact-xs" aria-label={`Approve ${u.username}`} onClick={() => run(() => api(`/users/${u.id}/approve`, { method: 'POST' }))}>
                            Approve
                          </Button>
                        )}
                        {u.status === 'active' && (
                          <Button variant="default" size="compact-xs" aria-label={`Disable ${u.username}`} onClick={() => patch(u, { status: 'disabled' })}>
                            Disable
                          </Button>
                        )}
                        {u.status === 'disabled' && (
                          <Button variant="default" size="compact-xs" aria-label={`Enable ${u.username}`} onClick={() => patch(u, { status: 'active' })}>
                            Enable
                          </Button>
                        )}
                        <Button variant="default" color="red" size="compact-xs" aria-label={`Delete ${u.username}`} onClick={() => setDeleting(u)}>
                          Delete
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
      {creating && <CreateUserModal onClose={() => setCreating(false)} />}
      <ConfirmDialog
        opened={deleting !== null}
        title="Delete user"
        message={`Delete ${deleting?.username ?? 'this user'}? This can't be undone.`}
        confirmLabel="Delete"
        onClose={() => setDeleting(null)}
        onConfirm={async () => {
          try {
            await api(`/users/${deleting!.id}`, { method: 'DELETE' })
          } finally {
            await queryClient.invalidateQueries({ queryKey: ['users'] })
          }
        }}
      />
    </Stack>
  )
}
