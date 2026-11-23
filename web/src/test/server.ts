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

export const server = setupServer(...handlers)
