import { Alert, Anchor, Button, Group, Loader, Stack, Table, Text, TextInput, Title, UnstyledButton } from '@mantine/core'
import { useDebouncedValue } from '@mantine/hooks'
import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { api, urlBase } from '../api/client'
import type { LibraryItem } from '../api/types'
import { formatAge, formatBytes } from '../format'

type SortKey = 'title' | 'totalSize' | 'importedAt' | 'requester'

function compare(a: LibraryItem, b: LibraryItem, key: SortKey): number {
  switch (key) {
    case 'title':
      return a.title.localeCompare(b.title)
    case 'requester':
      return a.requester.localeCompare(b.requester)
    case 'totalSize':
      return a.totalSize - b.totalSize
    case 'importedAt':
      return Date.parse(a.importedAt) - Date.parse(b.importedAt)
  }
}

export function LibraryPage() {
  const [filter, setFilter] = useState('')
  const [debounced] = useDebouncedValue(filter.trim(), 300)
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'importedAt', desc: true })

  const list = useQuery({
    queryKey: ['library', debounced],
    queryFn: () => api<LibraryItem[]>(`/library?q=${encodeURIComponent(debounced)}`),
    placeholderData: (prev) => prev,
  })

  const items = useMemo(() => {
    const sorted = [...(list.data ?? [])].sort((a, b) => compare(a, b, sort.key))
    return sort.desc ? sorted.reverse() : sorted
  }, [list.data, sort])

  function header(label: string, key: SortKey) {
    const active = sort.key === key
    return (
      <Table.Th aria-sort={active ? (sort.desc ? 'descending' : 'ascending') : undefined}>
        <UnstyledButton
          fz="sm"
          fw={700}
          onClick={() => setSort({ key, desc: active ? !sort.desc : key === 'importedAt' })}
        >
          {label}
          {active ? (sort.desc ? ' ▾' : ' ▴') : ''}
        </UnstyledButton>
      </Table.Th>
    )
  }

  return (
    <Stack gap="md">
      <Group justify="space-between">
        <Title order={3}>Library</Title>
        <TextInput
          size="xs"
          placeholder="Filter library"
          aria-label="Filter library"
          value={filter}
          onChange={(e) => setFilter(e.currentTarget.value)}
          w={260}
        />
      </Group>
      {list.error && <Alert color="red">{list.error.message}</Alert>}
      {list.isLoading && <Loader size="sm" />}
      {list.data && items.length === 0 && (
        <Text c="dimmed">{debounced ? 'Nothing in the library matches that.' : 'Nothing in the library yet.'}</Text>
      )}
      {items.length > 0 && (
        <Table.ScrollContainer minWidth={640}>
          <Table verticalSpacing={4} fz="sm">
            <Table.Thead>
              <Table.Tr>
                {header('Title', 'title')}
                {header('Size', 'totalSize')}
                {header('Added', 'importedAt')}
                {header('Requested by', 'requester')}
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {items.map((it) => (
                <Table.Tr key={it.id}>
                  <Table.Td>
                    <Anchor size="sm" component={Link} to={`/library/${it.id}`}>
                      {it.title}
                    </Anchor>
                    {it.releaseName !== it.title && (
                      <Text size="xs" c="dimmed">
                        {it.releaseName}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>{formatBytes(it.totalSize)}</Table.Td>
                  <Table.Td>{formatAge(it.importedAt)} ago</Table.Td>
                  <Table.Td>{it.requester}</Table.Td>
                  <Table.Td>
                    <Button
                      component="a"
                      href={`${urlBase}/api/v1/library/${it.id}/download`}
                      download
                      size="compact-xs"
                      variant="light"
                    >
                      Download
                    </Button>
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
