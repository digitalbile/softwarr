import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { Button, Table, Text, UnstyledButton } from '@mantine/core'
import type { SearchResult } from '../api/types'
import { formatAge, formatBytes } from '../format'
import { StatusBadge } from './StatusBadge'

type SortKey = 'title' | 'size' | 'age' | 'peers'

const sortValue: Record<SortKey, (r: SearchResult) => string | number> = {
  title: (r) => r.title.toLowerCase(),
  size: (r) => r.size,
  age: (r) => Date.parse(r.publishDate) || 0,
  peers: (r) => r.seeders ?? r.grabs ?? -1,
}

function Header({ label, k, sort, onSort }: { label: string; k: SortKey; sort: { key: SortKey; dir: 1 | -1 }; onSort(k: SortKey): void }) {
  const active = sort.key === k
  return (
    <Table.Th aria-sort={active ? (sort.dir === 1 ? 'ascending' : 'descending') : 'none'}>
      <UnstyledButton fz="inherit" fw="inherit" onClick={() => onSort(k)}>
        {label}
        {active ? (sort.dir === 1 ? ' ▲' : ' ▼') : ''}
      </UnstyledButton>
    </Table.Th>
  )
}

function peers(r: SearchResult) {
  if (r.seeders != null) return `${r.seeders} / ${r.leechers ?? 0}`
  if (r.grabs != null) return `${r.grabs} grabs`
  return ''
}

interface Props {
  results: SearchResult[]
  onSelect(result: SearchResult): void
  // Which rows get the action button. Defaults to requestable ones.
  selectable?(result: SearchResult): boolean
  actionLabel?: string
}

export function ReleaseTable({ results, onSelect, selectable = (r) => r.status === 'requestable', actionLabel = 'Request' }: Props) {
  // Newest first by default, so a larger age timestamp sorts first.
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: 'age', dir: -1 })
  const rows = useMemo(() => {
    const val = sortValue[sort.key]
    return [...results].sort((a, b) => {
      const x = val(a)
      const y = val(b)
      return (x < y ? -1 : x > y ? 1 : 0) * sort.dir
    })
  }, [results, sort])

  const onSort = (key: SortKey) =>
    setSort((s) => (s.key === key ? { key, dir: (s.dir * -1) as 1 | -1 } : { key, dir: key === 'title' ? 1 : -1 }))

  if (results.length === 0) return <Text c="dimmed">No results.</Text>

  return (
    <Table.ScrollContainer minWidth={760}>
      <Table highlightOnHover verticalSpacing={4} fz="sm">
        <Table.Thead>
          <Table.Tr>
            <Header label="Title" k="title" sort={sort} onSort={onSort} />
            <Table.Th>Indexer</Table.Th>
            <Table.Th>Protocol</Table.Th>
            <Header label="Size" k="size" sort={sort} onSort={onSort} />
            <Header label="Age" k="age" sort={sort} onSort={onSort} />
            <Header label="Seeders / leechers" k="peers" sort={sort} onSort={onSort} />
            <Table.Th>Status</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {rows.map((r) => (
            <Table.Tr key={r.key}>
              <Table.Td>{r.title}</Table.Td>
              <Table.Td>{r.indexer}</Table.Td>
              <Table.Td>{r.protocol}</Table.Td>
              <Table.Td>{formatBytes(r.size)}</Table.Td>
              <Table.Td>{formatAge(r.publishDate)}</Table.Td>
              <Table.Td>{peers(r)}</Table.Td>
              <Table.Td>
                {selectable(r) ? (
                  <Button size="compact-xs" onClick={() => onSelect(r)} aria-label={`${actionLabel} ${r.title}`}>
                    {actionLabel}
                  </Button>
                ) : r.status === 'available' && r.libraryItemId != null ? (
                  <Link to={`/library/${r.libraryItemId}`} style={{ textDecoration: 'none' }}>
                    <StatusBadge status="available" style={{ cursor: 'pointer' }} />
                  </Link>
                ) : (
                  <StatusBadge status={r.status} />
                )}
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  )
}
