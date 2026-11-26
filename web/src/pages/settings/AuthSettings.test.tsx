import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { expect, test } from 'vitest'
import type { Settings } from '../../api/types'
import { makeSettings, makeUser, server } from '../../test/server'
import { renderApp, setupApiServer } from '../../test/utils'

setupApiServer()

function setup(settings: Settings, user = makeUser({ isAdmin: true })) {
  server.use(
    http.get('*/api/v1/auth/me', () => HttpResponse.json(user)),
    http.get('*/api/v1/settings', () => HttpResponse.json(settings)),
  )
  return renderApp('/settings/auth')
}

test('OIDC cannot be enabled without external URL', async () => {
  setup(makeSettings())
  const toggle = await screen.findByRole('switch', { name: 'Enable single sign-on' })
  expect(toggle).toBeDisabled()
  expect(
    screen.getByText('Set the external URL in General settings before enabling single sign-on.'),
  ).toBeInTheDocument()
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
  server.use(
    http.put('*/api/v1/settings', async ({ request }) => {
      body = (await request.json()) as Settings
      return HttpResponse.json(body)
    }),
  )
  await userEvent.type(await screen.findByLabelText('Issuer URL'), 'https://idp.example.com')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(body).toBeDefined())
  expect(body!.oidc.issuer).toBe('https://idp.example.com')
  expect(body!.libraryRoot).toBe('/data')
  expect(await screen.findByText('Saved.')).toBeInTheDocument()
})

test('disable local auth needs SSO', async () => {
  setup(makeSettings({ externalUrl: 'https://soft.example.com' }))
  expect(await screen.findByRole('switch', { name: /^Disable local auth/ })).toBeDisabled()
})

test('turning SSO off clears disable local auth', async () => {
  let body: Settings | undefined
  const current = makeSettings({ externalUrl: 'https://soft.example.com' })
  current.oidc = { ...current.oidc, enabled: true, issuer: 'https://idp.example.com', disableLocalAuth: true }
  setup(current)
  server.use(
    http.put('*/api/v1/settings', async ({ request }) => {
      body = (await request.json()) as Settings
      return HttpResponse.json(body)
    }),
  )
  expect(await screen.findByRole('switch', { name: /^Disable local auth/ })).toBeChecked()
  await userEvent.click(screen.getByRole('switch', { name: 'Enable single sign-on' }))
  expect(screen.getByRole('switch', { name: /^Disable local auth/ })).not.toBeChecked()
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(body).toBeDefined())
  expect(body!.oidc.enabled).toBe(false)
  expect(body!.oidc.disableLocalAuth).toBe(false)
})

test('test button checks the form values', async () => {
  let body: Record<string, unknown> | undefined
  const current = makeSettings({ externalUrl: 'https://soft.example.com' })
  current.oidc = { ...current.oidc, clientSecret: 'shh' }
  setup(current)
  server.use(
    http.post('*/api/v1/settings/oidc/test', async ({ request }) => {
      body = (await request.json()) as Record<string, unknown>
      return HttpResponse.json({ ok: false, error: 'Enter the client ID.' })
    }),
  )
  await userEvent.type(await screen.findByLabelText('Issuer URL'), 'https://idp.example.com')
  await userEvent.click(screen.getByRole('button', { name: 'Test' }))
  expect(await screen.findByRole('status')).toHaveTextContent('Enter the client ID.')
  expect(body).toEqual({ externalUrl: 'https://soft.example.com', issuer: 'https://idp.example.com', clientId: '' })
})

const lockoutWarning = /Your account isn't connected to SSO/

function ssoOn() {
  const s = makeSettings({ externalUrl: 'https://soft.example.com' })
  s.oidc = { ...s.oidc, enabled: true, issuer: 'https://idp.example.com' }
  return s
}

test('warns an admin without SSO that disabling local auth signs them out', async () => {
  setup(ssoOn())
  await userEvent.click(await screen.findByRole('switch', { name: /^Disable local auth/ }))
  expect(screen.getByText(lockoutWarning)).toBeInTheDocument()
})

test('no lockout warning for the owner or an SSO-linked admin', async () => {
  const first = setup(ssoOn(), makeUser({ isAdmin: true, isOwner: true }))
  await userEvent.click(await screen.findByRole('switch', { name: /^Disable local auth/ }))
  expect(screen.queryByText(lockoutWarning)).not.toBeInTheDocument()
  first.unmount()

  setup(ssoOn(), makeUser({ isAdmin: true, hasOidc: true }))
  await userEvent.click(await screen.findByRole('switch', { name: /^Disable local auth/ }))
  expect(screen.queryByText(lockoutWarning)).not.toBeInTheDocument()
})
