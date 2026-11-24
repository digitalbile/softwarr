import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import type { Settings, User } from '../api/types'

export function makeUser(over: Partial<User> = {}): User {
  return {
    id: 2,
    username: 'sam',
    email: '',
    isOwner: false,
    isAdmin: false,
    autoApprove: false,
    mustChangePassword: false,
    hasPassword: true,
    hasOidc: false,
    status: 'active',
    ...over,
  }
}

export function makeSettings(over: Partial<Settings> = {}): Settings {
  return {
    externalUrl: '',
    apiKey: 'key123',
    commentRequired: false,
    defaultRetentionDays: 0,
    stallHours: 24,
    libraryRoot: '/library',
    prowlarr: { url: '', apiKey: '', categories: [4050] },
    oidc: {
      enabled: false,
      issuer: '',
      clientId: '',
      clientSecret: '',
      buttonLabel: '',
      scopes: ['openid', 'profile', 'email'],
      autoProvision: false,
      linkVerifiedEmail: false,
      adminClaim: '',
      adminValue: '',
    },
    ...over,
  }
}

export const handlers = [
  http.get('*/api/v1/setup', () => HttpResponse.json({ needed: false })),
  http.get('*/api/v1/auth/me', () => HttpResponse.json({ error: 'Sign in first.' }, { status: 401 })),
  http.get('*/api/v1/auth/oidc/config', () => HttpResponse.json({ enabled: false, buttonLabel: '' })),
  http.get('*/api/v1/notifications', () => HttpResponse.json([])),
  http.get('*/api/v1/requests', () => HttpResponse.json([])),
]

// The routes the server lets a user with a temporary password reach. Keep in
// step with mustChangeExempt in internal/httpapi/middleware.go.
export const mustChangeExempt = [
  'GET /api/v1/setup',
  'GET /api/v1/auth/me',
  'GET /api/v1/auth/oidc/config',
  'GET /api/v1/ping',
  'POST /api/v1/auth/password',
  'POST /api/v1/auth/logout',
]

// mustChangeGate answers every other API route the way the server does for a
// user who still has to change their password, for as long as active() says
// so. Pass it to its own server.use call after the handlers for the exempt
// routes, so it is checked first and falls through for those.
export function mustChangeGate(active: () => boolean = () => true) {
  return http.all('*/api/v1/*', ({ request }) => {
    const key = `${request.method} ${new URL(request.url).pathname}`
    if (!active() || mustChangeExempt.includes(key)) return undefined
    return HttpResponse.json({ error: 'password_change_required' }, { status: 403 })
  })
}

export const server = setupServer(...handlers)
