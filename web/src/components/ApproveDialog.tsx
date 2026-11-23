import { useState } from 'react'
import { Alert, Button, Group, Loader, Modal, Stack, Text, TextInput } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { api, isConflict } from '../api/client'
import type { Release, Request, SearchResult } from '../api/types'
import { toRelease } from '../releases'
import { ReleaseTable } from './ReleaseTable'

export interface ApproveBody {
  displayTitle?: string
  replacement?: Release
}

interface Props {
  request: Request | null
  onClose(): void
  onApprove(id: number, body: ApproveBody): Promise<void>
}

export function ApproveDialog({ request, onClose, onApprove }: Props) {
  return (
    <Modal opened={request !== null} onClose={onClose} title="Approve this request" size="xl" centered>
      {request && <Form key={request.id} request={request} onClose={onClose} onApprove={onApprove} />}
    </Modal>
  )
}

const canPick = (r: SearchResult) => r.status !== 'available' && r.status !== 'no_client'

function Form({ request, onClose, onApprove }: Props & { request: Request }) {
  const [title, setTitle] = useState('')
  const [query, setQuery] = useState(request.release.title)
  const [submitted, setSubmitted] = useState<string | null>(null)
  const [chosen, setChosen] = useState<SearchResult | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const search = useQuery({
    queryKey: ['search', 'replacement', submitted],
    queryFn: () => api<SearchResult[]>(`/search?${new URLSearchParams({ q: submitted! })}`),
    enabled: submitted !== null,
    retry: false,
  })

  const runSearch = (e: React.FormEvent) => {
    e.preventDefault()
    e.stopPropagation()
    const q = query.trim()
    if (q) setSubmitted(q)
  }

  const approve = async () => {
    const body: ApproveBody = {}
    if (title.trim()) body.displayTitle = title.trim()
    if (chosen) body.replacement = toRelease(chosen)
    setBusy(true)
    setError('')
    try {
      await onApprove(request.id, body)
      onClose()
    } catch (err) {
      // The page shows conflicts, so close and let it.
      if (isConflict(err)) onClose()
      else setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Stack gap="sm">
      {error && (
        <Alert color="red" role="alert">
          {error}
        </Alert>
      )}
      <Text fw={500}>{request.release.title}</Text>
      <TextInput
        label="Display title"
        placeholder={request.release.title}
        value={title}
        onChange={(e) => setTitle(e.currentTarget.value)}
      />
      <Text fw={500} mt="xs">
        Use a different release
      </Text>
      <form onSubmit={runSearch}>
        <Group align="flex-end" gap="sm">
          <TextInput
            label="Search for a replacement"
            value={query}
            onChange={(e) => setQuery(e.currentTarget.value)}
            style={{ flex: 1 }}
          />
          <Button type="submit" variant="default" loading={search.isFetching}>
            Search
          </Button>
        </Group>
      </form>
      {search.error && (
        <Alert color="red" role="alert">
          {search.error.message}
        </Alert>
      )}
      {search.isLoading && <Loader size="sm" />}
      {search.data && <ReleaseTable results={search.data} onSelect={setChosen} selectable={canPick} actionLabel="Use" />}
      {chosen && (
        <Group gap="xs">
          <Text size="sm">Replacement: {chosen.title}</Text>
          <Button size="compact-xs" variant="default" onClick={() => setChosen(null)}>
            Clear
          </Button>
        </Group>
      )}
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>
          Cancel
        </Button>
        <Button onClick={approve} loading={busy}>
          Approve
        </Button>
      </Group>
    </Stack>
  )
}
