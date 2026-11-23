declare global {
  interface Window {
    __SOFTWARR__?: { urlBase: string }
  }
}

export const urlBase: string = window.__SOFTWARR__?.urlBase ?? ''

export class ApiError extends Error {
  status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export async function api<T>(path: string, init: RequestInit & { json?: unknown } = {}): Promise<T> {
  const { json, headers: initHeaders, ...rest } = init
  const method = (rest.method ?? 'GET').toUpperCase()
  const headers = new Headers(initHeaders)
  if (method !== 'GET') headers.set('X-Softwarr-CSRF', '1')
  if (json !== undefined) {
    headers.set('Content-Type', 'application/json')
    rest.body = JSON.stringify(json)
  }

  const res = await fetch(`${urlBase}/api/v1${path}`, {
    ...rest,
    method,
    headers,
    credentials: 'same-origin',
  })

  if (!res.ok) {
    let message = res.statusText || 'Something went wrong.'
    try {
      const body = await res.json()
      if (body && typeof body.error === 'string') message = body.error
    } catch {
      // keep the status text
    }
    if (res.status === 401) window.dispatchEvent(new Event('softwarr:unauthorized'))
    throw new ApiError(message, res.status)
  }
  if (res.status === 204) return undefined as T
  const text = await res.text()
  if (text === '') return undefined as T
  try {
    return JSON.parse(text) as T
  } catch {
    throw new ApiError('The server sent a response the app could not read.', res.status)
  }
}

export function isConflict(err: unknown): err is ApiError {
  return err instanceof ApiError && err.status === 409
}
