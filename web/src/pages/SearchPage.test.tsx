import { focusManager } from '@tanstack/react-query'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { expect, test, vi } from 'vitest'
import type { SearchResult } from '../api/types'
import { makeUser, server } from '../test/server'
import { FakeEventSource, renderApp, setupApiServer } from '../test/utils'

setupApiServer()

function result(over: Partial<SearchResult>): SearchResult {
  return {
    guid: 'g',
    title: 'Thing',
    indexer: 'Idx',
    indexerId: 1,
    protocol: 'torrent',
    size: 1024 ** 3,
    publishDate: new Date(Date.now() - 3 * 86400000).toISOString(),
    seeders: 10,
    leechers: 2,
    grabs: null,
    infoHash: 'abc',
    categories: [4000],
    key: 'k',
    status: 'requestable',
    libraryItemId: null,
    token: `tok-${over.key ?? 'k'}`,
    ...over,
  }
}

const results = [
  result({ key: 'a', guid: 'a', title: 'Alpha app', status: 'available', libraryItemId: 7 }),
  result({ key: 'b', guid: 'b', title: 'Beta app', status: 'requested' }),
  result({ key: 'c', guid: 'c', title: 'Gamma app', status: 'no_client', protocol: 'usenet' }),
  result({ key: 'd', guid: 'd', title: 'Delta app', status: 'requestable' }),
]

function setup(user = {}, searchHandler = () => HttpResponse.json(results)) {
  server.use(
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser(user))),
    http.get('*/api/v1/search', searchHandler),
  )
  renderApp('/search')
}

async function runSearch() {
  const u = userEvent.setup()
  await u.type(await screen.findByLabelText('Search for software'), 'app{Enter}')
  return u
}

test('search renders results with correct badges', async () => {
  setup()
  await runSearch()
  const row = (name: string) => screen.getByText(name).closest('tr')!
  await screen.findByText('Alpha app')
  expect(within(row('Alpha app')).getByRole('link', { name: 'Available' })).toHaveAttribute('href', '/library/7')
  expect(within(row('Beta app')).getByText('Requested')).toBeInTheDocument()
  expect(within(row('Gamma app')).getByText('No compatible download client')).toBeInTheDocument()
  expect(within(row('Delta app')).getByRole('button', { name: /Request/ })).toBeInTheDocument()
  expect(within(row('Delta app')).getByText('1.0 GiB')).toBeInTheDocument()
  expect(within(row('Delta app')).getByText('3d')).toBeInTheDocument()
  expect(screen.getByRole('columnheader', { name: 'Category' })).toBeInTheDocument()
  expect(within(row('Delta app')).getByText('PC')).toBeInTheDocument()
})

test('the same release from two indexers renders as two rows without key clashes', async () => {
  const errors = vi.spyOn(console, 'error').mockImplementation(() => {})
  setup({}, () =>
    HttpResponse.json([
      result({ key: 'ih:same', title: 'Twin app', indexer: 'One', indexerId: 1 }),
      result({ key: 'ih:same', title: 'Twin app', indexer: 'Two', indexerId: 2, categories: [4020, 100001] }),
    ]),
  )
  await runSearch()
  await waitFor(() => expect(screen.getAllByText('Twin app')).toHaveLength(2))
  expect(screen.getByText('PC/ISO, 100001')).toBeInTheDocument()
  const clashes = errors.mock.calls.filter((c) => String(c[0]).includes('same key'))
  errors.mockRestore()
  expect(clashes).toEqual([])
})

test('no_client results have no request button', async () => {
  setup()
  await runSearch()
  await screen.findByText('Gamma app')
  expect(screen.getAllByRole('button', { name: /^Request / })).toHaveLength(1)
  expect(within(screen.getByText('Gamma app').closest('tr')!).queryByRole('button')).not.toBeInTheDocument()
})

test('request dialog enforces required comment', async () => {
  let body: unknown
  server.use(
    http.post('*/api/v1/requests', async ({ request }) => {
      body = await request.json()
      return HttpResponse.json({ id: 1 }, { status: 201 })
    }),
  )
  setup({ commentRequired: true })
  const u = await runSearch()
  await u.click(await screen.findByRole('button', { name: 'Request Delta app' }))
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByText('1.0 GiB from Idx')).toBeInTheDocument()
  await u.click(within(dialog).getByRole('button', { name: 'Send request' }))
  expect(await within(dialog).findByText('Tell us why you need this software.')).toBeInTheDocument()
  expect(body).toBeUndefined()

  await u.type(within(dialog).getByLabelText(/Why do you need this software\?/), 'For work')
  await u.click(within(dialog).getByRole('button', { name: 'Send request' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  expect(body).toEqual({ token: 'tok-d', comment: 'For work' })
  const row = screen.getByText('Delta app').closest('tr')!
  expect(within(row).getByText('Requested')).toBeInTheDocument()
})

test('duplicate request shows server message', async () => {
  server.use(
    http.post('*/api/v1/requests', () =>
      HttpResponse.json({ error: 'This release has already been requested.' }, { status: 409 }),
    ),
  )
  setup()
  const u = await runSearch()
  await u.click(await screen.findByRole('button', { name: 'Request Delta app' }))
  const dialog = await screen.findByRole('dialog')
  await u.click(within(dialog).getByRole('button', { name: 'Send request' }))
  expect(await within(dialog).findByText('This release has already been requested.')).toBeInTheDocument()
})

test('server errors show above the table', async () => {
  setup({}, () => HttpResponse.json({ error: "Prowlarr isn't set up yet." }, { status: 409 }))
  await runSearch()
  expect(await screen.findByText("Prowlarr isn't set up yet.")).toBeInTheDocument()
})

test('admins can filter by indexer', async () => {
  let ids: string | null = 'unset'
  server.use(
    http.get('*/api/v1/indexers', () =>
      HttpResponse.json([{ id: 3, name: 'Nyaa', protocol: 'torrent', enabled: true }]),
    ),
  )
  setup({ isAdmin: true }, () => HttpResponse.json(results))
  server.use(
    http.get('*/api/v1/search', ({ request }) => {
      ids = new URL(request.url).searchParams.get('indexerIds')
      return HttpResponse.json(results)
    }),
  )
  const u = userEvent.setup()
  await screen.findByText('Search for software')
  await u.click(await screen.findByPlaceholderText('All indexers'))
  await u.click(await screen.findByText('Nyaa', {}, { timeout: 2000 }))
  await u.type(screen.getByLabelText('Search for software'), 'app{Enter}')
  await screen.findByText('Alpha app')
  expect(ids).toBe('3')
})

test('events and focus do not re-run the search, but the Search button does', async () => {
  let searches = 0
  server.use(http.post('*/api/v1/requests', () => HttpResponse.json({ id: 1 }, { status: 201 })))
  setup({}, () => {
    searches++
    return HttpResponse.json(results)
  })
  const u = await runSearch()
  await screen.findByText('Delta app')
  expect(searches).toBe(1)

  await u.click(screen.getByRole('button', { name: 'Request Delta app' }))
  await u.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Send request' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

  await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
  const events = FakeEventSource.instances[0]
  events.emit('library.updated', { id: 3 })
  events.emit('request.updated', { id: 1, status: 'pending', progress: 0 })
  focusManager.setFocused(false)
  focusManager.setFocused(true)
  focusManager.setFocused(undefined)
  await new Promise((r) => setTimeout(r, 50))
  expect(searches).toBe(1)
  // The optimistic badge stays.
  expect(within(screen.getByText('Delta app').closest('tr')!).getByText('Requested')).toBeInTheDocument()

  await u.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() => expect(searches).toBe(2))
})
