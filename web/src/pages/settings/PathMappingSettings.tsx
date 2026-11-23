import { useState } from 'react'
import { Alert, Button, Group, Loader, Modal, Stack, Table, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../api/client'
import type { PathMapping } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { messageOf } from '../../errors'

type Draft = Omit<PathMapping, 'id'>
const empty: Draft = { host: '', remotePath: '', localPath: '' }

function MappingModal({ editing, onClose }: { editing: PathMapping | 'new'; onClose: () => void }) {
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Draft>(editing === 'new' ? empty : editing)
  const [error, setError] = useState('')
  const save = useMutation({
    mutationFn: () =>
      editing === 'new'
        ? api('/pathmappings', { method: 'POST', json: draft })
        : api(`/pathmappings/${editing.id}`, { method: 'PUT', json: draft }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['pathmappings'] })
      onClose()
    },
    onError: (err) => setError(messageOf(err)),
  })
  const field = (k: keyof Draft) => ({ value: draft[k], onChange: (e: { currentTarget: { value: string } }) => setDraft({ ...draft, [k]: e.currentTarget.value }) })

  return (
    <Modal opened onClose={onClose} title={editing === 'new' ? 'Add path mapping' : 'Edit path mapping'}>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          setError('')
          save.mutate()
        }}
      >
        <Stack gap="sm">
          <TextInput label="Host" description="The download client host this applies to." {...field('host')} />
          <TextInput label="Remote path" description="The path as the download client sees it." {...field('remotePath')} />
          <TextInput label="Local path" description="The same folder as softwarr sees it." {...field('localPath')} />
          {error && <Alert color="red" role="alert">{error}</Alert>}
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>Cancel</Button>
            <Button type="submit" loading={save.isPending}>Save</Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  )
}

export function PathMappingSettings() {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<PathMapping | 'new' | null>(null)
  const [deleting, setDeleting] = useState<PathMapping | null>(null)
  const list = useQuery({ queryKey: ['pathmappings'], queryFn: () => api<PathMapping[]>('/pathmappings') })

  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Title order={4}>Remote path mappings</Title>
        <Button onClick={() => setEditing('new')}>Add mapping</Button>
      </Group>
      <Text size="sm" c="dimmed">
        Use these when the download client sees your files at a different path than softwarr does.
      </Text>
      {list.error && <Alert color="red">{list.error.message}</Alert>}
      {list.isLoading && <Loader size="sm" />}
      {list.data?.length === 0 && <Text c="dimmed">No path mappings.</Text>}
      {!!list.data?.length && (
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Host</Table.Th>
              <Table.Th>Remote path</Table.Th>
              <Table.Th>Local path</Table.Th>
              <Table.Th />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {list.data.map((m) => (
              <Table.Tr key={m.id}>
                <Table.Td>{m.host}</Table.Td>
                <Table.Td>{m.remotePath}</Table.Td>
                <Table.Td>{m.localPath}</Table.Td>
                <Table.Td>
                  <Group gap="xs" justify="flex-end">
                    <Button variant="default" size="compact-xs" aria-label={`Edit mapping for ${m.host}`} onClick={() => setEditing(m)}>Edit</Button>
                    <Button variant="default" color="red" size="compact-xs" aria-label={`Delete mapping for ${m.host}`} onClick={() => setDeleting(m)}>Delete</Button>
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      {editing && <MappingModal editing={editing} onClose={() => setEditing(null)} />}
      <ConfirmDialog
        opened={deleting !== null}
        title="Delete path mapping"
        message="Delete this path mapping?"
        confirmLabel="Delete"
        onClose={() => setDeleting(null)}
        onConfirm={async () => {
          await api(`/pathmappings/${deleting!.id}`, { method: 'DELETE' })
          await queryClient.invalidateQueries({ queryKey: ['pathmappings'] })
        }}
      />
    </Stack>
  )
}
