import { MantineProvider } from '@mantine/core'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterAll, afterEach, beforeAll, vi } from 'vitest'
import { AppRoutes } from '../App'
import { AuthProvider } from '../auth/AuthProvider'
import { theme } from '../theme'
import { server } from './server'

export class FakeEventSource {
  static instances: FakeEventSource[] = []
  listeners = new Map<string, ((e: MessageEvent) => void)[]>()
  closed = false
  readyState = 0
  constructor(public url: string) {
    FakeEventSource.instances.push(this)
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn])
  }
  close() {
    this.closed = true
    this.readyState = 2
  }
  // The connection opened.
  open() {
    this.readyState = 1
    this.emit('open')
  }
  // The browser gave up on the stream, as after a refused reconnect.
  fail() {
    this.readyState = 2
    this.emit('error')
  }
  emit(type: string, data: unknown = {}) {
    for (const fn of this.listeners.get(type) ?? []) fn(new MessageEvent(type, { data: JSON.stringify(data) }))
  }
}

// Call once at the top of a test file.
export function useApiServer() {
  beforeAll(() => {
    server.listen()
    const real = globalThis.fetch
    // jsdom has no base for Node's fetch, so resolve relative URLs here.
    vi.stubGlobal('fetch', (input: RequestInfo | URL, init?: RequestInit) =>
      real(typeof input === 'string' ? new URL(input, 'http://localhost').href : input, init),
    )
    vi.stubGlobal('EventSource', FakeEventSource)
  })
  afterEach(() => {
    server.resetHandlers()
    FakeEventSource.instances = []
  })
  afterAll(() => {
    server.close()
    vi.unstubAllGlobals()
  })
}

export function renderApp(route: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const result = render(
    <MantineProvider theme={theme} defaultColorScheme="dark">
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[route]}>
          <AuthProvider>
            <AppRoutes />
          </AuthProvider>
        </MemoryRouter>
      </QueryClientProvider>
    </MantineProvider>,
  )
  return { client, ...result }
}
