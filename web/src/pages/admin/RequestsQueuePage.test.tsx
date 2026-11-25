import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { expect, test } from 'vitest'
import type { Request, SearchResult } from '../../api/types'
import { makeUser, server } from '../../test/server'
import { renderApp, setupApiServer } from '../../test/utils'

setupApiServer()

function req(over: Partial<Request>): Request {
  return {
    id: 1,
    requesterId: 2,
    requester: 'sam',
    status: 'pending',
    release: { title: 'Release one', indexer: 'Idx', size: 1024 ** 3, guid: 'g1' } as Request['release'],
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

function result(over: Partial<SearchResult>): SearchResult {
  return {
    guid: 'r',
    title: 'Other',
    indexer: 'Idx',
    indexerId: 1,
    protocol: 'torrent',
    size: 1024,
    publishDate: '2026-11-01T00:00:00Z',
    seeders: 1,
    leechers: 0,
    grabs: null,
    infoHash: 'abc',
    categories: [],
    key: 'k',
    status: 'requestable',
    libraryItemId: null,
    token: `tok-${over.key ?? 'k'}`,
    ...over,
  }
}

const list = [
  req({
    id: 1,
    status: 'downloading',
    release: { title: 'Busy app' } as Request['release'],
    updatedAt: '2026-11-22T10:00:00Z',
  }),
  req({
    id: 2,
    status: 'pending',
    comment: 'Need it for work',
    release: { title: 'Pending app', indexer: 'Idx', size: 1024 ** 3, guid: 'g2' } as Request['release'],
  }),
  req({
    id: 3,
    status: 'failed',
    release: { title: 'Broken app' } as Request['release'],
    updatedAt: '2026-11-21T10:00:00Z',
  }),
]

function setup(extra: Parameters<typeof server.use> = []) {
  server.use(
    ...extra,
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ isAdmin: true }))),
    http.get('*/api/v1/requests', ({ request }) => {
      expect(new URL(request.url).searchParams.get('all')).toBe('1')
      return HttpResponse.json(list)
    }),
  )
  renderApp('/admin/requests')
}

test('queue lists pending first and filters by status', async () => {
  setup()
  await screen.findByText('Pending app')
  const titles = screen
    .getAllByRole('row')
    .slice(1)
    .map((r) => within(r).getAllByRole('cell')[0].textContent)
  expect(titles[0]).toContain('Pending app')
  expect(titles[0]).toContain('Need it for work')
  expect(titles[1]).toContain('Busy app')
  expect(titles[2]).toContain('Broken app')

  await userEvent.click(screen.getByText('Failed or denied'))
  expect(screen.queryByText('Busy app')).not.toBeInTheDocument()
  expect(screen.getByText('Broken app')).toBeInTheDocument()
})

test('approve sends display title', async () => {
  let body: unknown
  setup([
    http.post('*/api/v1/requests/2/approve', async ({ request }) => {
      body = await request.json()
      return HttpResponse.json(req({ id: 2 }))
    }),
  ])
  await userEvent.click(await screen.findByRole('button', { name: 'Approve Pending app' }))
  await userEvent.type(await screen.findByLabelText('Display title'), 'Nice name')
  await userEvent.click(screen.getByRole('button', { name: 'Approve' }))
  await waitFor(() => expect(body).toEqual({ displayTitle: 'Nice name' }))
})

test('approve with replacement sends the chosen result token', async () => {
  let body: Record<string, unknown> = {}
  let q = ''
  setup([
    http.get('*/api/v1/search', ({ request }) => {
      q = new URL(request.url).searchParams.get('q')!
      return HttpResponse.json([
        result({ key: 'a', guid: 'a', title: 'Taken app', status: 'available', libraryItemId: 3 }),
        result({ key: 'b', guid: 'b', title: 'Better app' }),
      ])
    }),
    http.post('*/api/v1/requests/2/approve', async ({ request }) => {
      body = (await request.json()) as Record<string, unknown>
      return HttpResponse.json(req({ id: 2 }))
    }),
  ])
  await userEvent.click(await screen.findByRole('button', { name: 'Approve Pending app' }))
  expect(await screen.findByLabelText('Search for a replacement')).toHaveValue('Pending app')
  await userEvent.click(screen.getByRole('button', { name: 'Search' }))
  await screen.findByText('Better app')
  expect(q).toBe('Pending app')
  expect(screen.queryByRole('button', { name: 'Use Taken app' })).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Use Better app' }))
  expect(screen.getByText('Replacement: Better app')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Approve' }))
  await waitFor(() => expect(body).toEqual({ replacementToken: 'tok-b' }))
})

test('deny sends reason', async () => {
  let body: unknown
  setup([
    http.post('*/api/v1/requests/2/deny', async ({ request }) => {
      body = await request.json()
      return HttpResponse.json(req({ id: 2 }))
    }),
  ])
  await userEvent.click(await screen.findByRole('button', { name: 'Deny Pending app' }))
  await userEvent.type(await screen.findByLabelText('Reason (optional)'), 'Not licensed')
  await userEvent.click(screen.getByRole('button', { name: 'Deny' }))
  await waitFor(() => expect(body).toEqual({ reason: 'Not licensed' }))
})

test('conflict shows message and refetches', async () => {
  let fetches = 0
  setup([
    http.get('*/api/v1/requests', () => {
      fetches++
      return HttpResponse.json(list)
    }),
    http.post('*/api/v1/requests/2/deny', () =>
      HttpResponse.json({ error: 'That request has already been handled.' }, { status: 409 }),
    ),
  ])
  await userEvent.click(await screen.findByRole('button', { name: 'Deny Pending app' }))
  await screen.findByLabelText('Reason (optional)')
  const before = fetches
  await userEvent.click(screen.getByRole('button', { name: 'Deny' }))
  expect(await screen.findByText('That request has already been handled.')).toBeInTheDocument()
  await waitFor(() => expect(fetches).toBeGreaterThan(before))
})
