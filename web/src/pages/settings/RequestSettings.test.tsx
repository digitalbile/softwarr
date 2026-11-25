import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { expect, test } from 'vitest'
import type { Settings } from '../../api/types'
import { makeSettings, makeUser, server } from '../../test/server'
import { renderApp, setupApiServer } from '../../test/utils'

setupApiServer()

function setup(settings: Settings, path = '/settings/requests') {
  server.use(
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ isAdmin: true }))),
    http.get('*/api/v1/settings', () => HttpResponse.json(settings)),
  )
  renderApp(path)
}

test('the settings nav opens the requests page', async () => {
  setup(makeSettings(), '/settings/general')
  const nav = await screen.findByRole('navigation', { name: 'Settings sections' })
  await userEvent.click(within(nav).getByText('Requests'))
  expect(await screen.findByRole('switch', { name: 'Require a reason with every request' })).toBeInTheDocument()
})

test('shows the saved request settings', async () => {
  setup(makeSettings({ commentRequired: true, stallHours: 12 }))
  expect(await screen.findByRole('switch', { name: 'Require a reason with every request' })).toBeChecked()
  expect(screen.getByLabelText('Stall threshold hours')).toHaveValue('12')
})

test('saving sends the whole settings object with the request changes merged', async () => {
  let body: Settings | undefined
  setup(makeSettings({ externalUrl: 'https://soft.example.com', defaultRetentionDays: 30 }))
  server.use(
    http.put('*/api/v1/settings', async ({ request }) => {
      body = (await request.json()) as Settings
      return HttpResponse.json(body)
    }),
  )
  await userEvent.click(await screen.findByRole('switch', { name: 'Require a reason with every request' }))
  const stall = screen.getByLabelText('Stall threshold hours')
  await userEvent.clear(stall)
  await userEvent.type(stall, '6')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(body).toBeDefined())
  expect(body!.commentRequired).toBe(true)
  expect(body!.stallHours).toBe(6)
  expect(body!.externalUrl).toBe('https://soft.example.com')
  expect(body!.defaultRetentionDays).toBe(30)
  expect(await screen.findByText('Saved.')).toBeInTheDocument()
})
