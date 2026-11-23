import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { expect, test } from 'vitest'
import { makeUser, server } from '../../test/server'
import { renderApp, useApiServer } from '../../test/utils'

useApiServer()

function setup() {
  server.use(
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ isAdmin: true }))),
    http.get('*/api/v1/system/health', () =>
      HttpResponse.json([
        { id: 'a', level: 'ok', message: 'Library is writable.' },
        { id: 'b', level: 'warning', message: 'Disk is nearly full.' },
        { id: 'c', level: 'error', message: 'Prowlarr is unreachable.' },
      ]),
    ),
    http.get('*/api/v1/system/status', () =>
      HttpResponse.json({ version: '1.2.3', goVersion: 'go1.27', dataDir: '/data', urlBase: '', startedAt: '2026-01-01T00:00:00Z' }),
    ),
    http.get('*/api/v1/system/logs', () =>
      HttpResponse.json([
        { time: '2026-01-01T00:00:00Z', level: 'ERROR', message: 'boom' },
        { time: '2026-01-01T00:00:01Z', level: 'INFO', message: 'started' },
      ]),
    ),
  )
  renderApp('/system')
}

test('system page renders health levels', async () => {
  setup()
  const ok = (await screen.findByText('Library is writable.')).closest('[data-level]')
  expect(ok).toHaveAttribute('data-level', 'ok')
  expect(screen.getByText('Disk is nearly full.').closest('[data-level]')).toHaveAttribute('data-level', 'warning')
  expect(screen.getByText('Prowlarr is unreachable.').closest('[data-level]')).toHaveAttribute('data-level', 'error')
  expect(await screen.findByText('1.2.3')).toBeInTheDocument()
})

test('log level filter hides lower levels', async () => {
  setup()
  await screen.findByText('started')
  await userEvent.click(screen.getByRole('radio', { name: 'Error' }))
  expect(screen.queryByText('started')).not.toBeInTheDocument()
  expect(within(screen.getByText('boom').closest('tr')!).getByText('ERROR')).toBeInTheDocument()
})
