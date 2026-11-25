import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { expect, test } from 'vitest'
import type { Settings } from '../../api/types'
import { makeSettings, makeUser, server } from '../../test/server'
import { renderApp, setupApiServer } from '../../test/utils'

setupApiServer()

function setup(settings: Settings) {
  server.use(
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ isAdmin: true }))),
    http.get('*/api/v1/settings', () => HttpResponse.json(settings)),
  )
  renderApp('/settings/library')
}

test('shows the saved library settings', async () => {
  setup(makeSettings({ libraryRoot: '/data/library', defaultRetentionDays: 30 }))
  expect(await screen.findByLabelText('Library root path')).toHaveValue('/data/library')
  expect(screen.getByLabelText('Default retention days')).toHaveValue('30')
})

test('saving sends the whole settings object with the library changes merged', async () => {
  let body: Settings | undefined
  setup(makeSettings({ externalUrl: 'https://soft.example.com', stallHours: 12 }))
  server.use(
    http.put('*/api/v1/settings', async ({ request }) => {
      body = (await request.json()) as Settings
      return HttpResponse.json(body)
    }),
  )
  const retention = await screen.findByLabelText('Default retention days')
  await userEvent.clear(retention)
  await userEvent.type(retention, '14')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(body).toBeDefined())
  expect(body!.defaultRetentionDays).toBe(14)
  expect(body!.libraryRoot).toBe('/library')
  expect(body!.externalUrl).toBe('https://soft.example.com')
  expect(body!.stallHours).toBe(12)
  expect(await screen.findByText('Saved.')).toBeInTheDocument()
})
