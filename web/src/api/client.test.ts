import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { ApiError, api } from './client'

const fetchMock = vi.fn()

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  fetchMock.mockReset()
  vi.unstubAllGlobals()
})

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

test('adds CSRF header on POST, not GET', async () => {
  fetchMock.mockImplementation(async () => jsonResponse(200, {}))
  await api('/ping')
  await api('/requests', { method: 'POST', json: { comment: 'hi' } })

  const [getUrl, getInit] = fetchMock.mock.calls[0]
  expect(getUrl).toBe('/api/v1/ping')
  expect(new Headers(getInit.headers).has('X-Softwarr-CSRF')).toBe(false)
  expect(getInit.credentials).toBe('same-origin')

  const [, postInit] = fetchMock.mock.calls[1]
  const headers = new Headers(postInit.headers)
  expect(headers.get('X-Softwarr-CSRF')).toBe('1')
  expect(headers.get('Content-Type')).toBe('application/json')
  expect(postInit.body).toBe('{"comment":"hi"}')
})

test('throws ApiError with server message and status', async () => {
  fetchMock.mockResolvedValue(jsonResponse(409, { error: 'This release has already been requested.' }))
  const err = (await api('/requests', { method: 'POST', json: {} }).catch((e: unknown) => e)) as ApiError
  expect(err).toBeInstanceOf(ApiError)
  expect(err.status).toBe(409)
  expect(err.message).toBe('This release has already been requested.')
})

test('dispatches softwarr:unauthorized on 401', async () => {
  fetchMock.mockResolvedValue(jsonResponse(401, { error: 'Sign in to continue.' }))
  const handler = vi.fn()
  window.addEventListener('softwarr:unauthorized', handler)
  await expect(api('/auth/me')).rejects.toBeInstanceOf(ApiError)
  window.removeEventListener('softwarr:unauthorized', handler)
  expect(handler).toHaveBeenCalledTimes(1)
})

test('returns undefined for 204', async () => {
  fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
  await expect(api('/library/1', { method: 'DELETE' })).resolves.toBeUndefined()
})
