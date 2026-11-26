// Mirrors of the Go JSON types. Timestamps are RFC 3339 strings except
// Notification.createdAt, which is unix seconds. apitypes_test.go in
// internal/httpapi checks the field names against Go.

export interface User {
  id: number
  username: string
  email: string
  isOwner: boolean
  isAdmin: boolean
  autoApprove: boolean
  mustChangePassword: boolean
  hasPassword: boolean
  hasOidc: boolean
  status: string
  // Only on GET /auth/me and login.
  commentRequired?: boolean
}

export interface Indexer {
  id: number
  name: string
  protocol: string
  enabled: boolean
}

export interface ProwlarrSettings {
  url: string
  apiKey: string
  categories: number[]
}

export interface OIDCSettings {
  enabled: boolean
  disableLocalAuth: boolean
  issuer: string
  clientId: string
  clientSecret: string
  buttonLabel: string
  scopes: string[]
  autoProvision: boolean
  linkVerifiedEmail: boolean
  adminClaim: string
  adminValue: string
}

export interface Settings {
  externalUrl: string
  apiKey: string
  commentRequired: boolean
  defaultRetentionDays: number
  stallHours: number
  libraryRoot: string
  prowlarr: ProwlarrSettings
  oidc: OIDCSettings
}

// Download links never leave the server; a search result carries a sealed token instead.
export interface Release {
  guid: string
  title: string
  indexer: string
  indexerId: number
  protocol: string
  size: number
  publishDate: string
  seeders: number | null
  leechers: number | null
  grabs: number | null
  infoHash: string
  categories: number[]
}

export type SearchStatus = 'available' | 'requested' | 'requestable' | 'no_client'

export interface SearchResult extends Release {
  key: string
  status: SearchStatus
  libraryItemId: number | null
  // Names this result in POST /requests and approve-with-replacement.
  token: string
}

export type RequestStatus =
  | 'pending'
  | 'approved'
  | 'downloading'
  | 'importing'
  | 'scanning'
  | 'available'
  | 'denied'
  | 'cancelled'
  | 'failed'
  | 'abandoned'

export interface Request {
  id: number
  requesterId: number
  requester: string
  status: RequestStatus
  release: Release
  originalRelease: Release | null
  comment?: string
  displayTitle: string
  denyReason: string
  error: string
  // Only sent to admins: the full error behind error.
  errorDetail?: string
  progress: number
  libraryItemId: number | null
  createdAt: string
  updatedAt: string
}

export interface Activity {
  requestId: number
  title: string
  requester: string
  status: RequestStatus
  progress: number
  stalled: boolean
  error: string
  errorDetail?: string
  updatedAt: string
}

export interface LibraryFile {
  path: string
  size: number
}

export interface LibraryItem {
  id: number
  title: string
  releaseName: string
  totalSize: number
  protocol: string
  indexer: string
  requester: string
  comment: string
  approver: string
  importMode: string
  retentionDays: number | null
  importedAt: string
  lastDownloadedAt: string | null
  downloadCount: number
  files: LibraryFile[]
  expiresAt: string | null
}

export interface Notification {
  id: number
  type: string
  message: string
  requestId: number | null
  itemId: number | null
  read: boolean
  createdAt: number
}

export interface Webhook {
  id: number
  name: string
  url: string
  hasSecret: boolean
  events: string[]
  enabled: boolean
}

export interface DownloadClient {
  id: number
  name: string
  type: string
  protocol: string
  enabled: boolean
  priority: number
  settings: Record<string, unknown>
}

export interface PathMapping {
  id: number
  host: string
  remotePath: string
  localPath: string
}

export type HealthLevel = 'ok' | 'warning' | 'error'

export interface HealthCheck {
  id: string
  level: HealthLevel
  message: string
}

export interface LogEntry {
  time: string
  level: string
  message: string
  attrs?: Record<string, unknown>
}
