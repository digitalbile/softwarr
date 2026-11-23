import { useState } from 'react'
import { Alert, Badge, Group, Loader, SegmentedControl, Stack, Table, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../api/client'
import type { HealthCheck, HealthLevel, LogEntry } from '../../api/types'

interface Status {
  version: string
  goVersion: string
  dataDir: string
  urlBase: string
  startedAt: string
}

const levelColor: Record<HealthLevel, string> = { ok: 'green', warning: 'yellow', error: 'red' }

const logLevels = [
  { value: 'all', label: 'All' },
  { value: 'debug', label: 'Debug' },
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warn' },
  { value: 'error', label: 'Error' },
]
const rank: Record<string, number> = { all: 0, debug: 0, info: 1, warn: 2, warning: 2, error: 3 }

function logColor(level: string) {
  const r = rank[level.toLowerCase()] ?? 0
  return r >= 3 ? 'red' : r === 2 ? 'yellow' : 'gray'
}

export function SystemPage() {
  const [level, setLevel] = useState('all')
  const health = useQuery({ queryKey: ['health'], queryFn: () => api<HealthCheck[]>('/system/health') })
  const status = useQuery({ queryKey: ['system', 'status'], queryFn: () => api<Status>('/system/status') })
  const logs = useQuery({
    queryKey: ['system', 'logs'],
    queryFn: () => api<LogEntry[]>('/system/logs'),
    refetchInterval: 10_000,
  })

  const rows = (logs.data ?? []).filter((e) => (rank[e.level.toLowerCase()] ?? 0) >= rank[level])

  return (
    <Stack gap="lg">
      <Title order={3}>System</Title>

      <Stack gap="xs">
        <Title order={4}>Health</Title>
        {health.error && <Alert color="red">{health.error.message}</Alert>}
        {health.isLoading && <Loader size="sm" />}
        {health.data?.map((c) => (
          <Alert key={c.id} data-level={c.level} color={levelColor[c.level] ?? 'gray'} p="xs">
            {c.message}
          </Alert>
        ))}
      </Stack>

      <Stack gap="xs">
        <Title order={4}>Status</Title>
        {status.error && <Alert color="red">{status.error.message}</Alert>}
        {status.data && (
          <Table w="auto">
            <Table.Tbody>
              <Table.Tr><Table.Th>Version</Table.Th><Table.Td>{status.data.version}</Table.Td></Table.Tr>
              <Table.Tr><Table.Th>Go version</Table.Th><Table.Td>{status.data.goVersion}</Table.Td></Table.Tr>
              <Table.Tr><Table.Th>Data directory</Table.Th><Table.Td>{status.data.dataDir}</Table.Td></Table.Tr>
              <Table.Tr><Table.Th>URL base</Table.Th><Table.Td>{status.data.urlBase || '/'}</Table.Td></Table.Tr>
              <Table.Tr><Table.Th>Started</Table.Th><Table.Td>{new Date(status.data.startedAt).toLocaleString()}</Table.Td></Table.Tr>
            </Table.Tbody>
          </Table>
        )}
      </Stack>

      <Stack gap="xs">
        <Group justify="space-between">
          <Title order={4}>Logs</Title>
          <SegmentedControl aria-label="Filter by level" size="xs" data={logLevels} value={level} onChange={setLevel} />
        </Group>
        {logs.error && <Alert color="red">{logs.error.message}</Alert>}
        {logs.isLoading && <Loader size="sm" />}
        {logs.data && rows.length === 0 && <Text c="dimmed">No log entries.</Text>}
        {rows.length > 0 && (
          <Table.ScrollContainer minWidth={640}>
            <Table>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Time</Table.Th>
                  <Table.Th>Level</Table.Th>
                  <Table.Th>Message</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rows.map((e, i) => (
                  <Table.Tr key={`${e.time}-${i}`}>
                    <Table.Td style={{ whiteSpace: 'nowrap' }}>{new Date(e.time).toLocaleTimeString()}</Table.Td>
                    <Table.Td>
                      <Badge color={logColor(e.level)} variant="light">{e.level}</Badge>
                    </Table.Td>
                    <Table.Td>
                      {e.message}
                      {e.attrs && Object.keys(e.attrs).length > 0 && (
                        <Text size="xs" c="dimmed" ff="monospace">
                          {Object.entries(e.attrs).map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`).join(' ')}
                        </Text>
                      )}
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Stack>
    </Stack>
  )
}
