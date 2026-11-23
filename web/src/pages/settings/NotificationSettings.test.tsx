import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { expect, test } from 'vitest'
import { makeUser, server } from '../../test/server'
import { renderApp, useApiServer } from '../../test/utils'

useApiServer()

function setup(hooks: unknown[] = []) {
  server.use(
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ isAdmin: true }))),
    http.get('*/api/v1/webhooks', () => HttpResponse.json(hooks)),
  )
  renderApp('/settings/notifications')
}

test('webhook form sends selected events', async () => {
  let sent: Record<string, unknown> = {}
  setup()
  server.use(http.post('*/api/v1/webhooks', async ({ request }) => { sent = (await request.json()) as Record<string, unknown>; return HttpResponse.json({}, { status: 201 }) }))
  await userEvent.click(await screen.findByRole('button', { name: 'Add webhook' }))
  const dialog = await screen.findByRole('dialog')
  await userEvent.type(within(dialog).getByLabelText('Name'), 'Discord')
  await userEvent.type(within(dialog).getByLabelText('URL'), 'https://hooks.example.com/x')
  await userEvent.click(within(dialog).getByLabelText('Request approved'))
  await userEvent.click(within(dialog).getByLabelText('Item available'))
  await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(sent.name).toBe('Discord'))
  expect(sent).toEqual({
    name: 'Discord',
    url: 'https://hooks.example.com/x',
    secret: '',
    clearSecret: false,
    events: ['request.approved', 'item.available'],
    enabled: true,
  })
})

test('existing secret is kept unless cleared', async () => {
  let sent: Record<string, unknown> = {}
  setup([{ id: 5, name: 'Hook', url: 'https://h.example.com', hasSecret: true, events: ['item.expired'], enabled: true }])
  server.use(http.put('*/api/v1/webhooks/5', async ({ request }) => { sent = (await request.json()) as Record<string, unknown>; return HttpResponse.json({}) }))
  await userEvent.click(await screen.findByRole('button', { name: 'Edit Hook' }))
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByText('A secret is set. Leave this blank to keep it.')).toBeInTheDocument()
  await userEvent.click(within(dialog).getByLabelText('Remove the saved secret'))
  await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(sent.clearSecret).toBe(true))
  expect(sent.secret).toBe('')
})
