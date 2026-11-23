import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { expect, test } from 'vitest'
import type { Settings } from '../../api/types'
import { makeSettings, makeUser, server } from '../../test/server'
import { renderApp, useApiServer } from '../../test/utils'

useApiServer()

function setup(settings: Settings) {
  server.use(
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ isAdmin: true }))),
    http.get('*/api/v1/settings', () => HttpResponse.json(settings)),
  )
  renderApp('/settings/auth')
}

test('OIDC cannot be enabled without external URL', async () => {
  setup(makeSettings())
  const toggle = await screen.findByRole('switch', { name: 'Enable single sign-on' })
  expect(toggle).toBeDisabled()
  expect(screen.getByText('Set the external URL in General settings before enabling single sign-on.')).toBeInTheDocument()
})

test('redirect URI shown from external URL', async () => {
  setup(makeSettings({ externalUrl: 'https://soft.example.com/' }))
  expect(await screen.findByText('https://soft.example.com/api/v1/auth/oidc/callback')).toBeInTheDocument()
  expect(screen.getByRole('switch', { name: 'Enable single sign-on' })).toBeEnabled()
})

test('saving sends the whole settings object with the OIDC changes merged', async () => {
  let body: Settings | undefined
  const current = makeSettings({ externalUrl: 'https://soft.example.com', libraryRoot: '/data' })
  setup(current)
  server.use(http.put('*/api/v1/settings', async ({ request }) => { body = (await request.json()) as Settings; return HttpResponse.json(body) }))
  await userEvent.type(await screen.findByLabelText('Issuer URL'), 'https://idp.example.com')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(body).toBeDefined())
  expect(body!.oidc.issuer).toBe('https://idp.example.com')
  expect(body!.libraryRoot).toBe('/data')
  expect(await screen.findByText('Saved.')).toBeInTheDocument()
})
