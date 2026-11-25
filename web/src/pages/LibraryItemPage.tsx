import {
  Alert,
  Anchor,
  Button,
  Card,
  Group,
  Loader,
  Modal,
  NumberInput,
  SegmentedControl,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { ApiError, api, urlBase } from '../api/client'
import type { LibraryItem } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { formatBytes } from '../format'

type RetentionMode = 'default' | 'never' | 'custom'

function modeOf(days: number | null): RetentionMode {
  return days === null ? 'default' : days === 0 ? 'never' : 'custom'
}

function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Table.Tr>
      <Table.Th w={160}>{label}</Table.Th>
      <Table.Td>{children}</Table.Td>
    </Table.Tr>
  )
}

function AdminEditor({ item }: { item: LibraryItem }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [title, setTitle] = useState(item.title)
  const [mode, setMode] = useState<RetentionMode>(modeOf(item.retentionDays))
  const [custom, setCustom] = useState<number | string>(
    item.retentionDays && item.retentionDays > 0 ? item.retentionDays : 30,
  )
  const [confirming, setConfirming] = useState(false)

  useEffect(() => {
    setTitle(item.title)
    setMode(modeOf(item.retentionDays))
    if (item.retentionDays && item.retentionDays > 0) setCustom(item.retentionDays)
  }, [item.title, item.retentionDays])

  const customDays = typeof custom === 'number' && Number.isInteger(custom) && custom >= 1 ? custom : null
  const days = mode === 'default' ? null : mode === 'never' ? 0 : customDays

  const save = useMutation({
    mutationFn: (body: Record<string, unknown>) => api(`/library/${item.id}`, { method: 'PATCH', json: body }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['library'] }),
  })
  const remove = useMutation({
    mutationFn: () => api(`/library/${item.id}`, { method: 'DELETE' }),
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: ['library', 'item', String(item.id)] })
      navigate('/library')
      queryClient.invalidateQueries({ queryKey: ['library'] })
    },
  })

  function onSave() {
    const body: Record<string, unknown> = {}
    if (title !== item.title) body.title = title.trim() === '' ? null : title.trim()
    if (mode !== 'custom' || customDays !== null) {
      if (days !== item.retentionDays) body.retentionDays = days
    }
    if (Object.keys(body).length > 0) save.mutate(body)
  }

  return (
    <Card withBorder padding="md">
      <Stack gap="sm">
        <Title order={5}>Edit</Title>
        {save.error && <Alert color="red">{save.error.message}</Alert>}
        <TextInput
          label="Title"
          description="Leave empty to use the release name."
          value={title}
          onChange={(e) => setTitle(e.currentTarget.value)}
        />
        <div>
          <Text size="sm" fw={500} mb={4}>
            Retention
          </Text>
          <Group gap="sm" align="flex-end">
            <SegmentedControl
              size="xs"
              value={mode}
              onChange={(v) => setMode(v as RetentionMode)}
              data={[
                { value: 'default', label: 'Use default' },
                { value: 'never', label: 'Never' },
                { value: 'custom', label: 'Custom days' },
              ]}
            />
            {mode === 'custom' && (
              <NumberInput
                aria-label="Days"
                size="xs"
                w={90}
                min={1}
                allowDecimal={false}
                allowNegative={false}
                value={custom}
                onChange={setCustom}
              />
            )}
          </Group>
        </div>
        <Group>
          <Button
            size="xs"
            onClick={onSave}
            loading={save.isPending}
            disabled={mode === 'custom' && customDays === null}
          >
            Save
          </Button>
          <Button size="xs" color="red" variant="light" onClick={() => setConfirming(true)}>
            Delete
          </Button>
        </Group>
      </Stack>
      <Modal
        opened={confirming}
        onClose={() => {
          setConfirming(false)
          remove.reset()
        }}
        title="Delete item"
        size="sm"
      >
        <Stack gap="md">
          <Text size="sm">
            Delete this item from the library? Seeding files in the download client aren't affected.
          </Text>
          {remove.error && <Alert color="red">{remove.error.message}</Alert>}
          <Group justify="flex-end">
            <Button
              variant="default"
              size="xs"
              onClick={() => {
                setConfirming(false)
                remove.reset()
              }}
            >
              Cancel
            </Button>
            <Button color="red" size="xs" loading={remove.isPending} onClick={() => remove.mutate()}>
              Delete
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Card>
  )
}

export function LibraryItemPage() {
  const { id } = useParams()
  const { user } = useAuth()
  const q = useQuery({
    queryKey: ['library', 'item', id],
    queryFn: () => api<LibraryItem>(`/library/${id}`),
    retry: false,
  })

  if (q.isLoading) return <Loader size="sm" />
  if (q.error) {
    const missing = q.error instanceof ApiError && q.error.status === 404
    return (
      <Stack gap="sm">
        <Alert color={missing ? 'gray' : 'red'}>{missing ? "That library item doesn't exist." : q.error.message}</Alert>
        <Anchor component={Link} to="/library" size="sm">
          Back to library
        </Anchor>
      </Stack>
    )
  }
  const it = q.data
  if (!it) return null

  return (
    <Stack gap="md" maw={760}>
      <Group justify="space-between" align="flex-start">
        <div>
          <Title order={3}>{it.title}</Title>
          {it.releaseName !== it.title && (
            <Text size="sm" c="dimmed">
              {it.releaseName}
            </Text>
          )}
        </div>
        <Button component="a" href={`${urlBase}/api/v1/library/${it.id}/download`} download size="xs">
          Download
        </Button>
      </Group>

      <Table fz="sm" verticalSpacing={4}>
        <Table.Tbody>
          <Detail label="Size">{formatBytes(it.totalSize)}</Detail>
          <Detail label="Requested by">{it.requester}</Detail>
          {it.comment && <Detail label="Comment">{it.comment}</Detail>}
          <Detail label="Approved by">{it.approver}</Detail>
          <Detail label="Import mode">{it.importMode}</Detail>
          <Detail label="Added">{new Date(it.importedAt).toLocaleDateString()}</Detail>
          <Detail label="Downloads">{it.downloadCount}</Detail>
          <Detail label="Expires">
            {it.expiresAt ? new Date(it.expiresAt).toLocaleDateString() : 'Never expires'}
          </Detail>
        </Table.Tbody>
      </Table>

      <div>
        <Title order={5} mb={4}>
          Files
        </Title>
        <Table fz="sm" verticalSpacing={2}>
          <Table.Tbody>
            {it.files.map((f) => (
              <Table.Tr key={f.path}>
                <Table.Td>{f.path}</Table.Td>
                <Table.Td ta="right">{formatBytes(f.size)}</Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </div>

      {user?.isAdmin && <AdminEditor item={it} />}
    </Stack>
  )
}
