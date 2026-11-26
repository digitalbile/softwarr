import { Alert, Button, Group, Loader, MultiSelect, Stack, TextInput, Title } from '@mantine/core'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../api/client'
import type { Indexer, SearchResult } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { ReleaseTable } from '../components/ReleaseTable'
import { RequestDialog } from '../components/RequestDialog'

interface Submitted {
  q: string
  indexerIds: number[]
}

export function SearchPage() {
  const { user } = useAuth()
  const queryClient = useQueryClient()
  const [text, setText] = useState('')
  const [picked, setPicked] = useState<string[]>([])
  const [submitted, setSubmitted] = useState<Submitted | null>(null)
  const [selected, setSelected] = useState<SearchResult | null>(null)

  const indexers = useQuery({
    queryKey: ['indexers'],
    queryFn: () => api<Indexer[]>('/indexers'),
    enabled: !!user?.isAdmin,
    retry: false,
  })

  const queryKey = ['search', submitted?.q ?? '', submitted?.indexerIds ?? []]
  const search = useQuery({
    queryKey,
    queryFn: () => {
      const params = new URLSearchParams({ q: submitted!.q })
      if (submitted!.indexerIds.length > 0) params.set('indexerIds', submitted!.indexerIds.join(','))
      return api<SearchResult[]>(`/search?${params}`)
    },
    enabled: submitted !== null,
    retry: false,
    // Searching hits every indexer, so only run it when asked to.
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const q = text.trim()
    if (!q) return
    const next = { q, indexerIds: picked.map(Number).sort((a, b) => a - b) }
    // The same search again won't change the query key, so fetch it explicitly.
    if (submitted && JSON.stringify(submitted) === JSON.stringify(next)) void search.refetch()
    else setSubmitted(next)
  }

  const markRequested = (r: SearchResult) =>
    queryClient.setQueryData<SearchResult[]>(queryKey, (old) =>
      old?.map((x) => (x.key === r.key ? { ...x, status: 'requested' } : x)),
    )

  const indexerOptions = (indexers.data ?? [])
    .filter((i) => i.enabled)
    .map((i) => ({ value: String(i.id), label: i.name }))

  return (
    <Stack gap="md">
      <Title order={3}>Search</Title>
      <form onSubmit={onSubmit}>
        <Group align="flex-end" gap="sm">
          <TextInput
            label="Search for software"
            value={text}
            onChange={(e) => setText(e.currentTarget.value)}
            style={{ flex: 1, minWidth: 220 }}
          />
          {indexerOptions.length > 0 && (
            <MultiSelect
              label="Indexers"
              placeholder="All indexers"
              data={indexerOptions}
              value={picked}
              onChange={setPicked}
              clearable
            />
          )}
          {/* Match the inputs' height so the button lines up with them. */}
          <Button type="submit" size="sm" loading={search.isFetching}>
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
      {search.data && <ReleaseTable results={search.data} onSelect={setSelected} />}
      <RequestDialog
        result={selected}
        commentRequired={!!user?.commentRequired}
        onClose={() => setSelected(null)}
        onRequested={markRequested}
      />
    </Stack>
  )
}
