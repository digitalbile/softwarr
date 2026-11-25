import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { expect, test } from 'vitest'
import { makeUser, server } from '../../test/server'
import { renderApp, setupApiServer } from '../../test/utils'

setupApiServer()

function setup() {
  server.use(
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ isAdmin: true }))),
    http.get('*/api/v1/downloadclients', () => HttpResponse.json([])),
    http.get('*/api/v1/downloadclients/types', () =>
      HttpResponse.json([{ type: 'qbittorrent', protocol: 'torrent', name: 'qBittorrent' }]),
    ),
  )
  renderApp('/settings/downloadclients')
}

test('download client test shows error inline', async () => {
  let sent: Record<string, unknown> = {}
  setup()
  server.use(
    http.post('*/api/v1/downloadclients/test', async ({ request }) => {
      sent = (await request.json()) as Record<string, unknown>
      return HttpResponse.json({ ok: false, error: 'Connection refused' })
    }),
  )
  await userEvent.click(await screen.findByRole('button', { name: 'Add client' }))
  const dialog = await screen.findByRole('dialog')
  await userEvent.type(within(dialog).getByLabelText('Name'), 'qbit')
  await userEvent.type(within(dialog).getByLabelText('Host'), 'qbit.local')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Test' }))
  expect(await within(dialog).findByText('Connection refused')).toBeInTheDocument()
  expect(sent.type).toBe('qbittorrent')
  expect((sent.settings as Record<string, unknown>).host).toBe('qbit.local')
})

test('adding a client posts the form', async () => {
  let sent: Record<string, unknown> = {}
  setup()
  server.use(
    http.post('*/api/v1/downloadclients', async ({ request }) => {
      sent = (await request.json()) as Record<string, unknown>
      return HttpResponse.json({}, { status: 201 })
    }),
  )
  await userEvent.click(await screen.findByRole('button', { name: 'Add client' }))
  const dialog = await screen.findByRole('dialog')
  await userEvent.type(within(dialog).getByLabelText('Name'), 'qbit')
  await userEvent.type(within(dialog).getByLabelText('Host'), 'qbit.local')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(sent.name).toBe('qbit'))
  expect(sent).toMatchObject({ type: 'qbittorrent', enabled: true, priority: 1 })
})
