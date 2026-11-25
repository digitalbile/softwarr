import { Alert, Button, Group, Loader, Modal, Stack, Text, TextInput } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api, isConflict } from '../api/client'
import type { SearchResult } from '../api/types'
import { ReleaseTable } from './ReleaseTable'

export interface ApproveBody {
  displayTitle?: string
  replacementToken?: string
}

// The request being approved: its id and the title to show and search for.
export interface ApproveTarget {
  id: number
  title: string
}

interface Props {
  request: ApproveTarget | null
  // 'replace' is for a failed request: a different release must be chosen.
  mode?: 'approve' | 'replace'
  onClose(): void
  onApprove(id: number, body: ApproveBody): Promise<void>
}

export function ApproveDialog({ request, mode = 'approve', onClose, onApprove }: Props) {
  const heading = mode === 'replace' ? 'Use a different release' : 'Approve this request'
  return (
    <Modal opened={request !== null} onClose={onClose} title={heading} size="xl" centered>
      {request && <Form key={request.id} request={request} mode={mode} onClose={onClose} onApprove={onApprove} />}
    </Modal>
  )
}

const canPick = (r: SearchResult) => r.status !== 'available' && r.status !== 'no_client'

function Form({ request, mode, onClose, onApprove }: Props & { request: ApproveTarget }) {
  const replacing = mode === 'replace'
  const [title, setTitle] = useState('')
  const [query, setQuery] = useState(request.title)
  const [submitted, setSubmitted] = useState<string | null>(null)
  const [chosen, setChosen] = useState<SearchResult | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const search = useQuery({
    queryKey: ['search', 'replacement', submitted],
    queryFn: () => api<SearchResult[]>(`/search?${new URLSearchParams({ q: submitted! })}`),
    enabled: submitted !== null,
    retry: false,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })

  const runSearch = (e: React.FormEvent) => {
    e.preventDefault()
    e.stopPropagation()
    const q = query.trim()
    if (!q) return
    if (q === submitted) void search.refetch()
    else setSubmitted(q)
  }

  const approve = async () => {
    const body: ApproveBody = {}
    if (title.trim()) body.displayTitle = title.trim()
    if (chosen) body.replacementToken = chosen.token
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
      <Text fw={500}>{request.title}</Text>
      <TextInput
        label="Display title"
        placeholder={request.title}
        value={title}
        onChange={(e) => setTitle(e.currentTarget.value)}
      />
      {!replacing && (
        <Text fw={500} mt="xs">
          Use a different release
        </Text>
      )}
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
      {search.data && (
        <ReleaseTable results={search.data} onSelect={setChosen} selectable={canPick} actionLabel="Use" />
      )}
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
        <Button onClick={approve} loading={busy} disabled={replacing && !chosen}>
          {replacing ? 'Use this release' : 'Approve'}
        </Button>
      </Group>
    </Stack>
  )
}
