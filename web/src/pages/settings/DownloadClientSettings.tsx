import {
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  Modal,
  NumberInput,
  PasswordInput,
  Select,
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
import type { DownloadClient } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { messageOf } from '../../errors'
import { TestButton } from './TestButton'

interface ClientType {
  type: string
  protocol: string
  name: string
}

function str(v: unknown, fallback = '') {
  return typeof v === 'string' ? v : fallback
}

function ClientModal({
  editing,
  types,
  onClose,
}: {
  editing: DownloadClient | 'new'
  types: ClientType[]
  onClose: () => void
}) {
  const queryClient = useQueryClient()
  const existing = editing === 'new' ? null : editing
  const s = existing?.settings ?? {}
  const [name, setName] = useState(existing?.name ?? '')
  const [type, setType] = useState<string | null>(existing?.type ?? types[0]?.type ?? null)
  const [enabled, setEnabled] = useState(existing?.enabled ?? true)
  const [priority, setPriority] = useState<number | string>(existing?.priority ?? 1)
  const [host, setHost] = useState(str(s.host))
  const [port, setPort] = useState<number | string>(typeof s.port === 'number' ? s.port : 8080)
  const [useTls, setUseTls] = useState(s.useTls === true)
  const [urlBase, setUrlBase] = useState(str(s.urlBase))
  const [username, setUsername] = useState(str(s.username))
  const [password, setPassword] = useState(str(s.password))
  const [category, setCategory] = useState(str(s.category, 'softwarr'))
  const [error, setError] = useState('')

  const body = () => ({
    name: name.trim(),
    type: type ?? '',
    enabled,
    priority: Number(priority) || 0,
    settings: {
      host: host.trim(),
      port: Number(port) || 0,
      useTls,
      urlBase: urlBase.trim(),
      username,
      password,
      category: category.trim(),
    },
  })

  const save = useMutation({
    mutationFn: () =>
      existing
        ? api(`/downloadclients/${existing.id}`, { method: 'PUT', json: body() })
        : api('/downloadclients', { method: 'POST', json: body() }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['downloadclients'] })
      onClose()
    },
    onError: (err) => setError(messageOf(err)),
  })

  return (
    <Modal opened onClose={onClose} title={existing ? 'Edit download client' : 'Add download client'}>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          setError('')
          save.mutate()
        }}
      >
        <Stack gap="sm">
          <TextInput label="Name" value={name} onChange={(e) => setName(e.currentTarget.value)} />
          <Select
            label="Type"
            data={types.map((t) => ({ value: t.type, label: t.name }))}
            value={type}
            onChange={setType}
            allowDeselect={false}
            disabled={!!existing}
          />
          <Group grow align="flex-start">
            <TextInput label="Host" value={host} onChange={(e) => setHost(e.currentTarget.value)} />
            <NumberInput label="Port" min={1} max={65535} allowDecimal={false} value={port} onChange={setPort} />
          </Group>
          <Switch label="Use TLS" checked={useTls} onChange={(e) => setUseTls(e.currentTarget.checked)} />
          <TextInput
            label="URL base"
            description="Only needed behind a reverse proxy path."
            value={urlBase}
            onChange={(e) => setUrlBase(e.currentTarget.value)}
          />
          <Group grow align="flex-start">
            <TextInput label="Username" value={username} onChange={(e) => setUsername(e.currentTarget.value)} />
            <PasswordInput label="Password" value={password} onChange={(e) => setPassword(e.currentTarget.value)} />
          </Group>
          <TextInput label="Category" value={category} onChange={(e) => setCategory(e.currentTarget.value)} />
          <Group grow align="flex-start">
            <NumberInput
              label="Priority"
              description="Lower numbers are tried first."
              min={0}
              allowDecimal={false}
              value={priority}
              onChange={setPriority}
            />
            <Switch mt={28} label="Enabled" checked={enabled} onChange={(e) => setEnabled(e.currentTarget.checked)} />
          </Group>
          {error && (
            <Alert color="red" role="alert">
              {error}
            </Alert>
          )}
          <Group>
            <Button type="submit" loading={save.isPending}>
              Save
            </Button>
            <TestButton
              run={() =>
                api('/downloadclients/test', {
                  method: 'POST',
                  json: { ...body(), ...(existing ? { id: existing.id } : {}) },
                })
              }
            />
            <Button variant="default" ml="auto" onClick={onClose}>
              Cancel
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  )
}

export function DownloadClientSettings() {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<DownloadClient | 'new' | null>(null)
  const [deleting, setDeleting] = useState<DownloadClient | null>(null)
  const list = useQuery({ queryKey: ['downloadclients'], queryFn: () => api<DownloadClient[]>('/downloadclients') })
  const types = useQuery({
    queryKey: ['downloadclients', 'types'],
    queryFn: () => api<ClientType[]>('/downloadclients/types'),
  })

  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Title order={4}>Download clients</Title>
        <Button onClick={() => setEditing('new')} disabled={!types.data}>
          Add client
        </Button>
      </Group>
      {list.error && <Alert color="red">{list.error.message}</Alert>}
      {list.isLoading && <Loader size="sm" />}
      {list.data?.length === 0 && (
        <Text c="dimmed">No download clients yet. Add one so approved requests can download.</Text>
      )}
      {!!list.data?.length && (
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Name</Table.Th>
              <Table.Th>Type</Table.Th>
              <Table.Th>Protocol</Table.Th>
              <Table.Th>Priority</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {list.data.map((c) => (
              <Table.Tr key={c.id}>
                <Table.Td>{c.name}</Table.Td>
                <Table.Td>{types.data?.find((t) => t.type === c.type)?.name ?? c.type}</Table.Td>
                <Table.Td>{c.protocol}</Table.Td>
                <Table.Td>{c.priority}</Table.Td>
                <Table.Td>
                  <Badge color={c.enabled ? 'green' : 'gray'} variant="light">
                    {c.enabled ? 'Enabled' : 'Disabled'}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Group gap="xs" justify="flex-end">
                    <Button
                      variant="default"
                      size="compact-xs"
                      aria-label={`Edit ${c.name}`}
                      onClick={() => setEditing(c)}
                    >
                      Edit
                    </Button>
                    <Button
                      variant="default"
                      color="red"
                      size="compact-xs"
                      aria-label={`Delete ${c.name}`}
                      onClick={() => setDeleting(c)}
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
      {editing && types.data && <ClientModal editing={editing} types={types.data} onClose={() => setEditing(null)} />}
      <ConfirmDialog
        opened={deleting !== null}
        title="Delete download client"
        message={`Delete ${deleting?.name ?? 'this client'}? Downloads already sent to it stay there.`}
        confirmLabel="Delete"
        onClose={() => setDeleting(null)}
        onConfirm={async () => {
          await api(`/downloadclients/${deleting!.id}`, { method: 'DELETE' })
          await queryClient.invalidateQueries({ queryKey: ['downloadclients'] })
        }}
      />
    </Stack>
  )
}
