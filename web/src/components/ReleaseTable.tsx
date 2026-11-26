import { Button, Group, Table, Text, Tooltip, UnstyledButton } from '@mantine/core'
import { IconArrowDown, IconArrowUp } from '@tabler/icons-react'
import { type ReactNode, useMemo, useState } from 'react'
import { Link } from 'react-router'
import type { SearchResult } from '../api/types'
import { formatAge, formatBytes, formatCategories, shortCategories } from '../format'
import { StatusBadge } from './StatusBadge'

type SortKey = 'title' | 'category' | 'size' | 'age' | 'indexer' | 'seeders' | 'leechers'

const sortValue: Record<SortKey, (r: SearchResult) => string | number> = {
  title: (r) => r.title.toLowerCase(),
  category: (r) => shortCategories(r.categories).toLowerCase(),
  size: (r) => r.size,
  age: (r) => Date.parse(r.publishDate) || 0,
  indexer: (r) => r.indexer.toLowerCase(),
  // Usenet results have grabs in place of seeders.
  seeders: (r) => r.seeders ?? r.grabs ?? -1,
  leechers: (r) => r.leechers ?? -1,
}

// Text columns sort A to Z first; numbers sort largest first.
const textKeys: SortKey[] = ['title', 'category', 'indexer']

const nowrap = { whiteSpace: 'nowrap' } as const

function Header({
  label,
  tooltip,
  k,
  sort,
  onSort,
}: {
  label: ReactNode
  // Set when the label is an icon: names the column for the tooltip and screen readers.
  tooltip?: string
  k: SortKey
  sort: { key: SortKey; dir: 1 | -1 }
  onSort(k: SortKey): void
}) {
  const active = sort.key === k
  const button = (
    <UnstyledButton fz="inherit" fw="inherit" onClick={() => onSort(k)} aria-label={tooltip}>
      <Group gap={2} wrap="nowrap">
        {label}
        {active ? (sort.dir === 1 ? ' ▲' : ' ▼') : ''}
      </Group>
    </UnstyledButton>
  )
  return (
    <Table.Th style={nowrap} aria-sort={active ? (sort.dir === 1 ? 'ascending' : 'descending') : 'none'}>
      {tooltip ? <Tooltip label={tooltip}>{button}</Tooltip> : button}
    </Table.Th>
  )
}

function Categories({ ids }: { ids: number[] }) {
  const short = shortCategories(ids)
  const full = formatCategories(ids)
  if (short === full) return short
  return (
    <Tooltip label={full} multiline maw={320}>
      <span>{short}</span>
    </Tooltip>
  )
}

function Seeders({ r }: { r: SearchResult }) {
  if (r.seeders != null) return r.seeders
  if (r.grabs != null)
    return (
      <Tooltip label="Grabs">
        <span>{r.grabs}</span>
      </Tooltip>
    )
  return null
}

interface Props {
  results: SearchResult[]
  onSelect(result: SearchResult): void
  // Which rows get the action button. Defaults to requestable ones.
  selectable?(result: SearchResult): boolean
  actionLabel?: string
}

export function ReleaseTable({
  results,
  onSelect,
  selectable = (r) => r.status === 'requestable',
  actionLabel = 'Request',
}: Props) {
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
    setSort((s) =>
      s.key === key ? { key, dir: (s.dir * -1) as 1 | -1 } : { key, dir: textKeys.includes(key) ? 1 : -1 },
    )

  if (results.length === 0) return <Text c="dimmed">No results.</Text>

  return (
    <Table.ScrollContainer minWidth={720}>
      <Table highlightOnHover verticalSpacing={4} fz="sm">
        <Table.Thead>
          <Table.Tr>
            <Header label="Title" k="title" sort={sort} onSort={onSort} />
            <Header label="Category" k="category" sort={sort} onSort={onSort} />
            <Header label="Size" k="size" sort={sort} onSort={onSort} />
            <Header label="Age" k="age" sort={sort} onSort={onSort} />
            <Header label="Indexer" k="indexer" sort={sort} onSort={onSort} />
            <Header label={<IconArrowUp size={16} />} tooltip="Seeders" k="seeders" sort={sort} onSort={onSort} />
            <Header label={<IconArrowDown size={16} />} tooltip="Leechers" k="leechers" sort={sort} onSort={onSort} />
            <Table.Th>Status</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {rows.map((r) => (
            // The same release can come from more than one indexer.
            <Table.Tr key={`${r.key}|${r.indexerId}|${r.guid}`}>
              {/* Release names often have no spaces, so let them break anywhere. */}
              <Table.Td style={{ overflowWrap: 'anywhere' }}>{r.title}</Table.Td>
              <Table.Td style={nowrap}>
                <Categories ids={r.categories} />
              </Table.Td>
              <Table.Td style={nowrap}>{formatBytes(r.size)}</Table.Td>
              <Table.Td style={nowrap}>{formatAge(r.publishDate)}</Table.Td>
              <Table.Td style={nowrap}>{r.indexer}</Table.Td>
              <Table.Td>
                <Seeders r={r} />
              </Table.Td>
              <Table.Td>{r.leechers}</Table.Td>
              <Table.Td style={nowrap}>
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
