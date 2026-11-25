import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { expect, test } from 'vitest'
import type { LibraryItem } from '../api/types'
import { makeUser, server } from '../test/server'
import { renderApp, setupApiServer } from '../test/utils'

setupApiServer()

function item(over: Partial<LibraryItem>): LibraryItem {
  return {
    id: 1,
    title: 'Alpha app',
    releaseName: 'Alpha.App.1.0',
    totalSize: 1048576,
    protocol: 'torrent',
    indexer: 'idx',
    requester: 'sam',
    comment: '',
    approver: 'root',
    importMode: 'hardlink',
    retentionDays: null,
    importedAt: '2026-11-20T10:00:00Z',
    lastDownloadedAt: null,
    downloadCount: 0,
    files: [{ path: 'a.bin', size: 1048576 }],
    expiresAt: null,
    ...over,
  }
}

const me = () => http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser()))
const admin = () => http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ isAdmin: true })))

test('library list filters by text', async () => {
  const queries: string[] = []
  server.use(
    me(),
    http.get('*/api/v1/library', ({ request }) => {
      const q = new URL(request.url).searchParams.get('q') ?? ''
      queries.push(q)
      const all = [item({ id: 1, title: 'Alpha app' }), item({ id: 2, title: 'Beta tool', releaseName: 'Beta.Tool' })]
      return HttpResponse.json(all.filter((i) => i.title.toLowerCase().includes(q.toLowerCase())))
    }),
  )
  renderApp('/library')
  await screen.findByText('Alpha app')
  expect(screen.getByText('Beta tool')).toBeInTheDocument()
  await userEvent.type(screen.getByPlaceholderText('Filter library'), 'beta')
  await screen.findByText('Beta tool')
  await expect.poll(() => screen.queryByText('Alpha app')).toBeNull()
  expect(queries).toContain('beta')
})

test('download link points at download endpoint with url base', async () => {
  server.use(
    me(),
    http.get('*/api/v1/library', () => HttpResponse.json([item({ id: 7 })])),
  )
  renderApp('/library')
  const row = (await screen.findByText('Alpha app')).closest('tr')!
  const link = within(row).getByRole('link', { name: 'Download' })
  expect(link).toHaveAttribute('href', '/api/v1/library/7/download')
  expect(link).toHaveAttribute('download')
})

test('retention control maps Use default, Never and Custom days', async () => {
  const bodies: unknown[] = []
  server.use(
    admin(),
    http.get('*/api/v1/library/:id', () => HttpResponse.json(item({ id: 3, retentionDays: 5 }))),
    http.patch('*/api/v1/library/:id', async ({ request }) => {
      const b = await request.json()
      bodies.push(b)
      return HttpResponse.json(item({ id: 3 }))
    }),
  )
  renderApp('/library/3')
  await screen.findByText('Alpha app')
  const save = () => userEvent.click(screen.getByRole('button', { name: 'Save' }))

  await userEvent.click(screen.getByText('Use default'))
  await save()
  await expect.poll(() => bodies.length).toBe(1)
  expect(bodies[0]).toEqual({ retentionDays: null })

  await userEvent.click(screen.getByText('Never'))
  await save()
  await expect.poll(() => bodies.length).toBe(2)
  expect(bodies[1]).toEqual({ retentionDays: 0 })

  await userEvent.click(screen.getByText('Custom days'))
  const days = screen.getByLabelText('Days')
  await userEvent.clear(days)
  await userEvent.type(days, '14')
  await save()
  await expect.poll(() => bodies.length).toBe(3)
  expect(bodies[2]).toEqual({ retentionDays: 14 })
})

test('clearing the title sends null', async () => {
  const bodies: unknown[] = []
  server.use(
    admin(),
    http.get('*/api/v1/library/:id', () => HttpResponse.json(item({ id: 3 }))),
    http.patch('*/api/v1/library/:id', async ({ request }) => {
      bodies.push(await request.json())
      return HttpResponse.json(item({ id: 3 }))
    }),
  )
  renderApp('/library/3')
  await screen.findByText('Never expires')
  await userEvent.clear(screen.getByLabelText('Title'))
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await expect.poll(() => bodies.length).toBe(1)
  expect(bodies[0]).toEqual({ title: null })
})

test('delete asks for confirmation then returns to the library', async () => {
  let deleted = false
  server.use(
    admin(),
    http.get('*/api/v1/library/:id', () =>
      deleted ? HttpResponse.json({ error: 'gone' }, { status: 404 }) : HttpResponse.json(item({ id: 3 })),
    ),
    http.get('*/api/v1/library', () => HttpResponse.json([])),
    http.delete('*/api/v1/library/:id', () => {
      deleted = true
      return new HttpResponse(null, { status: 204 })
    }),
  )
  renderApp('/library/3')
  await screen.findByText('Never expires')
  await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
  expect(
    await screen.findByText("Delete this item from the library? Seeding files in the download client aren't affected."),
  ).toBeInTheDocument()
  await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }))
  await screen.findByText('Nothing in the library yet.')
  expect(deleted).toBe(true)
  expect(screen.queryByText("That library item doesn't exist.")).not.toBeInTheDocument()
})

test('non-admin sees details but no edit or delete controls', async () => {
  server.use(
    me(),
    http.get('*/api/v1/library/:id', () =>
      HttpResponse.json(item({ id: 3, comment: 'for work', expiresAt: '2027-01-02T00:00:00Z', downloadCount: 4 })),
    ),
  )
  renderApp('/library/3')
  await screen.findByText('Alpha app')
  expect(screen.getByText('for work')).toBeInTheDocument()
  expect(screen.getByText('a.bin')).toBeInTheDocument()
  expect(screen.queryByText('Never expires')).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
  expect(screen.queryByLabelText('Title')).not.toBeInTheDocument()
})

test('missing item shows a message', async () => {
  server.use(
    me(),
    http.get('*/api/v1/library/:id', () => HttpResponse.json({ error: 'x' }, { status: 404 })),
  )
  renderApp('/library/99')
  expect(await screen.findByText("That library item doesn't exist.")).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Back to library' })).toHaveAttribute('href', '/library')
})
