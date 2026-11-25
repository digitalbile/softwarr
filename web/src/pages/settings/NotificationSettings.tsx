import {
  Alert,
  Badge,
  Button,
  Checkbox,
  Group,
  Loader,
  Modal,
  PasswordInput,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  Title,
} from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../../api/client'
import type { Webhook } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { messageOf } from '../../errors'
import { TestButton, type TestOutcome } from './TestButton'

const eventOptions = [
  { value: 'request.created', label: 'Request created' },
  { value: 'request.approved', label: 'Request approved' },
  { value: 'request.denied', label: 'Request denied' },
  { value: 'request.failed', label: 'Request failed' },
  { value: 'item.available', label: 'Item available' },
  { value: 'item.expired', label: 'Item expired' },
  { value: 'health.warning', label: 'Health warning' },
]

function WebhookModal({ editing, onClose }: { editing: Webhook | 'new'; onClose: () => void }) {
  const queryClient = useQueryClient()
  const existing = editing === 'new' ? null : editing
  const [name, setName] = useState(existing?.name ?? '')
  const [url, setUrl] = useState(existing?.url ?? '')
  const [secret, setSecret] = useState('')
  const [clearSecret, setClearSecret] = useState(false)
  const [events, setEvents] = useState<string[]>(existing?.events ?? [])
  const [enabled, setEnabled] = useState(existing?.enabled ?? true)
  const [error, setError] = useState('')

  const body = () => ({
    name: name.trim(),
    url: url.trim(),
    secret,
    clearSecret,
    // Keep a stable order regardless of click order.
    events: eventOptions.map((o) => o.value).filter((v) => events.includes(v)),
    enabled,
  })
  const save = useMutation({
    mutationFn: () =>
      existing
        ? api(`/webhooks/${existing.id}`, { method: 'PUT', json: body() })
        : api('/webhooks', { method: 'POST', json: body() }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['webhooks'] })
      onClose()
    },
    onError: (err) => setError(messageOf(err)),
  })

  return (
    <Modal opened onClose={onClose} title={existing ? 'Edit webhook' : 'Add webhook'}>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          setError('')
          save.mutate()
        }}
      >
        <Stack gap="sm">
          <TextInput label="Name" value={name} onChange={(e) => setName(e.currentTarget.value)} />
          <TextInput label="URL" placeholder="https://" value={url} onChange={(e) => setUrl(e.currentTarget.value)} />
          <PasswordInput
            label="Secret"
            description={
              existing?.hasSecret
                ? 'A secret is set. Leave this blank to keep it.'
                : 'Optional. Used to sign each delivery.'
            }
            value={secret}
            onChange={(e) => setSecret(e.currentTarget.value)}
          />
          {existing?.hasSecret && (
            <Checkbox
              label="Remove the saved secret"
              checked={clearSecret}
              onChange={(e) => setClearSecret(e.currentTarget.checked)}
            />
          )}
          <Checkbox.Group label="Events" value={events} onChange={setEvents}>
            <Stack gap={4} mt={4}>
              {eventOptions.map((o) => (
                <Checkbox key={o.value} value={o.value} label={o.label} />
              ))}
            </Stack>
          </Checkbox.Group>
          <Switch label="Enabled" checked={enabled} onChange={(e) => setEnabled(e.currentTarget.checked)} />
          {error && (
            <Alert color="red" role="alert">
              {error}
            </Alert>
          )}
          <Group>
            <Button type="submit" loading={save.isPending}>
              Save
            </Button>
            {existing ? (
              <TestButton
                okText="Delivered."
                run={() => api<TestOutcome>(`/webhooks/${existing.id}/test`, { method: 'POST' })}
              />
            ) : (
              <Text size="xs" c="dimmed">
                Save the webhook to send a test.
              </Text>
            )}
            <Button variant="default" ml="auto" onClick={onClose}>
              Cancel
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  )
}

export function NotificationSettings() {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<Webhook | 'new' | null>(null)
  const [deleting, setDeleting] = useState<Webhook | null>(null)
  const list = useQuery({ queryKey: ['webhooks'], queryFn: () => api<Webhook[]>('/webhooks') })

  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Title order={4}>Webhooks</Title>
        <Button onClick={() => setEditing('new')}>Add webhook</Button>
      </Group>
      {list.error && <Alert color="red">{list.error.message}</Alert>}
      {list.isLoading && <Loader size="sm" />}
      {list.data?.length === 0 && <Text c="dimmed">No webhooks yet.</Text>}
      {!!list.data?.length && (
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Name</Table.Th>
              <Table.Th>URL</Table.Th>
              <Table.Th>Events</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {list.data.map((h) => (
              <Table.Tr key={h.id}>
                <Table.Td>{h.name}</Table.Td>
                <Table.Td>{h.url}</Table.Td>
                <Table.Td>{h.events.length}</Table.Td>
                <Table.Td>
                  <Badge color={h.enabled ? 'green' : 'gray'} variant="light">
                    {h.enabled ? 'Enabled' : 'Disabled'}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Group gap="xs" justify="flex-end">
                    <Button
                      variant="default"
                      size="compact-xs"
                      aria-label={`Edit ${h.name}`}
                      onClick={() => setEditing(h)}
                    >
                      Edit
                    </Button>
                    <Button
                      variant="default"
                      color="red"
                      size="compact-xs"
                      aria-label={`Delete ${h.name}`}
                      onClick={() => setDeleting(h)}
                    >
                      Delete
                    </Button>
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      {editing && <WebhookModal editing={editing} onClose={() => setEditing(null)} />}
      <ConfirmDialog
        opened={deleting !== null}
        title="Delete webhook"
        message={`Delete ${deleting?.name ?? 'this webhook'}?`}
        confirmLabel="Delete"
        onClose={() => setDeleting(null)}
        onConfirm={async () => {
          await api(`/webhooks/${deleting!.id}`, { method: 'DELETE' })
          await queryClient.invalidateQueries({ queryKey: ['webhooks'] })
        }}
      />
    </Stack>
  )
}
