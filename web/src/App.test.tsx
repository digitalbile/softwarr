import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { expect, test, vi } from 'vitest'
import { makeUser, mustChangeGate, server } from './test/server'
import { FakeEventSource, renderApp, setupApiServer } from './test/utils'

setupApiServer()

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
    http.get('*/api/v1/auth/oidc/config', () =>
      HttpResponse.json({ enabled: true, buttonLabel: 'Sign in with Authentik' }),
    ),
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

test('must-change user reaches the change password page and can submit', async () => {
  let changed = false
  const requests: string[] = []
  server.events.on('request:start', ({ request }) => {
    requests.push(`${request.method} ${new URL(request.url).pathname}`)
  })
  server.use(
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ mustChangePassword: !changed }))),
    http.post('*/api/v1/auth/password', () => {
      changed = true
      return new HttpResponse(null, { status: 204 })
    }),
  )
  server.use(mustChangeGate(() => !changed))
  renderApp('/search')
  expect(await screen.findByRole('heading', { name: 'Choose a new password' })).toBeInTheDocument()
  const blocked = requests.filter((r) => r.startsWith('GET') && !r.match(/setup|auth\/me|oidc|ping/))
  expect(blocked).toEqual([])
  expect(FakeEventSource.instances).toHaveLength(0)

  await userEvent.type(screen.getByLabelText(/Current password/), 'temporary')
  await userEvent.type(screen.getByLabelText(/New password/), 'my own password')
  await userEvent.click(screen.getByRole('button', { name: 'Change password' }))
  expect(await screen.findByRole('heading', { name: 'Search' })).toBeInTheDocument()
  server.events.removeAllListeners()
})

test('a failing setup check does not sign a must-change user out', async () => {
  server.use(
    http.get('*/api/v1/setup', () => HttpResponse.json({ error: 'password_change_required' }, { status: 403 })),
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ mustChangePassword: true }))),
  )
  renderApp('/search')
  expect(await screen.findByRole('heading', { name: 'Choose a new password' })).toBeInTheDocument()
  expect(screen.queryByText('Sign in to softwarr')).not.toBeInTheDocument()
})

test('a password reset mid-session sends the user to the change password page', async () => {
  let reset = false
  server.use(http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ mustChangePassword: reset }))))
  server.use(mustChangeGate(() => reset))
  renderApp('/search')
  await screen.findByRole('heading', { name: 'Search' })
  await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
  reset = true
  // The refetch this triggers hits the gate.
  FakeEventSource.instances[0].emit('notification', { id: 1 })
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
      HttpResponse.json([
        { id: 1, status: 'pending' },
        { id: 2, status: 'pending' },
        { id: 3, status: 'available' },
      ]),
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
  server.use(http.get('*/api/v1/auth/oidc/config', () => HttpResponse.json({ enabled: true, buttonLabel: 'SSO' })))
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
      const all = [
        {
          id: 1,
          type: 'request.approved',
          message: 'Your request was approved.',
          requestId: 1,
          itemId: null,
          read: false,
          createdAt: 1700000000,
        },
      ]
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

test('a stream the browser gave up on is reopened with a backoff', async () => {
  signedInAs()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'], shouldAdvanceTime: true })
  try {
    renderApp('/search')
    await screen.findByRole('heading', { name: 'Search' })
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
    const latest = () => FakeEventSource.instances[FakeEventSource.instances.length - 1]

    // A dropped connection the browser is still retrying is left alone.
    latest().emit('error')
    await vi.advanceTimersByTimeAsync(5000)
    expect(FakeEventSource.instances).toHaveLength(1)

    latest().fail()
    expect(latest().closed).toBe(true)
    await vi.advanceTimersByTimeAsync(1900)
    expect(FakeEventSource.instances).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(200)
    expect(FakeEventSource.instances).toHaveLength(2)

    // It fails again before opening: the wait doubles.
    latest().fail()
    await vi.advanceTimersByTimeAsync(2100)
    expect(FakeEventSource.instances).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(2000)
    expect(FakeEventSource.instances).toHaveLength(3)

    // Once a stream opens, the next reopen waits the short delay again.
    latest().open()
    latest().fail()
    await vi.advanceTimersByTimeAsync(2100)
    expect(FakeEventSource.instances).toHaveLength(4)
  } finally {
    vi.useRealTimers()
  }
})

test('a closed stream for a signed-out user goes to login without reopening', async () => {
  signedInAs()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'], shouldAdvanceTime: true })
  try {
    renderApp('/search')
    await screen.findByRole('heading', { name: 'Search' })
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
    server.use(
      http.get('*/api/v1/auth/me', () => HttpResponse.json({ error: 'Sign in to continue.' }, { status: 401 })),
    )

    FakeEventSource.instances[0].fail()
    expect(await screen.findByText('Sign in to softwarr')).toBeInTheDocument()
    await vi.advanceTimersByTimeAsync(60000)
    expect(FakeEventSource.instances).toHaveLength(1)
  } finally {
    vi.useRealTimers()
  }
})
