import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { expect, test, vi } from 'vitest'
import { FakeEventSource, renderApp, useApiServer } from './test/utils'
import { makeUser, server } from './test/server'

useApiServer()

function signedInAs(over = {}) {
  server.use(http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser(over))))
}

test('redirects to setup when setup is needed', async () => {
  server.use(http.get('*/api/v1/setup', () => HttpResponse.json({ needed: true })))
  renderApp('/search')
  expect(await screen.findByText('Create the owner account')).toBeInTheDocument()
})

test('redirects to login when signed out', async () => {
  renderApp('/library')
  expect(await screen.findByText('Sign in to softwarr')).toBeInTheDocument()
})

test('login shows SSO button only when OIDC enabled', async () => {
  const first = renderApp('/login')
  await screen.findByText('Sign in to softwarr')
  expect(screen.queryByRole('link', { name: 'Sign in with Authentik' })).not.toBeInTheDocument()
  first.unmount()

  server.use(
    http.get('*/api/v1/auth/oidc/config', () => HttpResponse.json({ enabled: true, buttonLabel: 'Sign in with Authentik' })),
  )
  renderApp('/login')
  const link = await screen.findByRole('link', { name: 'Sign in with Authentik' })
  expect(link).toHaveAttribute('href', '/api/v1/auth/oidc/login')
  expect(screen.getByLabelText(/Username/)).toBeInTheDocument()
})

test('login shows error from query string', async () => {
  renderApp('/login?error=That%20account%20is%20disabled.')
  expect(await screen.findByText('That account is disabled.')).toBeInTheDocument()
})

test('signed-in user landing on login with an error goes to the profile', async () => {
  signedInAs()
  renderApp('/login?error=Link%20failed.')
  expect(await screen.findByText('Link failed.')).toBeInTheDocument()
  expect(await screen.findByRole('heading', { name: 'Profile' })).toBeInTheDocument()
})

test('must-change-password user is sent to change password page', async () => {
  signedInAs({ mustChangePassword: true })
  renderApp('/search')
  expect(await screen.findByRole('heading', { name: 'Choose a new password' })).toBeInTheDocument()
})

test('non-admin sidebar hides admin links', async () => {
  signedInAs()
  renderApp('/search')
  expect(await screen.findByRole('link', { name: /My requests/ })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /Library/ })).toBeInTheDocument()
  expect(screen.queryByRole('link', { name: /Activity/ })).not.toBeInTheDocument()
  expect(screen.queryByRole('link', { name: /Settings/ })).not.toBeInTheDocument()
})

test('admin sidebar shows admin links and the pending count', async () => {
  signedInAs({ isAdmin: true })
  server.use(
    http.get('*/api/v1/requests', () =>
      HttpResponse.json([{ id: 1, status: 'pending' }, { id: 2, status: 'pending' }, { id: 3, status: 'available' }]),
    ),
  )
  renderApp('/search')
  expect(await screen.findByRole('link', { name: /Activity/ })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /System/ })).toBeInTheDocument()
  expect(await screen.findByText('2')).toBeInTheDocument()
})

test('non-admin cannot open admin routes', async () => {
  signedInAs()
  renderApp('/system')
  expect(await screen.findByRole('heading', { name: 'Search' })).toBeInTheDocument()
})

test('profile hides Connect SSO for owner and shows it for linkable users', async () => {
  server.use(
    http.get('*/api/v1/auth/oidc/config', () => HttpResponse.json({ enabled: true, buttonLabel: 'SSO' })),
  )
  signedInAs({ isOwner: true, isAdmin: true })
  const first = renderApp('/profile')
  await screen.findByRole('heading', { name: 'Profile' })
  await waitFor(() => expect(screen.getByLabelText(/Current password/)).toBeInTheDocument())
  expect(screen.queryByRole('button', { name: 'Connect SSO account' })).not.toBeInTheDocument()
  first.unmount()

  signedInAs()
  renderApp('/profile')
  expect(await screen.findByRole('button', { name: 'Connect SSO account' })).toBeInTheDocument()
})

test('profile shows success after linking', async () => {
  signedInAs()
  renderApp('/profile?linked=1')
  expect(await screen.findByText('Your SSO account is connected.')).toBeInTheDocument()
})

test('notification event invalidates notifications query', async () => {
  signedInAs()
  let calls = 0
  server.use(
    http.get('*/api/v1/notifications', () => {
      calls++
      return HttpResponse.json([])
    }),
  )
  renderApp('/search')
  await screen.findByRole('heading', { name: 'Search' })
  await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
  expect(FakeEventSource.instances[0].url).toBe('/api/v1/events')
  await waitFor(() => expect(calls).toBeGreaterThan(0))
  const before = calls
  FakeEventSource.instances[0].emit('notification', { id: 1 })
  await waitFor(() => expect(calls).toBeGreaterThan(before))
})

test('unauthorized event sends the user to login', async () => {
  signedInAs()
  renderApp('/search')
  await screen.findByRole('heading', { name: 'Search' })
  window.dispatchEvent(new Event('softwarr:unauthorized'))
  expect(await screen.findByText('Sign in to softwarr')).toBeInTheDocument()
  await waitFor(() => expect(FakeEventSource.instances[0].closed).toBe(true))
  vi.restoreAllMocks()
})

test('bell shows unread count and marks all as read', async () => {
  signedInAs()
  let marked: unknown = null
  server.use(
    http.get('*/api/v1/notifications', ({ request }) => {
      const unread = new URL(request.url).searchParams.get('unread') === '1'
      const all = [{ id: 1, type: 'request.approved', message: 'Your request was approved.', requestId: 1, itemId: null, read: false, createdAt: 1700000000 }]
      return HttpResponse.json(unread ? all : all)
    }),
    http.post('*/api/v1/notifications/read', async ({ request }) => {
      marked = await request.json()
      return new HttpResponse(null, { status: 204 })
    }),
  )
  renderApp('/search')
  const bell = await screen.findByRole('button', { name: /Notifications/ })
  await waitFor(() => expect(bell).toHaveTextContent('1'))
  await userEvent.click(bell)
  expect(await screen.findByText('Your request was approved.')).toBeInTheDocument()
  await userEvent.click(await screen.findByRole('button', { name: 'Mark all as read', hidden: true }))
  await waitFor(() => expect(marked).toEqual({ all: true }))
})
