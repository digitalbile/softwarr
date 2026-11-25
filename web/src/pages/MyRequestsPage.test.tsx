import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { expect, test } from 'vitest'
import type { Request } from '../api/types'
import { makeUser, server } from '../test/server'
import { renderApp, setupApiServer } from '../test/utils'

setupApiServer()

function req(over: Partial<Request>): Request {
  return {
    id: 1,
    requesterId: 2,
    requester: 'sam',
    status: 'pending',
    release: { title: 'Release one' } as Request['release'],
    originalRelease: null,
    displayTitle: '',
    denyReason: '',
    error: '',
    progress: 0,
    libraryItemId: null,
    createdAt: '2026-11-20T10:00:00Z',
    updatedAt: '2026-11-20T10:00:00Z',
    ...over,
  }
}

test('my requests shows progress for downloading and cancel for pending', async () => {
  let cancelled = ''
  let list = [
    req({ id: 1, status: 'pending', displayTitle: 'Pending app' }),
    req({
      id: 2,
      status: 'downloading',
      progress: 0.4,
      release: { title: 'Downloading release' } as Request['release'],
    }),
    req({ id: 3, status: 'denied', denyReason: 'Not licensed', displayTitle: 'Denied app' }),
    req({ id: 4, status: 'failed', error: 'Disk full', displayTitle: 'Failed app' }),
  ]
  server.use(
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser())),
    http.get('*/api/v1/requests', () => HttpResponse.json(list)),
    http.post('*/api/v1/requests/:id/cancel', ({ params }) => {
      cancelled = String(params.id)
      list = list.map((r) => (r.id === 1 ? { ...r, status: 'cancelled' } : r))
      return HttpResponse.json(list[0])
    }),
  )
  renderApp('/requests')
  const row = (t: string) => screen.getByText(t).closest('tr')!

  await screen.findByText('Pending app')
  expect(within(row('Downloading release')).getByRole('progressbar')).toHaveAttribute('aria-valuenow', '40')
  expect(within(row('Pending app')).queryByRole('progressbar')).not.toBeInTheDocument()
  expect(within(row('Downloading release')).queryByRole('button', { name: 'Cancel' })).not.toBeInTheDocument()
  expect(within(row('Denied app')).getByText('Reason: Not licensed')).toBeInTheDocument()
  expect(within(row('Failed app')).getByText('Disk full')).toBeInTheDocument()

  await userEvent.click(within(row('Pending app')).getByRole('button', { name: 'Cancel' }))
  expect(await within(row('Pending app')).findByText('Cancelled')).toBeInTheDocument()
  expect(cancelled).toBe('1')
})
