import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { expect, test } from 'vitest'
import type { Activity } from '../../api/types'
import { makeUser, server } from '../../test/server'
import { renderApp, useApiServer } from '../../test/utils'

useApiServer()

function act(over: Partial<Activity>): Activity {
  return { requestId: 1, title: 'App', requester: 'sam', status: 'downloading', progress: 0.5, stalled: false, error: '', updatedAt: '', ...over }
}

function setup(rows: Activity[], extra: Parameters<typeof server.use> = []) {
  server.use(
    ...extra,
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ isAdmin: true }))),
    http.get('*/api/v1/activity', () => HttpResponse.json(rows)),
  )
  renderApp('/activity')
}

const row = (t: string) => screen.getByText(t).closest('tr')!

test('activity shows stalled badge and retry for failed', async () => {
  let retried = ''
  setup(
    [
      act({ requestId: 1, title: 'Slow app', stalled: true }),
      act({ requestId: 2, title: 'Broken app', status: 'failed', error: 'Disk full', progress: 0.2 }),
      act({ requestId: 3, title: 'Importing app', status: 'importing', progress: 1 }),
    ],
    [http.post('*/api/v1/requests/:id/retry', ({ params }) => { retried = String(params.id); return HttpResponse.json({}) })],
  )
  await screen.findByText('Slow app')
  expect(within(row('Slow app')).getByText('Stalled')).toBeInTheDocument()
  expect(within(row('Slow app')).getByRole('progressbar')).toHaveAttribute('aria-valuenow', '50')
  expect(within(row('Slow app')).queryByRole('button', { name: /Retry/ })).not.toBeInTheDocument()
  expect(within(row('Broken app')).getByText('Disk full')).toBeInTheDocument()
  expect(within(row('Importing app')).queryByRole('button')).not.toBeInTheDocument()
  await userEvent.click(within(row('Broken app')).getByRole('button', { name: 'Retry Broken app' }))
  await waitFor(() => expect(retried).toBe('2'))
})

test('abandon asks for confirmation first', async () => {
  let abandoned = 0
  setup([act({ requestId: 4, title: 'Gone app' })], [http.post('*/api/v1/requests/4/abandon', () => { abandoned++; return HttpResponse.json({}) })])
  await userEvent.click(await screen.findByRole('button', { name: 'Abandon Gone app' }))
  expect(await screen.findByText('Stop tracking this download? The torrent stays in the download client.')).toBeInTheDocument()
  expect(abandoned).toBe(0)
  const dialog = screen.getByRole('dialog')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Abandon' }))
  await waitFor(() => expect(abandoned).toBe(1))
})

test('conflict on retry shows the message and refetches', async () => {
  let fetches = 0
  setup([], [
    http.get('*/api/v1/activity', () => { fetches++; return HttpResponse.json([act({ requestId: 2, title: 'Broken app', status: 'failed' })]) }),
    http.post('*/api/v1/requests/2/retry', () => HttpResponse.json({ error: 'That request has already moved on.' }, { status: 409 })),
  ])
  await userEvent.click(await screen.findByRole('button', { name: 'Retry Broken app' }))
  expect(await screen.findByText('That request has already moved on.')).toBeInTheDocument()
  await waitFor(() => expect(fetches).toBeGreaterThan(1))
})
