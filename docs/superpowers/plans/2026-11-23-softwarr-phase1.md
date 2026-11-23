# softwarr phase 1 (MVP) implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the phase 1 MVP of softwarr: search Prowlarr, request and approve releases, download them with qBittorrent, import them into a shared library with hardlinks, and serve them to users from a web UI with local and OIDC sign-in.

**Architecture:** One Go binary serves a JSON API under `/api/v1`, an SSE stream, and an embedded React SPA. SQLite holds all state. Background workers in the same process drive requests through grab, download, and import, and apply retention daily.

**Tech Stack:** Go 1.27, `net/http`, `modernc.org/sqlite`, `sqlc` and `goose` (both run through `go tool`), `golang.org/x/crypto/argon2`, `coreos/go-oidc/v3`, `golang.org/x/oauth2`. Frontend: React, TypeScript, Vite, Mantine, TanStack Query, React Router, Vitest, Testing Library, MSW. Node 24 with npm.

**Spec:** `docs/superpowers/specs/2026-11-23-softwarr-design.md`

## Global Constraints

- Module path: `github.com/digitalbile/softwarr`. Go 1.27. `CGO_ENABLED=0` builds must work.
- MIT license. No code copied or translated from Sonarr, Radarr, Prowlarr, or any GPL project.
- softwarr never pauses, removes, or recategorizes a torrent, and never modifies or deletes files under a download client path.
- The owner account (`is_owner = 1`) can never be deleted, disabled, demoted, or linked to OIDC. The local login form is always shown.
- OIDC identity key is (issuer, `sub`). Email is never used to match a login to an account except under the opt-in "link by verified email" setting with `email_verified=true`, and never for the owner.
- Every admin endpoint checks the role on the server.
- Environment variables (restart required): `SOFTWARR_PORT` (default `7979`), `SOFTWARR_DATA_DIR` (default `/config`), `SOFTWARR_URL_BASE` (default empty, normalized to `""` or `/base` without a trailing slash). The Docker image adds linuxserver-style `PUID` (default `1000`), `PGID` (default `1000`), and `UMASK` (default `022`), handled by the entrypoint, not by Go.
- Default Prowlarr categories: `[4000, 4010, 4020, 4030]`. Default qBittorrent category: `softwarr`. Default stall threshold: 24 hours. Default comment requirement: optional. Default global retention: `0` (never expire). OIDC auto-provisioning defaults on. Link by verified email defaults off.
- Exact UI copy fixed by the spec: request prompt "Why do you need this software?", duplicate rejection "This release has already been requested.", unrequestable badge "No compatible download client".
- All user-facing prose (UI strings, error messages, README, PR descriptions) goes through the `humanizer:humanizer` skill before commit: no em or en dashes, sentence-case headings, no decorative bold.
- JSON error body shape everywhere: `{"error": "<message>"}`.
- Commit after every task. No Claude or AI attribution in commits or PRs (no Co-Authored-By trailer, no "Generated with" line).

## Review Focus

1. Torrent file names containing `..`, absolute paths, or characters illegal on common filesystems: import must never write outside the item's library folder, and must produce a usable folder name. (Task 16)
2. Two admins (or an admin and the worker) acting on the same request at once: exactly one state change wins and the other gets a 409, never a double grab. (Task 15)
3. qBittorrent's session cookie expiring while softwarr runs: the client logs in again once and retries, instead of failing every poll. (Task 13)
4. A library item whose files were removed from disk by hand or whose mount is missing: download returns a clear 410 before any bytes are sent, never a truncated zip. (Task 18)
5. Prowlarr unreachable, slow, or returning errors: search returns a 502 with a readable message within 60 seconds instead of hanging or a blank result list. (Task 11)

---

## File structure

```
cmd/softwarr/main.go                 entrypoint: serve (default) and reset-password
internal/config/                     env parsing
internal/db/                         Open, Migrate, migrations/*.sql, queries/*.sql
internal/db/dbq/                     sqlc output (generated, committed)
internal/testutil/                   NewDB(t), clock helpers
internal/settings/                   typed app settings stored as JSON
internal/auth/                       passwords, users + owner rules, sessions, OIDC, reset
internal/events/                     SSE broker
internal/notify/                     in-app notifications, webhooks
internal/prowlarr/                   search client, Release type
internal/torrentfile/                infohash from .torrent bytes and magnets
internal/downloadclient/             Client interface, registry
internal/downloadclient/qbittorrent/ qBittorrent Web API v2 client
internal/pathmap/                    remote path mapping
internal/requests/                   state machine, service, worker
internal/importer/                   hardlink/copy/move into library
internal/library/                    items, delivery, retention
internal/health/                     health checks
internal/logbuf/                     slog ring buffer for System > Logs
internal/httpapi/                    server, middleware, one handlers file per area
web/                                 Vite React app; web/embed.go embeds web/dist
Dockerfile, Makefile, sqlc.yaml
```

### API routes (all under `{URL_BASE}/api/v1`)

| Route | Who | Task |
|---|---|---|
| `GET /ping` | anyone | 1 |
| `GET /setup`, `POST /setup` | anyone (POST only while no users exist) | 5 |
| `POST /auth/login`, `POST /auth/logout`, `GET /auth/me`, `POST /auth/password` | user | 5 |
| `GET /users`, `POST /users`, `PATCH /users/{id}`, `DELETE /users/{id}`, `POST /users/{id}/approve` | admin | 7 |
| `GET /settings`, `PUT /settings`, `POST /settings/apikey` | admin | 7 |
| `GET /auth/oidc/config`, `GET /auth/oidc/login`, `GET /auth/oidc/callback` | anyone | 8 |
| `POST /auth/oidc/link` | local non-owner user | 8 |
| `GET /events` | user | 9 |
| `GET /notifications`, `POST /notifications/read` | user | 10 |
| `GET /webhooks`, `POST /webhooks`, `PUT /webhooks/{id}`, `DELETE /webhooks/{id}`, `POST /webhooks/{id}/test` | admin | 10 |
| `GET /search`, `GET /indexers`, `POST /settings/prowlarr/test` | user / admin / admin | 11 |
| `GET /downloadclients`, `POST /downloadclients`, `PUT /downloadclients/{id}`, `DELETE /downloadclients/{id}`, `POST /downloadclients/test` | admin | 13 |
| `GET /pathmappings`, `POST /pathmappings`, `PUT /pathmappings/{id}`, `DELETE /pathmappings/{id}` | admin | 14 |
| `GET /requests`, `POST /requests`, `GET /requests/{id}`, `POST /requests/{id}/cancel` | user (own) | 15 |
| `POST /requests/{id}/approve`, `/deny`, `/abandon`, `/retry`, `GET /requests?all=1`, `GET /activity` | admin | 15, 17 |
| `GET /library`, `GET /library/{id}`, `GET /library/{id}/download` | user | 18 |
| `PATCH /library/{id}`, `DELETE /library/{id}` | admin | 18 |
| `GET /system/health`, `GET /system/logs`, `GET /system/status` | admin | 19 |

---

### Task 1: Project skeleton and config

**Files:**
- Create: `go.mod`, `cmd/softwarr/main.go`, `internal/config/config.go`, `internal/config/config_test.go`, `Makefile`, `.gitignore`

**Interfaces:**
- Produces: `config.Config{Port int; DataDir string; URLBase string}`; `config.FromEnv(getenv func(string) string) (config.Config, error)`; `main.version` string set by `-ldflags "-X main.version=..."` (default `"dev"`).

- [ ] **Step 1: Write the failing tests**

```go
func TestFromEnvDefaults(t *testing.T) {
	c, err := config.FromEnv(func(string) string { return "" })
	require.NoError(t, err)
	assert.Equal(t, config.Config{Port: 7979, DataDir: "/config", URLBase: ""}, c)
}
func TestFromEnvNormalizesURLBase(t *testing.T) {
	for in, want := range map[string]string{"softwarr": "/softwarr", "/softwarr/": "/softwarr", "/": ""} {
		c, _ := config.FromEnv(env(map[string]string{"SOFTWARR_URL_BASE": in}))
		assert.Equal(t, want, c.URLBase, in)
	}
}
func TestFromEnvRejectsBadPort(t *testing.T) {
	_, err := config.FromEnv(env(map[string]string{"SOFTWARR_PORT": "abc"}))
	assert.Error(t, err)
}
```

Use `github.com/stretchr/testify` for assertions throughout the Go code.

- [ ] **Step 2: Run** `go test ./internal/config/...` and expect a compile failure.
- [ ] **Step 3: Implement** `config.FromEnv`. Create `main.go` that parses config, logs it with `slog`, and serves `GET {URLBase}/api/v1/ping` returning `{"version": version}` on `:Port`. Makefile targets: `build`, `test` (`go test ./...`), `generate` (`go tool sqlc generate`), `web` (`cd web && npm ci && npm run build`). `.gitignore`: `/softwarr`, `web/node_modules`, `web/dist/*` with `!web/dist/.gitkeep`.
- [ ] **Step 4: Run** `go test ./... && go build ./cmd/softwarr` and expect PASS and a binary.
- [ ] **Step 5: Commit** `feat: project skeleton and env config`.

---

### Task 2: Database, migrations, and sqlc

**Files:**
- Create: `internal/db/db.go`, `internal/db/migrations/00001_init.sql`, `internal/db/queries/users.sql` (first queries), `sqlc.yaml`, `internal/db/dbq/` (generated), `internal/testutil/db.go`, `internal/db/db_test.go`

**Interfaces:**
- Produces: `db.Open(path string) (*sql.DB, error)` (sets `journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout=5000`); `db.Migrate(ctx, *sql.DB) error` (goose with embedded `migrations` FS); `dbq.New(db) *dbq.Queries`; `testutil.NewDB(t) (*sql.DB, *dbq.Queries)` backed by a migrated temp file.
- Every later task adds its queries to `internal/db/queries/<area>.sql` and runs `make generate`.

Schema in `00001_init.sql` (column types are the builder's call; names and constraints are fixed because later tasks use them):

| Table | Columns | Constraints |
|---|---|---|
| `users` | id, username, email, password_hash, is_owner, is_admin, auto_approve, status (`active`/`pending`/`disabled`), must_change_password, created_at | username unique nocase; email unique nocase where not null; partial unique index on is_owner where is_owner = 1 |
| `user_identities` | id, user_id → users ON DELETE CASCADE, issuer, subject, created_at | unique (issuer, subject); unique user_id |
| `sessions` | token_hash (PK), user_id → users CASCADE, created_at, expires_at | |
| `oidc_states` | state (PK), code_verifier, nonce, link_user_id (nullable), expires_at | |
| `settings` | key (PK), value (JSON text) | |
| `download_clients` | id, name, type, protocol, enabled, priority, settings (JSON) | |
| `path_mappings` | id, host, remote_path, local_path | |
| `requests` | id, requester_id → users, status, release (JSON), release_key, original_release (JSON, nullable), comment, display_title, deny_reason, error, client_id, client_item_id, progress, last_progress_at, approved_by, library_item_id, created_at, updated_at | partial unique index on release_key where status in (`pending`,`approved`,`downloading`,`importing`,`scanning`) |
| `library_items` | id, request_id, display_title, release_name, release_key, folder, total_size, protocol, indexer, import_mode, retention_days (nullable), status (`available`/`expired`/`deleted`), imported_at, last_downloaded_at, download_count | |
| `library_files` | id, item_id → library_items CASCADE, rel_path, size | |
| `notifications` | id, user_id → users CASCADE, type, message, request_id, item_id, read_at, created_at | |
| `webhooks` | id, name, url, secret, events (JSON array), enabled | |

- [ ] **Step 1: Write the failing tests**

```go
func TestMigrateCreatesSchema(t *testing.T)        // NewDB succeeds; sqlite_master lists all 12 tables
func TestOnlyOneOwner(t *testing.T)                // inserting a second users row with is_owner=1 returns a constraint error
func TestOpenRequestKeyUnique(t *testing.T)        // two 'pending' requests with the same release_key fail; one 'denied' + one 'pending' succeed
```

- [ ] **Step 2: Run** `go test ./internal/db/...` and expect failure.
- [ ] **Step 3: Implement.** Add sqlc and goose as tools: `go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc`, `go get github.com/pressly/goose/v3`. `sqlc.yaml`: engine sqlite, schema `internal/db/migrations`, queries `internal/db/queries`, package `dbq`, out `internal/db/dbq`, `emit_json_tags: true`. Write `GetUserByID` and `CountUsers` in `users.sql` so generation has something to emit.
- [ ] **Step 4: Run** `make generate && go test ./internal/db/...` and expect PASS.
- [ ] **Step 5: Commit** `feat: sqlite schema, migrations, sqlc`.

---

### Task 3: Settings store

**Files:**
- Create: `internal/settings/settings.go`, `internal/settings/settings_test.go`, `internal/db/queries/settings.sql`

**Interfaces:**
- Produces:

```go
type Settings struct {
	ExternalURL          string   `json:"externalUrl"`
	APIKey               string   `json:"apiKey"`
	CommentRequired      bool     `json:"commentRequired"`
	DefaultRetentionDays int      `json:"defaultRetentionDays"` // 0 = never
	StallHours           int      `json:"stallHours"`
	LibraryRoot          string   `json:"libraryRoot"`
	Prowlarr             Prowlarr `json:"prowlarr"`
	OIDC                 OIDC     `json:"oidc"`
}
type Prowlarr struct { URL, APIKey string; Categories []int }
type OIDC struct {
	Enabled bool; Issuer, ClientID, ClientSecret, ButtonLabel string; Scopes []string
	AutoProvision, LinkVerifiedEmail bool; AdminClaim, AdminValue string
}
func NewStore(q *dbq.Queries) *Store
func (s *Store) Get(ctx) (Settings, error)   // cached after first load
func (s *Store) Save(ctx, Settings) error    // validates, persists, updates cache
```

- [ ] **Step 1: Write the failing tests**

```go
func TestGetReturnsDefaultsAndGeneratesAPIKey(t *testing.T) // Categories == [4000 4010 4020 4030]; StallHours == 24; OIDC.AutoProvision; !OIDC.LinkVerifiedEmail; ButtonLabel == "Sign in with SSO"; Scopes == [openid profile email]; len(APIKey) == 32 hex chars; second Get returns same key
func TestSaveRoundTrips(t *testing.T)
func TestSaveRejectsOIDCWithoutExternalURL(t *testing.T)     // OIDC.Enabled && ExternalURL == "" → error "Set the external URL before enabling OIDC."
func TestSaveRejectsNegativeRetention(t *testing.T)
```

- [ ] **Step 2: Run** `go test ./internal/settings/...` and expect failure.
- [ ] **Step 3: Implement.** Store the whole struct as JSON under key `app`. Merge stored JSON over defaults so new fields get defaults. Trim the trailing slash from `ExternalURL`.
- [ ] **Step 4: Run** the tests and expect PASS.
- [ ] **Step 5: Commit** `feat: settings store with defaults`.

---

### Task 4: Passwords and users service with owner rules

**Files:**
- Create: `internal/auth/password.go`, `internal/auth/users.go`, `internal/auth/users_test.go`, `internal/auth/password_test.go`, `internal/db/queries/users.sql` (extend)

**Interfaces:**
- Produces:

```go
func HashPassword(pw string) (string, error)          // argon2id, PHC string: $argon2id$v=19$m=19456,t=2,p=1$salt$hash
func VerifyPassword(hash, pw string) bool             // constant-time compare
type User struct { ID int64; Username, Email string; IsOwner, IsAdmin, AutoApprove, MustChangePassword, HasPassword, HasOIDC bool; Status string }
var ErrOwnerProtected = errors.New("The owner account can't be changed this way.")
var ErrSetupDone = errors.New("Setup has already been completed.")
var ErrInvalidCredentials = errors.New("Wrong username or password.")
var ErrAccountInactive = errors.New("This account isn't active. Ask an admin for help.")
type Users struct{ /* q, db */ }
func NewUsers(db *sql.DB, q *dbq.Queries) *Users
func (u *Users) NeedsSetup(ctx) (bool, error)
func (u *Users) Setup(ctx, username, password string) (User, error)          // only when zero users; creates owner, admin, active
func (u *Users) CreateLocal(ctx, username, email, password string, isAdmin bool) (User, error)
func (u *Users) Authenticate(ctx, username, password string) (User, error)   // ErrInvalidCredentials; pending/disabled → ErrAccountInactive
func (u *Users) Get(ctx, id int64) (User, error)
func (u *Users) List(ctx) ([]User, error)
type UserPatch struct { IsAdmin, AutoApprove *bool; Status *string; Email *string }
func (u *Users) Update(ctx, id int64, p UserPatch) (User, error)            // owner: IsAdmin=false or Status!=active → ErrOwnerProtected
func (u *Users) Delete(ctx, id int64) error                                  // owner → ErrOwnerProtected
func (u *Users) ChangePassword(ctx, id int64, current, next string) error    // clears must_change_password; min length 8
```

- [ ] **Step 1: Write the failing tests**

```go
func TestHashAndVerify(t *testing.T)               // Verify(hash,"pw") true; Verify(hash,"nope") false; hash has prefix "$argon2id$v=19$m=19456,t=2,p=1$"
func TestSetupCreatesOwnerOnce(t *testing.T)       // first Setup → IsOwner && IsAdmin; second Setup → ErrSetupDone
func TestOwnerCannotBeDemotedDisabledOrDeleted(t *testing.T)
func TestAuthenticateRejectsPendingAndDisabled(t *testing.T)
func TestUsernamesCaseInsensitiveUnique(t *testing.T)
func TestChangePasswordRequiresCurrent(t *testing.T)
func TestChangePasswordMinLength(t *testing.T)     // 7 chars → error "Passwords need at least 8 characters."
```

- [ ] **Step 2: Run** `go test ./internal/auth/...` and expect failure.
- [ ] **Step 3: Implement** using `golang.org/x/crypto/argon2.IDKey` with a 16-byte random salt and a 32-byte key. Run `Setup` in a transaction that rechecks the user count.
- [ ] **Step 4: Run** the tests and expect PASS.
- [ ] **Step 5: Commit** `feat: users, argon2id passwords, owner protections`.

---

### Task 5: Sessions, HTTP server, middleware, and auth endpoints

**Files:**
- Create: `internal/auth/sessions.go`, `internal/auth/principal.go`, `internal/httpapi/server.go`, `internal/httpapi/json.go`, `internal/httpapi/middleware.go`, `internal/httpapi/auth_handlers.go`, `internal/httpapi/testserver_test.go`, `internal/httpapi/auth_handlers_test.go`, `internal/db/queries/sessions.sql`
- Modify: `cmd/softwarr/main.go` (use `httpapi.New`)

**Interfaces:**
- Consumes: `settings.Store`, `auth.Users` (Tasks 3, 4).
- Produces:

```go
// auth
type Principal struct { UserID int64; Username string; IsOwner, IsAdmin, AutoApprove, MustChangePassword, ViaAPIKey bool }
func WithPrincipal(ctx, Principal) context.Context
func FromContext(ctx) (Principal, bool)
type Sessions struct{}
func NewSessions(q *dbq.Queries, now func() time.Time) *Sessions
func (s *Sessions) Create(ctx, userID int64) (token string, expires time.Time, err error) // 32 random bytes, base64url; DB stores sha256(token); 30-day lifetime
func (s *Sessions) Lookup(ctx, token string) (userID int64, err error)                    // extends expiry when < 15 days left
func (s *Sessions) Delete(ctx, token string) error
func (s *Sessions) DeleteForUser(ctx, userID int64) error

// httpapi
type Deps struct { DB *sql.DB; Q *dbq.Queries; Settings *settings.Store; Users *auth.Users; Sessions *auth.Sessions; URLBase string; Version string; Now func() time.Time /* later tasks append fields */ }
func New(d Deps) http.Handler
func writeJSON(w, status int, v any)
func writeError(w, status int, msg string)
func decodeJSON(r *http.Request, v any) error  // 1 MiB limit, DisallowUnknownFields
func requireUser(h http.HandlerFunc) http.HandlerFunc
func requireAdmin(h http.HandlerFunc) http.HandlerFunc
```

Middleware rules:
- Cookie `softwarr_session` (HttpOnly, SameSite=Lax, Path = URLBase + "/", Secure when `r.TLS != nil` or `X-Forwarded-Proto: https`).
- `X-Api-Key` equal to `Settings.APIKey` gives a Principal for the owner with `IsAdmin` and `ViaAPIKey` set.
- Cookie-authenticated requests with a method other than GET/HEAD must send `X-Softwarr-CSRF: 1`, or get 403 `"Missing CSRF header."`.
- If `MustChangePassword` is set, everything except `GET /auth/me`, `POST /auth/password`, and `POST /auth/logout` gets 403 `{"error":"password_change_required"}`.
- Unauthenticated → 401 `"Sign in to continue."`. Non-admin on an admin route → 403 `"Admins only."`.

Endpoints: `GET /setup` → `{"needed": bool}`; `POST /setup {username,password}` → 201 + session cookie; `POST /auth/login {username,password}` → 200 user + cookie, or 401 `"Wrong username or password."`; `POST /auth/logout` → 204; `GET /auth/me` → user; `POST /auth/password {current,new}` → 204.

- [ ] **Step 1: Write the failing tests** (`newTestServer(t) *testServer` wraps `httptest.Server`, a cookie jar, and helpers `do(method, path, body) *http.Response` that add the CSRF header, plus `loginAsOwner()`, `loginAsUser()`)

```go
func TestSetupFlow(t *testing.T)                      // GET /setup needed=true; POST /setup 201; GET /setup needed=false; POST /setup again 409
func TestLoginLogoutMe(t *testing.T)
func TestLoginWrongPassword401(t *testing.T)
func TestMutationWithoutCSRFHeader403(t *testing.T)
func TestAPIKeyActsAsAdmin(t *testing.T)              // X-Api-Key on an admin route → 200, no CSRF header needed
func TestMustChangePasswordBlocksOtherRoutes(t *testing.T)
func TestSessionExpires(t *testing.T)                 // advance fake clock 31 days → 401
func TestURLBasePrefix(t *testing.T)                  // URLBase "/sw": /sw/api/v1/ping 200, /api/v1/ping 404
```

- [ ] **Step 2: Run** `go test ./internal/httpapi/... ./internal/auth/...` and expect failure.
- [ ] **Step 3: Implement.** Route with `http.ServeMux` method patterns (`"POST /api/v1/auth/login"`), mounted under URLBase with `http.StripPrefix`. Add a recover middleware that logs and returns 500 `"Something went wrong."`.
- [ ] **Step 4: Run** the tests and expect PASS.
- [ ] **Step 5: Commit** `feat: sessions, auth middleware, login and setup endpoints`.

---

### Task 6: Owner password reset command

**Files:**
- Create: `internal/auth/reset.go`, `internal/auth/reset_test.go`
- Modify: `cmd/softwarr/main.go`

**Interfaces:**
- Produces: `func ResetOwnerPassword(ctx, db *sql.DB, q *dbq.Queries) (newPassword string, err error)`. Generates 20 random characters from `[A-Za-z0-9]`, sets the owner's hash, sets `must_change_password = 1`, and deletes all of the owner's sessions in one transaction. Returns `ErrNoOwner` (`"No owner account exists yet. Open softwarr in a browser to run setup."`) when there are no users.

- [ ] **Step 1: Write the failing tests**

```go
func TestResetOwnerPassword(t *testing.T) // after reset: Authenticate(owner, new) ok; old password fails; MustChangePassword true; owner's sessions gone; other users' sessions remain
func TestResetWithoutOwner(t *testing.T)  // ErrNoOwner
```

- [ ] **Step 2: Run** and expect failure.
- [ ] **Step 3: Implement**, and make `main` dispatch on `os.Args[1] == "reset-password"`: open `{DataDir}/softwarr.db`, migrate, reset, and print:

```
The owner password has been reset.
Username: <username>
Temporary password: <password>
You'll be asked to choose a new password after signing in.
```

- [ ] **Step 4: Run** the tests and expect PASS.
- [ ] **Step 5: Commit** `feat: reset-password command for the owner`.

---

### Task 7: User management and settings endpoints

**Files:**
- Create: `internal/httpapi/users_handlers.go`, `internal/httpapi/settings_handlers.go`, tests for both

**Interfaces:**
- Consumes: `auth.Users`, `settings.Store`.
- Produces: `GET /users` → `[]User`; `POST /users {username,email,password,isAdmin}` → 201; `PATCH /users/{id}` with `auth.UserPatch` JSON; `DELETE /users/{id}` → 204; `POST /users/{id}/approve` (pending → active); `GET /settings` → `settings.Settings`; `PUT /settings` → saved settings; `POST /settings/apikey` → `{"apiKey": "<new>"}`. `ErrOwnerProtected` maps to 409. Validation errors map to 400 with the error text.

- [ ] **Step 1: Write the failing tests**

```go
func TestUsersEndpointsAdminOnly(t *testing.T)       // regular user → 403 on each route
func TestCannotDemoteOrDeleteOwnerViaAPI(t *testing.T) // 409
func TestApprovePendingUser(t *testing.T)
func TestPutSettingsValidation(t *testing.T)         // OIDC enabled without external URL → 400 "Set the external URL before enabling OIDC."
func TestRegenerateAPIKey(t *testing.T)              // old key → 401, new key → 200
```

- [ ] **Step 2: Run** and expect failure.
- [ ] **Step 3: Implement** the handlers. Disabling or deleting a user also calls `Sessions.DeleteForUser`.
- [ ] **Step 4: Run** and expect PASS.
- [ ] **Step 5: Commit** `feat: user and settings admin endpoints`.

---

### Task 8: OIDC sign-in, provisioning, linking, and admin group sync

**Files:**
- Create: `internal/auth/oidc.go`, `internal/auth/oidc_resolve.go`, `internal/auth/oidc_resolve_test.go`, `internal/httpapi/oidc_handlers.go`, `internal/httpapi/oidc_handlers_test.go`, `internal/db/queries/oidc.sql`

**Interfaces:**
- Consumes: `settings.OIDC`, `auth.Users`, `auth.Sessions`.
- Produces:

```go
type Claims struct { Issuer, Subject, Email, PreferredUsername, Name string; EmailVerified bool; Raw map[string]any }
type Outcome int
const ( OutcomeLogin Outcome = iota; OutcomeCreated; OutcomePending; OutcomeLinked )
var ErrEmailTaken = errors.New("An account with this email already exists. Sign in with your password, then connect SSO from your profile.")
var ErrAwaitingApproval = errors.New("Your account is waiting for an admin to approve it.")
var ErrOwnerNoOIDC = errors.New("The owner account can't be connected to SSO.")
var ErrIdentityInUse = errors.New("That SSO account is already connected to another user.")
func ResolveLogin(ctx, tx *dbq.Queries, cfg settings.OIDC, c Claims, linkUserID *int64) (userID int64, out Outcome, err error)
func IsAdminByClaim(cfg settings.OIDC, raw map[string]any) (isAdmin bool, applies bool) // applies=false when AdminClaim is empty
type OIDC struct{}
func NewOIDC(settings *settings.Store, q *dbq.Queries, db *sql.DB, now func() time.Time) *OIDC
func (o *OIDC) Begin(ctx, linkUserID *int64) (authURL string, err error)                 // stores state, PKCE verifier, nonce (10-min expiry)
func (o *OIDC) Callback(ctx, state, code string) (userID int64, out Outcome, err error)
```

`ResolveLogin` decision order (this is the algorithm; the tests pin each branch):
1. If `linkUserID` is set: the target must be active, local, not the owner (`ErrOwnerNoOIDC`), and not linked yet. If (issuer, sub) belongs to another user, return `ErrIdentityInUse`. Otherwise insert the identity and return `OutcomeLinked`.
2. If (issuer, sub) is known: the user must be active (pending → `ErrAwaitingApproval`, disabled → `ErrAccountInactive`). Return `OutcomeLogin`.
3. If `Email` matches an existing user: when `cfg.LinkVerifiedEmail && c.EmailVerified` and that user isn't the owner, link and return `OutcomeLogin`. Otherwise return `ErrEmailTaken`.
4. Create a user (username from `PreferredUsername`, else the email local part, else `user-<first 8 of sub>`, adding `-2`, `-3`, … on collision; no password). Status is `active` if `cfg.AutoProvision`, else `pending`. Insert the identity. Return `OutcomeCreated`, or `OutcomePending` together with `ErrAwaitingApproval`.

After a successful login or creation, if `IsAdminByClaim` applies, set `is_admin` from it. The claim value can be a string or an array of strings.

HTTP: `GET /auth/oidc/config` → `{"enabled": bool, "buttonLabel": string}`. `GET /auth/oidc/login` → 302 to the provider. `POST /auth/oidc/link` (signed-in local non-owner) → `{"redirect": url}`. `GET /auth/oidc/callback` sets the session cookie and redirects to `{URLBase}/`, or to `{URLBase}/login?error=<urlencoded message>` on failure (and to `{URLBase}/profile?linked=1` after a link). The redirect URI is `{ExternalURL}/api/v1/auth/oidc/callback`, never built from request headers.

- [ ] **Step 1: Write the failing tests** (table-driven against `testutil.NewDB`)

```go
func TestResolveKnownIdentityLogsIn(t *testing.T)
func TestResolveNewIdentityAutoProvisions(t *testing.T)
func TestResolveNewIdentityPendingWhenProvisioningOff(t *testing.T)   // ErrAwaitingApproval, user row status=pending
func TestResolveEmailTakenRejected(t *testing.T)                     // no identity row created
func TestResolveVerifiedEmailLinksWhenEnabled(t *testing.T)
func TestResolveUnverifiedEmailNotLinkedEvenWhenEnabled(t *testing.T)
func TestResolveNeverLinksOwnerByEmail(t *testing.T)                 // LinkVerifiedEmail on, verified, email = owner's → ErrEmailTaken
func TestResolveLinkFromSessionRejectsOwner(t *testing.T)
func TestResolveLinkRejectsIdentityInUse(t *testing.T)
func TestAdminClaimGrantsAndRevokes(t *testing.T)                    // groups ["softwarr-admins"] → admin; next login groups [] → not admin
func TestAdminClaimIgnoredWhenUnset(t *testing.T)
func TestUsernameCollisionSuffix(t *testing.T)
func TestOIDCCallbackEndToEnd(t *testing.T)                          // github.com/oauth2-proxy/mockoidc as provider: login → callback → session cookie → GET /auth/me 200
func TestOIDCRedirectURIUsesExternalURL(t *testing.T)                // request with Host: evil.example → redirect_uri still {ExternalURL}/api/v1/auth/oidc/callback
```

- [ ] **Step 2: Run** `go test ./internal/auth/... ./internal/httpapi/...` and expect failure.
- [ ] **Step 3: Implement** with `go-oidc/v3` (provider discovery cached per issuer, ID token verification including nonce) and `oauth2` with `oauth2.S256ChallengeOption`. Run `ResolveLogin` inside a transaction. Delete expired `oidc_states` rows on each `Begin`.
- [ ] **Step 4: Run** and expect PASS.
- [ ] **Step 5: Commit** `feat: OIDC sign-in with provisioning, linking, admin group`.

---

### Task 9: Event broker and SSE endpoint

**Files:**
- Create: `internal/events/broker.go`, `internal/events/broker_test.go`, `internal/httpapi/events_handler.go`, `internal/httpapi/events_handler_test.go`

**Interfaces:**
- Produces:

```go
const ( TypeRequestUpdated = "request.updated"; TypeNotification = "notification"; TypeLibraryUpdated = "library.updated"; TypeActivity = "activity"; TypeHealth = "health" )
type Message struct { Type string; Data any; UserIDs []int64; Admins bool; All bool }
type Broker struct{}
func NewBroker() *Broker
func (b *Broker) Subscribe(userID int64, isAdmin bool) (ch <-chan Message, cancel func())
func (b *Broker) Publish(m Message) // a subscriber receives m when All, or its userID is in UserIDs, or (Admins && isAdmin). Non-blocking: drops for a subscriber whose 32-slot buffer is full.
```

`GET /events` writes `text/event-stream` with `event: <Type>\ndata: <json>\n\n`, sets `X-Accel-Buffering: no`, and sends `: ping` every 25s.

- [ ] **Step 1: Write the failing tests**

```go
func TestBrokerRouting(t *testing.T)        // user 1 gets UserIDs:[1]; user 2 doesn't; admin gets Admins:true; everyone gets All:true
func TestBrokerSlowSubscriberDoesNotBlock(t *testing.T)
func TestSSEEndpointStreams(t *testing.T)    // read first event from response body after Publish
```

- [ ] **Step 2: Run** and expect failure.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** and expect PASS.
- [ ] **Step 5: Commit** `feat: SSE event broker`.

---

### Task 10: Notifications and webhooks

**Files:**
- Create: `internal/notify/notify.go`, `internal/notify/webhook.go`, `internal/notify/notify_test.go`, `internal/httpapi/notifications_handlers.go`, `internal/httpapi/webhooks_handlers.go`, tests, `internal/db/queries/notifications.sql`

**Interfaces:**
- Consumes: `events.Broker`, `settings.Store` (ExternalURL for links).
- Produces:

```go
const ( EvRequestCreated = "request.created"; EvRequestApproved = "request.approved"; EvRequestDenied = "request.denied"; EvRequestFailed = "request.failed"; EvItemAvailable = "item.available"; EvItemExpired = "item.expired"; EvHealthWarning = "health.warning" )
type Event struct { Type string; Message string; RequestID, ItemID int64; ToUserIDs []int64; ToAdmins bool; Payload map[string]any }
type Service struct{}
func New(q *dbq.Queries, b *events.Broker, s *settings.Store, httpc *http.Client, now func() time.Time) *Service
func (s *Service) Emit(ctx, e Event) // inserts a notifications row per recipient (admins resolved at emit time), publishes TypeNotification, sends matching webhooks in goroutines
```

Webhook body: `{"event": Type, "timestamp": RFC3339, "message": Message, "requestId": id|null, "itemId": id|null, "url": "<ExternalURL>/requests/<id>" or "/library/<id>" or null, ...Payload}`. When `secret` is set, add `X-Softwarr-Signature: sha256=<hex HMAC-SHA256 of body>`. Timeout 10s. Failures are logged and not retried.

HTTP: `GET /notifications?unread=1` → newest 100 for the caller; `POST /notifications/read {"ids":[...]}` or `{"all":true}` → 204. Webhook CRUD with body `{name,url,secret,events,enabled}`. `POST /webhooks/{id}/test` sends `{"event":"test", ...}` and returns `{"ok":bool,"status":int,"error":string}`.

- [ ] **Step 1: Write the failing tests**

```go
func TestEmitToUserCreatesRowAndPublishes(t *testing.T)
func TestEmitToAdminsFansOut(t *testing.T)
func TestWebhookOnlySubscribedEvents(t *testing.T)   // httptest receiver; subscribed to item.available only
func TestWebhookSignature(t *testing.T)              // receiver recomputes HMAC and compares
func TestNotificationsOnlyOwn(t *testing.T)          // user A can't mark user B's notification read (ids silently scoped to caller)
```

- [ ] **Step 2: Run** and expect failure.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** and expect PASS.
- [ ] **Step 5: Commit** `feat: in-app notifications and webhooks`.

---

### Task 11: Prowlarr client and search endpoint

**Files:**
- Create: `internal/prowlarr/client.go`, `internal/prowlarr/release.go`, `internal/prowlarr/client_test.go`, `internal/httpapi/search_handlers.go`, `internal/httpapi/search_handlers_test.go`, `internal/db/queries/search.sql`

**Interfaces:**
- Produces:

```go
type Release struct {
	GUID string `json:"guid"`; Title string `json:"title"`; Indexer string `json:"indexer"`; IndexerID int `json:"indexerId"`
	Protocol string `json:"protocol"` // "torrent" | "usenet"
	Size int64 `json:"size"`; PublishDate time.Time `json:"publishDate"`
	Seeders, Leechers, Grabs *int
	DownloadURL string `json:"downloadUrl"`; MagnetURL string `json:"magnetUrl"`; InfoHash string `json:"infoHash"`
	Categories []int `json:"categories"`
}
func (r Release) Key() string // "ih:"+lower(InfoHash) if set; else "guid:"+GUID if set; else "title:"+Title
type Indexer struct { ID int; Name string; Protocol string; Enabled bool }
type Client struct{}
func New(baseURL, apiKey string, httpc *http.Client) *Client
func (c *Client) Search(ctx, query string, categories, indexerIDs []int) ([]Release, error) // GET /api/v1/search?query=&type=search&categories=..&indexerIds=..
func (c *Client) Indexers(ctx) ([]Indexer, error)                                            // GET /api/v1/indexer
func (c *Client) Ping(ctx) error                                                              // GET /api/v1/system/status
```

All calls send `X-Api-Key`. The `httpc` timeout is 60s. Prowlarr's JSON uses `categories: [{id, name}]`, so flatten it to IDs. Pull the infohash from the magnet's `xt=urn:btih:` when `infoHash` is empty (use Task 12's `torrentfile.ParseMagnet` once it exists; until then, a local helper with a TODO is fine. Task 12 replaces it.)

Search endpoint: `GET /search?q=<text>&indexerIds=1,2` (non-admins can't pass `indexerIds`; ignore it for them). The response is `[]SearchResult`:

```go
type SearchResult struct { prowlarr.Release; Key string `json:"key"`; Status string `json:"status"` /* "available" | "requested" | "requestable" | "no_client" */; LibraryItemID *int64 `json:"libraryItemId"` }
```

Status order: `available` if an `available` library item has the same `release_key`, then `requested` if an open request has it, then `no_client` if no enabled download client has that protocol, else `requestable`. Results are sorted by publish date, newest first. An empty `q` → 400 `"Enter something to search for."`. Prowlarr not configured → 409 `"Prowlarr isn't set up yet. An admin can add it in Settings > Indexers."`. Prowlarr errors or timeouts → 502 `"Couldn't reach Prowlarr: <err>"`.

Also: `GET /indexers` (admin) and `POST /settings/prowlarr/test {url, apiKey}` (admin) → `{"ok":bool,"error":string}`.

- [ ] **Step 1: Write the failing tests** (fake Prowlarr with `httptest`, fixture JSON in `internal/prowlarr/testdata/search.json` containing one torrent with an infohash, one torrent with only a magnet, and one usenet release)

```go
func TestSearchParsesReleases(t *testing.T)            // 3 releases; categories flattened; magnet-only release gets InfoHash
func TestSearchSendsAPIKeyAndCategories(t *testing.T)  // fake asserts X-Api-Key and categories=4000&categories=4010...
func TestReleaseKeyPrecedence(t *testing.T)
func TestSearchStatusBadges(t *testing.T)              // seed library item + open request + one qbittorrent client: torrent A → available, torrent B → requested, usenet → no_client
func TestSearchNonAdminCannotFilterIndexers(t *testing.T)
func TestSearchProwlarrUnavailable(t *testing.T)       // fake returns 500, then a closed server → 502 with "Couldn't reach Prowlarr"
func TestSearchProwlarrTimeout(t *testing.T)           // inject 100ms client timeout, fake sleeps 1s → 502
```

- [ ] **Step 2: Run** and expect failure.
- [ ] **Step 3: Implement.** `Deps` gains `NewProwlarr func(url, key string) *prowlarr.Client` so tests can shorten the timeout.
- [ ] **Step 4: Run** and expect PASS.
- [ ] **Step 5: Commit** `feat: Prowlarr search with availability badges`.

---

### Task 12: Torrent infohash helpers

**Files:**
- Create: `internal/torrentfile/torrentfile.go`, `internal/torrentfile/torrentfile_test.go`, `internal/torrentfile/testdata/sample.torrent`
- Modify: `internal/prowlarr/client.go` (use `ParseMagnet`)

**Interfaces:**
- Produces: `func InfoHash(torrent []byte) (string, error)` returns the lowercase hex SHA-1 of the bencoded `info` dict, as raw bytes from the file (don't re-encode it). `func ParseMagnet(uri string) (hash string, err error)` accepts 40-char hex and 32-char base32 `urn:btih:` and returns lowercase hex.

- [ ] **Step 1: Write the failing tests**

```go
func TestInfoHashMatchesKnown(t *testing.T)   // sample.torrent → known hash (generate the fixture with a tiny Go test helper or mktorrent, record the hash in the test)
func TestInfoHashRejectsGarbage(t *testing.T)
func TestParseMagnetHexAndBase32(t *testing.T) // same torrent as hex and base32 → same lowercase hex
```

- [ ] **Step 2: Run** and expect failure.
- [ ] **Step 3: Implement** a minimal bencode scanner that finds the byte span of the top-level `info` value. No full decoding library is needed.
- [ ] **Step 4: Run** and expect PASS.
- [ ] **Step 5: Commit** `feat: torrent infohash and magnet parsing`.

---

### Task 13: Download client interface and qBittorrent

**Files:**
- Create: `internal/downloadclient/client.go`, `internal/downloadclient/registry.go`, `internal/downloadclient/qbittorrent/qbittorrent.go`, `internal/downloadclient/qbittorrent/qbittorrent_test.go`, `internal/downloadclient/qbittorrent/integration_test.go` (build tag `integration`), `internal/httpapi/downloadclients_handlers.go`, tests, `internal/db/queries/downloadclients.sql`

**Interfaces:**
- Produces:

```go
type State string
const ( StateQueued State = "queued"; StateDownloading = "downloading"; StateCompleted = "completed"; StateError = "error"; StateMissing = "missing" )
type File struct { Path string; Size int64 } // Path relative to SavePath, forward slashes
type Item struct { ID, Name, SavePath string; Progress float64; State State; Message string; Files []File }
type AddRequest struct { TorrentFile []byte; MagnetURI string; InfoHash string }
type Client interface {
	Protocol() string
	Test(ctx context.Context) error
	Add(ctx context.Context, req AddRequest) (id string, err error)
	Get(ctx context.Context, id string) (Item, error) // StateMissing when the client no longer has it
	Host() string                                      // for path mapping lookup
}
type Config struct { ID int64; Name, Type string; Enabled bool; Priority int; Settings json.RawMessage }
type Factory func(Config, *http.Client) (Client, error)
func Register(typ, protocol string, f Factory)
func New(cfg Config, httpc *http.Client) (Client, error)
func Types() []TypeInfo // {Type, Protocol, Name}; for the UI
```

qBittorrent settings JSON: `{host, port, useTls, urlBase, username, password, category}` (category default `softwarr`). Behavior:
- Log in with `POST /api/v2/auth/login` and keep the SID cookie. On a 403 from any call, log in again once and retry.
- `Add` posts multipart `torrents/add` with `torrents` (file) or `urls` (magnet), `category`, and `tags=softwarr`. The ID is the lowercase v1 infohash from `req.InfoHash`. If qBittorrent answers `Fails.`, call `Get` and return the ID if the torrent is already there (re-attach).
- `Get` uses `torrents/info?hashes=<id>` and `torrents/files?hash=<id>`. Completed when `progress == 1` and the state is one of `uploading, stalledUP, pausedUP, stoppedUP, queuedUP, forcedUP, checkingUP`. Error states: `error, missingFiles`. An empty list → `StateMissing`.
- `Test` uses `GET /api/v2/app/version`.

HTTP: CRUD at `/downloadclients` with body `{name,type,enabled,priority,settings}` (protocol comes from the registry). `GET /downloadclients/types`. `POST /downloadclients/test` takes an unsaved config and returns `{"ok":bool,"error":string}`. Responses mask `password` as `"********"`, and a PUT carrying that mask keeps the stored password.

Add `func PickClient(ctx, q *dbq.Queries, protocol string) (Config, bool, error)` in `registry.go`: the lowest `priority` value among enabled clients with that protocol, ties broken by ID.

- [ ] **Step 1: Write the failing tests** (fake qBittorrent with `httptest` that tracks cookies and torrents)

```go
func TestQbitAddUploadsFileWithCategoryAndTag(t *testing.T)
func TestQbitAddMagnet(t *testing.T)
func TestQbitAddDuplicateReattaches(t *testing.T)
func TestQbitGetCompletedStates(t *testing.T)     // table over the 7 complete states + "downloading" + "error"
func TestQbitGetMissing(t *testing.T)
func TestQbitRelogsOnForbidden(t *testing.T)      // fake expires SID after first call → second call logs in again and succeeds; a second consecutive 403 returns an error
func TestPickClientByPriority(t *testing.T)
func TestDownloadClientPasswordMasked(t *testing.T)
```

`integration_test.go` (`//go:build integration`) reads `QBT_URL`, `QBT_USER`, and `QBT_PASS`, adds `testdata/sample.torrent`, and asserts `Get` returns it. Run it manually against a real container.

- [ ] **Step 2: Run** `go test ./internal/downloadclient/... ./internal/httpapi/...` and expect failure.
- [ ] **Step 3: Implement.** Register `qbittorrent` (protocol `torrent`) from the package's `init`, and blank-import it in `main.go`.
- [ ] **Step 4: Run** and expect PASS.
- [ ] **Step 5: Commit** `feat: download client registry and qBittorrent`.

---

### Task 14: Remote path mappings

**Files:**
- Create: `internal/pathmap/pathmap.go`, `internal/pathmap/pathmap_test.go`, `internal/httpapi/pathmappings_handlers.go`, tests, `internal/db/queries/pathmappings.sql`

**Interfaces:**
- Produces: `type Mapping struct { ID int64; Host, RemotePath, LocalPath string }`; `func Map(ms []Mapping, host, remote string) string` uses the longest matching `RemotePath` prefix for that host (case-insensitive host, whole path segments only) and returns `remote` unchanged when nothing matches. CRUD at `/pathmappings`. Writes normalize both paths with a trailing `/`.

- [ ] **Step 1: Write the failing tests**

```go
func TestMapLongestPrefix(t *testing.T)    // /downloads/ and /downloads/complete/ both mapped → second wins
func TestMapSegmentBoundary(t *testing.T)  // mapping /data/ must not match /database/x
func TestMapOtherHostIgnored(t *testing.T)
func TestMapNoMatchReturnsInput(t *testing.T)
```

- [ ] **Steps 2 to 4:** Run (fail), implement, run (pass).
- [ ] **Step 5: Commit** `feat: remote path mappings`.

---

### Task 15: Request state machine, service, and endpoints

**Files:**
- Create: `internal/requests/state.go`, `internal/requests/service.go`, `internal/requests/state_test.go`, `internal/requests/service_test.go`, `internal/httpapi/requests_handlers.go`, tests, `internal/db/queries/requests.sql`

**Interfaces:**
- Consumes: `prowlarr.Release`, `notify.Service`, `events.Broker`, `settings.Store`, `downloadclient.PickClient`.
- Produces:

```go
type Status string
const ( Pending Status = "pending"; Approved = "approved"; Downloading = "downloading"; Importing = "importing"; Available = "available"; Denied = "denied"; Cancelled = "cancelled"; Failed = "failed"; Abandoned = "abandoned" )
type Action string
const ( ActApprove Action = "approve"; ActDeny = "deny"; ActCancel = "cancel"; ActGrabbed = "grabbed"; ActGrabFailed = "grab_failed"; ActCompleted = "completed"; ActClientError = "client_error"; ActImported = "imported"; ActImportFailed = "import_failed"; ActAbandon = "abandon"; ActRetry = "retry" )
func Next(from Status, a Action) (Status, error) // ErrInvalidTransition
func IsOpen(s Status) bool                       // pending, approved, downloading, importing

type Request struct { ID int64; RequesterID int64; Requester string; Status Status; Release prowlarr.Release; OriginalRelease *prowlarr.Release; Comment, DisplayTitle, DenyReason, Error string; Progress float64; LibraryItemID *int64; CreatedAt, UpdatedAt time.Time }
type ApproveOpts struct { DisplayTitle string; Replacement *prowlarr.Release }
var ErrConflict = errors.New("This request was changed by someone else. Refresh and try again.")
var ErrDuplicate = errors.New("This release has already been requested.")
var ErrInLibrary = errors.New("This release is already in the library.")
var ErrCommentRequired = errors.New("Please say why you need this software.")
var ErrNoClient = errors.New("No compatible download client")
var ErrNoLibraryRoot = errors.New("Set a library folder in Settings > Library before approving requests.")
var ErrNotFound = errors.New("Request not found.")
type Service struct{}
func NewService(db *sql.DB, q *dbq.Queries, s *settings.Store, n *notify.Service, b *events.Broker, now func() time.Time) *Service
func (s *Service) Create(ctx, p auth.Principal, rel prowlarr.Release, comment string) (Request, error)
func (s *Service) Approve(ctx, p auth.Principal, id int64, o ApproveOpts) (Request, error)
func (s *Service) Deny(ctx, p auth.Principal, id int64, reason string) (Request, error)
func (s *Service) Cancel(ctx, p auth.Principal, id int64) (Request, error) // only own, only pending
func (s *Service) Abandon(ctx, p auth.Principal, id int64) (Request, error)
func (s *Service) Retry(ctx, p auth.Principal, id int64) (Request, error)  // failed → approved; clears Error
func (s *Service) Get(ctx, p auth.Principal, id int64) (Request, error)    // non-admin, not own → ErrNotFound
func (s *Service) List(ctx, p auth.Principal, all bool) ([]Request, error) // all only for admins
func (s *Service) transition(ctx, id int64, a Action, mutate func(*dbq.UpdateRequestParams)) (Request, error) // UPDATE ... WHERE id=? AND status=? ; 0 rows → ErrConflict
```

Transition table (the tests pin every row): pending→{approve: approved, deny: denied, cancel: cancelled}; approved→{grabbed: downloading, grab_failed: failed, abandon: abandoned}; downloading→{completed: importing, client_error: failed, abandon: abandoned}; importing→{imported: available, import_failed: failed}; failed→{retry: approved, abandon: abandoned}. Every other pair → `ErrInvalidTransition`.

Create rules: the comment is trimmed and required when `CommentRequired`; the release's protocol must have an enabled client (`ErrNoClient`); `ErrInLibrary` if an available library item has the key; `ErrDuplicate` if the open-key unique index is violated. Admins and users with `AutoApprove` start at `approved` with `approved_by` set to themselves. Create emits `request.created` to admins only when the request is pending.

Approve rules: `LibraryRoot` must be set. A replacement moves the current release to `original_release`, sets the new release and key, and runs the duplicate check again. Emits `request.approved` to the requester. Deny emits `request.denied` with the reason. Every transition publishes `events.TypeRequestUpdated` to the requester and admins.

HTTP: `POST /requests {release, comment}` → 201; `GET /requests` (own) or `?all=1` (admin); `GET /requests/{id}`; `POST /requests/{id}/approve {displayTitle, replacement?}`; `/deny {reason}`; `/cancel`; `/abandon`; `/retry`. Error mapping: `ErrConflict`, `ErrDuplicate`, `ErrInLibrary`, `ErrInvalidTransition` → 409; `ErrCommentRequired`, `ErrNoClient` → 400; `ErrNoLibraryRoot` → 409; not found → 404. Non-admin responses for other users' requests are 404, and the comment is only returned to the requester and admins.

- [ ] **Step 1: Write the failing tests**

```go
func TestTransitionTable(t *testing.T)                    // every allowed row + a sample of rejected pairs
func TestCreatePendingForUser(t *testing.T)
func TestCreateAutoApprovedForAdminAndFlaggedUser(t *testing.T)
func TestCreateCommentRequired(t *testing.T)
func TestCreateDuplicateRejected(t *testing.T)            // message exactly "This release has already been requested."
func TestCreateAfterDenyAllowed(t *testing.T)
func TestCreateInLibraryRejected(t *testing.T)
func TestCreateNoClientRejected(t *testing.T)
func TestApproveWithReplacementKeepsOriginal(t *testing.T)
func TestApproveRequiresLibraryRoot(t *testing.T)
func TestApproveTwiceSecondGetsConflict(t *testing.T)     // two goroutines Approve the same id concurrently → exactly one success, one ErrConflict
func TestCancelOnlyOwnPending(t *testing.T)
func TestUserCannotSeeOthersRequests(t *testing.T)        // HTTP: GET /requests/{other} → 404; GET /requests?all=1 as user → only own
func TestNotificationsOnApproveAndDeny(t *testing.T)
```

- [ ] **Step 2: Run** and expect failure.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** and expect PASS.
- [ ] **Step 5: Commit** `feat: request lifecycle and endpoints`.

---

### Task 16: Importer

**Files:**
- Create: `internal/importer/importer.go`, `internal/importer/sanitize.go`, `internal/importer/importer_test.go`, `internal/importer/link_unix.go`

**Interfaces:**
- Produces:

```go
type Mode string
const ( ModeHardlink Mode = "hardlink"; ModeCopy Mode = "copy"; ModeMove Mode = "move" )
type Source struct { BaseDir string; Files []downloadclient.File; Protocol string }
type Result struct { Folder string /* absolute */; Mode Mode; Files []downloadclient.File /* rel to Folder */; TotalSize int64 }
func SanitizeName(name string) string
func Import(ctx, src Source, libraryRoot, releaseName string) (Result, error)
var ErrUnsafePath = errors.New("unsafe file path in download")
```

Rules:
- `SanitizeName` replaces `/ \ : * ? " < > |` and control characters with `_`, trims spaces and dots from both ends, caps the name at 200 bytes on a UTF-8 boundary, and returns `"untitled"` if nothing is left.
- Folder: `libraryRoot/SanitizeName(releaseName)`, or with ` (2)`, ` (3)`, … when it exists.
- If every file shares one top-level directory, strip it so files aren't nested twice.
- Every source and destination path is cleaned and must stay inside its base after `filepath.Clean`. A file path that is absolute or contains `..` → `ErrUnsafePath`, and the new folder is removed.
- Torrents: `os.Link`. On a `syscall.EXDEV` error, copy that file and every remaining one, and report `ModeCopy`. Any other link error fails the import. Usenet: `os.Rename` (moving is out of scope until phase 2, but implement it now since it's a few lines and the tests cover it).
- On any failure, remove the partially created folder. Never touch files under `BaseDir` except when moving.

- [ ] **Step 1: Write the failing tests** (temp dirs; the EXDEV fallback is tested by injecting `var link = os.Link` and replacing it with a func returning `&os.LinkError{Err: syscall.EXDEV}`)

```go
func TestSanitizeName(t *testing.T)                 // table: "a/b:c" → "a_b_c"; "  .x.  " → "x"; "" → "untitled"; 300×"é" ≤ 200 bytes and valid UTF-8
func TestImportHardlinks(t *testing.T)              // os.SameFile(src, dst) true; Mode hardlink
func TestImportStripsCommonRoot(t *testing.T)
func TestImportFolderCollisionSuffix(t *testing.T)
func TestImportFallsBackToCopyOnEXDEV(t *testing.T) // Mode copy; contents equal; !SameFile
func TestImportRejectsEscapingPaths(t *testing.T)   // file "../../etc/x" and "/abs" → ErrUnsafePath; library root has no leftover folder
func TestImportSourceUntouched(t *testing.T)        // source mtime and contents unchanged after hardlink and copy
```

- [ ] **Steps 2 to 4:** Run (fail), implement, run (pass).
- [ ] **Step 5: Commit** `feat: importer with hardlink and copy fallback`.

---

### Task 17: Request worker (grab, poll, stall, import) and activity endpoint

**Files:**
- Create: `internal/requests/worker.go`, `internal/requests/worker_test.go`, `internal/library/items.go` (create/insert only; Task 18 adds the rest), `internal/httpapi/activity_handler.go`, tests, `internal/db/queries/library.sql`

**Interfaces:**
- Consumes: `downloadclient.New/PickClient`, `torrentfile.InfoHash/ParseMagnet`, `pathmap.Map`, `importer.Import`, `notify.Service`, `events.Broker`.
- Produces:

```go
type Worker struct{}
func NewWorker(svc *Service, q *dbq.Queries, s *settings.Store, httpc *http.Client, newClient func(downloadclient.Config) (downloadclient.Client, error), now func() time.Time) *Worker
func (w *Worker) Tick(ctx) error          // one pass over approved, downloading, importing
func (w *Worker) Run(ctx, every time.Duration) // calls Tick every 15s until ctx is done
type Activity struct { RequestID int64; Title, Requester string; Status Status; Progress float64; Stalled bool; Error string; UpdatedAt time.Time } // in package requests
func (s *Service) Activity(ctx) ([]Activity, error)
// library/items.go (package library must not import requests, which imports it)
func CreateItem(ctx, q *dbq.Queries, requestID int64, displayTitle string, rel prowlarr.Release, res importer.Result, now time.Time) (int64, error)
```

Tick:
1. `approved`: pick a client for the protocol (`grab_failed` with `"No compatible download client"` if none). For torrents: if `DownloadURL` is set, GET it (60s timeout). If the body is a torrent, compute the infohash and add the file; if the response redirects to a `magnet:` URI, use it. Otherwise use `MagnetURL`. On success, store `client_id` and `client_item_id`, set `last_progress_at = now`, and apply `grabbed`. On failure, apply `grab_failed` with the error text and emit `request.failed` to the requester and admins.
2. `downloading`: `Get`. If progress rose, update it and `last_progress_at`, and publish `TypeActivity` to admins and `TypeRequestUpdated` to the requester. `StateCompleted` → `completed`. `StateError` or `StateMissing` → `client_error` with the message (emit `request.failed`).
3. `importing`: map the client's `SavePath` through path mappings using `client.Host()`, call `importer.Import`, create the library item and files, set `library_item_id`, apply `imported`, emit `item.available` to the requester (and webhooks), and publish `TypeLibraryUpdated` to all. An import error → `import_failed` + `request.failed`.

Retry on a `failed` request whose `client_item_id` is set: the next tick in `approved` calls `Get` first, and if the client still has the item, it applies `grabbed` without adding it again.

Each request is handled on its own, and one failure never stops the tick. A transition that returns `ErrConflict` (an admin abandoned it meanwhile) is skipped silently.

`GET /activity` (admin): requests in `approved`, `downloading`, `importing`, or `failed`, as `[]Activity`. `Stalled` = downloading and `now - last_progress_at > StallHours`. No notification is sent for stalls.

- [ ] **Step 1: Write the failing tests** (fake `downloadclient.Client` in the test file, fake indexer download server, temp dirs as client save path and library root)

```go
func TestWorkerGrabsTorrentFile(t *testing.T)          // status downloading, client got TorrentFile bytes and InfoHash
func TestWorkerGrabFollowsMagnetRedirect(t *testing.T)
func TestWorkerGrabStaleURLFails(t *testing.T)         // 404 → failed with error text; requester notified
func TestWorkerTracksProgress(t *testing.T)
func TestWorkerCompletesAndImports(t *testing.T)       // two ticks: downloading→importing→available; library item exists with files; item.available notification
func TestWorkerUsesPathMapping(t *testing.T)
func TestWorkerClientMissingFails(t *testing.T)
func TestWorkerRetryReattaches(t *testing.T)           // failed with client_item_id; Retry; tick → downloading, client.Add not called
func TestWorkerSkipsAbandoned(t *testing.T)            // abandon between list and transition → no error, no client call after
func TestActivityStalledFlag(t *testing.T)             // last_progress_at 25h ago → Stalled true; 23h → false
```

- [ ] **Step 2: Run** and expect failure.
- [ ] **Step 3: Implement.** Start `Worker.Run` from `main`.
- [ ] **Step 4: Run** and expect PASS.
- [ ] **Step 5: Commit** `feat: request worker and activity endpoint`.

---

### Task 18: Library browsing, delivery, and retention

**Files:**
- Create: `internal/library/service.go`, `internal/library/delivery.go`, `internal/library/retention.go`, tests for each, `internal/httpapi/library_handlers.go`, `internal/httpapi/library_handlers_test.go`

**Interfaces:**
- Produces:

```go
type Item struct { ID int64; Title, ReleaseName string; TotalSize int64; Protocol, Indexer, Requester, Comment, Approver, ImportMode string; RetentionDays *int; ImportedAt time.Time; LastDownloadedAt *time.Time; DownloadCount int; Files []File; ExpiresAt *time.Time }
type File struct { Path string; Size int64 }
type Service struct{}
func NewService(q *dbq.Queries, s *settings.Store, n *notify.Service, b *events.Broker, now func() time.Time) *Service
func (s *Service) List(ctx, filter string) ([]Item, error) // status=available; filter matches title or release name, case-insensitive
func (s *Service) Get(ctx, id int64) (Item, error)
func (s *Service) Update(ctx, id int64, title *string, retentionDays **int) (Item, error) // retentionDays: nil = unchanged, *nil = use default
func (s *Service) Delete(ctx, id int64) error // removes the library folder (must be inside LibraryRoot) and marks status=deleted
func ExpiresAt(it Item, defaultDays int) *time.Time // nil when the effective days == 0
func (s *Service) ApplyRetention(ctx) (expired int, err error) // removes the folder, status=expired, emits item.expired
func (s *Service) RunRetention(ctx) // at startup, then every 24h
func Serve(w http.ResponseWriter, r *http.Request, folder string, it Item) error
var ErrFilesMissing = errors.New("Files for this item are missing on disk.")
```

`Item.Comment` is filled only for admins and the requester. The handler clears it for others.

Retention: effective days = the item's `RetentionDays` if non-nil, else `DefaultRetentionDays`. 0 means never. Reference time = the later of `ImportedAt` and `LastDownloadedAt`. Expired when `now > ref + days*24h`.

Delivery (`Serve`):
- Stat every file first. If any is missing, return `ErrFilesMissing` before writing (the handler maps it to 410).
- Exactly one file whose lowercase extension is in `{.zip .7z .rar .dmg .iso .pkg .mpkg .exe .msi .img .tar .gz .tgz .xz .bz2 .zst}` → `http.ServeContent` with `Content-Disposition: attachment; filename="<base name>"`.
- Otherwise stream `archive/zip` with `Method: zip.Store`, names = rel paths with forward slashes, and `Content-Disposition: attachment; filename="<SanitizeName(title)>.zip"`, with no Content-Length. Set `UncompressedSize64` on each header so large files get zip64.
- After the response starts, increment `download_count` and set `last_downloaded_at` (for range requests, only when the range starts at 0).

HTTP: `GET /library?q=`; `GET /library/{id}`; `GET /library/{id}/download`; admin `PATCH /library/{id} {"title": string|null, "retentionDays": int|null|absent}`; admin `DELETE /library/{id}` → 204.

- [ ] **Step 1: Write the failing tests**

```go
func TestExpiresAt(t *testing.T)                         // table: default 30 & override nil → imported+30d; override 0 → nil; default 0 & override nil → nil; downloaded later moves the date
func TestApplyRetentionRemovesOnlyLibraryCopy(t *testing.T) // source file hardlinked elsewhere still exists; folder gone; status expired; item.expired emitted
func TestServeSingleArchiveDirect(t *testing.T)          // .dmg → Content-Type not zip; Range: bytes=0-9 → 206
func TestServeMultiFileZip(t *testing.T)                 // read body with zip.NewReader: names and contents match; entries use Store
func TestServeSingleNonArchiveZipped(t *testing.T)       // one .app bundle file or .txt → zip
func TestDownloadMissingFileReturnsGone(t *testing.T)    // delete one file → 410 with {"error":"Files for this item are missing on disk."}, no zip bytes
func TestDownloadUpdatesCountAndTime(t *testing.T)
func TestLibraryCommentHiddenFromOtherUsers(t *testing.T)
func TestLibraryAdminOnlyMutations(t *testing.T)
func TestDeleteRefusesFolderOutsideRoot(t *testing.T)    // tampered folder value → error, nothing removed
```

- [ ] **Step 2: Run** and expect failure.
- [ ] **Step 3: Implement**, and start `RunRetention` from `main`.
- [ ] **Step 4: Run** and expect PASS.
- [ ] **Step 5: Commit** `feat: library delivery and retention`.

---

### Task 19: Health checks, logs, and system status

**Files:**
- Create: `internal/health/health.go`, `internal/health/health_test.go`, `internal/logbuf/logbuf.go`, `internal/logbuf/logbuf_test.go`, `internal/httpapi/system_handlers.go`, tests
- Modify: `cmd/softwarr/main.go` (install the `logbuf` handler via `slog.SetDefault`, teeing to stderr)

**Interfaces:**
- Produces:

```go
type Check struct { ID, Level /* "ok" | "warning" | "error" */, Message string }
type Checker struct{}
func NewChecker(q *dbq.Queries, s *settings.Store, newProwlarr func(url, key string) *prowlarr.Client, newClient func(downloadclient.Config) (downloadclient.Client, error), n *notify.Service, b *events.Broker) *Checker
func (c *Checker) Run(ctx) []Check
func (c *Checker) Loop(ctx, every time.Duration) // every 5 min; emits health.warning to admins for checks that weren't failing on the previous run; publishes TypeHealth
// logbuf
func New(capacity int, next slog.Handler) *Handler
func (h *Handler) Entries() []Entry // Entry{Time, Level, Message, Attrs map[string]any}
```

Checks and their messages (each `Message` goes through the humanizer):
- `prowlarr`: not configured → warning "Prowlarr isn't set up." Ping fails → error "Can't reach Prowlarr: <err>".
- `downloadclient:<id>`: Test fails → error "Can't reach download client <name>: <err>".
- `noclient`: no enabled client → warning "No download client is enabled, so requests can't be downloaded."
- `library`: root empty → warning "No library folder is set." Missing or not writable → error "The library folder <path> doesn't exist or isn't writable."
- `pathmapping:<id>`: local path not readable → error "softwarr can't read <local path> (mapped from <remote>)."
- `hardlinks`: the most recent torrent import used copy → warning "The last import was copied instead of hardlinked, which uses extra disk space. Put the download and library folders on the same filesystem to fix this."
- `oidc`: enabled but provider discovery fails → error "Can't reach the OIDC provider: <err>".

HTTP (admin): `GET /system/health` → `[]Check`; `GET /system/logs` → last 500 entries, newest first; `GET /system/status` → `{"version", "goVersion", "dataDir", "urlBase", "startedAt"}`.

- [ ] **Step 1: Write the failing tests**

```go
func TestHealthAllOK(t *testing.T)
func TestHealthLibraryMissing(t *testing.T)
func TestHealthHardlinkFallbackWarning(t *testing.T)
func TestHealthLoopNotifiesOnlyOnNewFailures(t *testing.T) // two runs with the same failure → one notification
func TestLogBufCapacity(t *testing.T)                     // capacity 3, 5 records → last 3
```

- [ ] **Steps 2 to 4:** Run (fail), implement, run (pass).
- [ ] **Step 5: Commit** `feat: health checks, log buffer, system status`.

---

### Task 20: Frontend scaffold, theme, API client, and SPA embedding

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/index.html`, `web/src/main.tsx`, `web/src/theme.ts`, `web/src/api/client.ts`, `web/src/api/client.test.ts`, `web/src/api/types.ts`, `web/src/test/setup.ts`, `web/dist/.gitkeep`, `web/embed.go`, `internal/httpapi/spa.go`, `internal/httpapi/spa_test.go`

**Interfaces:**
- Produces:
  - `web/embed.go`: `package web; //go:embed all:dist; var Dist embed.FS`.
  - `httpapi` serves `Dist` for non-API paths. Unknown paths that don't name a file return `index.html` (SPA fallback). `index.html` is served with `<head>` gaining `<base href="{URLBase}/">` and `<script>window.__SOFTWARR__={"urlBase":"{URLBase}"}</script>`. Hashed assets get `Cache-Control: public, max-age=31536000, immutable`. If `index.html` is missing, `/` returns 503 `"The web UI hasn't been built. Run make web."`.
  - `web/src/api/client.ts`: `export const urlBase: string`; `export class ApiError extends Error { status: number }`; `export async function api<T>(path: string, init?: RequestInit & { json?: unknown }): Promise<T>`. It prefixes `${urlBase}/api/v1`, adds `X-Softwarr-CSRF: 1` on non-GET requests, sends `credentials: 'same-origin'`, parses `{error}` into `ApiError`, returns `undefined` on 204, and fires `window.dispatchEvent(new Event('softwarr:unauthorized'))` on 401.
  - `web/src/api/types.ts`: TypeScript mirrors of the Go JSON types: `User`, `Settings`, `Release`, `SearchResult`, `Request`, `RequestStatus`, `Activity`, `LibraryItem`, `Notification`, `Webhook`, `DownloadClient`, `PathMapping`, `HealthCheck`, `LogEntry`.
- Vite: `base: './'`, dev proxy `/api` → `http://localhost:7979`, build script `vite build && touch dist/.gitkeep`. Vitest with jsdom, `setup.ts` mocks `window.matchMedia` and `ResizeObserver` for Mantine.
- Theme: Mantine `defaultColorScheme="dark"`, the primary color a teal close to the *arr accent, compact default sizes (`xs`/`sm`) for tables and inputs.

- [ ] **Step 1: Write the failing tests**

```ts
test('adds CSRF header on POST, not GET')
test('throws ApiError with server message and status')
test('dispatches softwarr:unauthorized on 401')
test('returns undefined for 204')
```

```go
func TestSPAFallbackServesIndex(t *testing.T)      // GET /library/5 → 200 text/html containing window.__SOFTWARR__
func TestSPAInjectsURLBase(t *testing.T)           // URLBase "/sw" → `<base href="/sw/">`
func TestSPAAssetCacheHeaders(t *testing.T)
func TestAPIPathNotSwallowedBySPA(t *testing.T)    // GET /api/v1/nope → 404 JSON, not index.html
```

For the Go tests, give `httpapi.Deps` a `UI fs.FS` field (default `web.Dist` sub `dist`) so tests can pass an `fstest.MapFS`.

- [ ] **Step 2: Run** `cd web && npx vitest run` and `go test ./internal/httpapi/...` and expect failure.
- [ ] **Step 3: Implement.** `npm create vite@latest` (React + TS), then `npm i @mantine/core @mantine/hooks @mantine/notifications @mantine/form @tanstack/react-query react-router @tabler/icons-react` and dev deps `vitest jsdom @testing-library/react @testing-library/user-event @testing-library/jest-dom msw`. Commit `package-lock.json`.
- [ ] **Step 4: Run** both test suites, plus `make web && go build ./cmd/softwarr`, and expect PASS and a binary that serves the placeholder page.
- [ ] **Step 5: Commit** `feat: web scaffold, API client, embedded SPA`.

---

### Task 21: App shell, sign-in, setup, and notifications

**Files:**
- Create: `web/src/App.tsx`, `web/src/auth/AuthProvider.tsx`, `web/src/auth/useAuth.ts`, `web/src/pages/SetupPage.tsx`, `web/src/pages/LoginPage.tsx`, `web/src/pages/ChangePasswordPage.tsx`, `web/src/pages/ProfilePage.tsx`, `web/src/layout/Shell.tsx`, `web/src/layout/NotificationsBell.tsx`, `web/src/events/useEvents.ts`, tests beside each, `web/src/test/server.ts` (MSW handlers)

**Interfaces:**
- Consumes: `api`, the types, and the endpoints from Tasks 5, 8, 9, 10.
- Produces: `useAuth(): { user: User | null; loading: boolean; refresh(): Promise<void>; logout(): Promise<void> }`; `<RequireAdmin>` route guard; `useEvents()` opens one `EventSource(urlBase + '/api/v1/events')` and invalidates TanStack Query keys: `request.updated` → `['requests']`, `['activity']`; `notification` → `['notifications']`; `library.updated` → `['library']`, `['search']`; `activity` → `['activity']`; `health` → `['health']`. Query keys are those literal tuples, used by every later page.

Routing (React Router, `basename` = `urlBase`): `/setup`, `/login`, `/change-password`, then inside `Shell`: `/` (redirects to `/search`), `/search`, `/library`, `/library/:id`, `/requests` (My Requests), `/profile`, and admin-only `/admin/requests`, `/activity`, `/settings/*`, `/system`.

Behavior:
- On load: if `GET /setup` says needed → `/setup`. Else if `/auth/me` is 401 → `/login`. Else if `mustChangePassword` → `/change-password`.
- The login page always shows the username/password form. When `/auth/oidc/config` is enabled, it also shows a button with `buttonLabel` linking to `{urlBase}/api/v1/auth/oidc/login`. It shows `?error=` from the URL in an alert.
- The sidebar shows Search, Library, and My Requests to everyone. A divider, then Requests (with a pending count badge), Activity, Settings, and System appear for admins only.
- The bell shows the unread count and a dropdown of the latest notifications, with "Mark all as read".
- The profile page lets a local user change their password. "Connect SSO account" (POST `/auth/oidc/link`, then navigate to `redirect`) appears only when OIDC is enabled, the user is local, isn't the owner, and isn't linked. `?linked=1` shows a success alert.

- [ ] **Step 1: Write the failing tests** (Vitest + Testing Library + MSW)

```ts
test('redirects to setup when setup is needed')
test('login shows SSO button only when OIDC enabled')
test('login shows error from query string')
test('must-change-password user is sent to change password page')
test('non-admin sidebar hides admin links')
test('profile hides Connect SSO for owner')
test('notification event invalidates notifications query')   // mock EventSource
```

- [ ] **Steps 2 to 4:** Run `npx vitest run` (fail), implement, run (pass). Run all copy through the humanizer before committing.
- [ ] **Step 5: Commit** `feat(web): shell, auth pages, notifications`.

---

### Task 22: Search, request dialog, and My Requests

**Files:**
- Create: `web/src/pages/SearchPage.tsx`, `web/src/components/RequestDialog.tsx`, `web/src/components/StatusBadge.tsx`, `web/src/components/ReleaseTable.tsx`, `web/src/pages/MyRequestsPage.tsx`, tests, `web/src/format.ts` (`formatBytes`, `formatAge`) with tests

**Interfaces:**
- Consumes: `GET /search`, `POST /requests`, `GET /requests`, `POST /requests/{id}/cancel`, `GET /settings` is admin-only, so the comment requirement reaches users as `commentRequired` on `GET /auth/me` (add that field to the Task 5 handler in this task, with a Go test).
- Produces: `<ReleaseTable results onSelect>`, reused by the admin "approve with a different release" flow in Task 24. `<StatusBadge status>` covers both request statuses and search statuses.

Behavior:
- Search runs on submit, not as you type (Prowlarr searches are slow). Results show in a sortable dense table: title, indexer, protocol, size, age, seeders/leechers or grabs, and a status badge. Admins get an indexer multi-select (from `GET /indexers`).
- Badges: `available` → "Available" (links to `/library/{id}`), `requested` → "Requested", `no_client` → "No compatible download client", `requestable` → a "Request" button.
- The request dialog shows the title, size, and indexer, plus a textarea labeled "Why do you need this software?", marked required when `commentRequired`. On success, it shows a toast and the row's badge becomes "Requested". A 409 shows the server message.
- My Requests lists the user's requests: title (display title or release name), status badge, progress bar while downloading, deny reason, error, created date, and Cancel for pending.

- [ ] **Step 1: Write the failing tests**

```ts
test('formatBytes and formatAge')
test('search renders results with correct badges')
test('no_client results have no request button')
test('request dialog enforces required comment')
test('duplicate request shows server message')
test('my requests shows progress for downloading and cancel for pending')
```

- [ ] **Steps 2 to 4:** Run (fail), implement, run (pass). Humanize copy.
- [ ] **Step 5: Commit** `feat(web): search, request dialog, my requests`.

---

### Task 23: Library pages

**Files:**
- Create: `web/src/pages/LibraryPage.tsx`, `web/src/pages/LibraryItemPage.tsx`, tests

**Interfaces:**
- Consumes: `GET /library`, `GET /library/{id}`, `GET /library/{id}/download`, admin `PATCH`/`DELETE /library/{id}`.

Behavior:
- The list has a filter box and a sortable table: title, release name (muted, under the title), size, added, requested by, and a Download button (a plain `<a href download>` so the browser handles it).
- The item page shows the file list with sizes, requester, comment (when returned), approver, import mode, download count, and an expiry date ("Never expires" when null).
- Admins can edit the title, and set retention with a control offering "Use default", "Never", and "Custom days". Delete asks for confirmation ("Delete this item from the library? Seeding files in the download client aren't affected.").

- [ ] **Step 1: Write the failing tests**

```ts
test('library list filters by text')
test('download link points at download endpoint with url base')
test('retention control maps Use default → null, Never → 0, Custom → n')
test('non-admin sees no edit or delete controls')
```

- [ ] **Steps 2 to 4:** Run (fail), implement, run (pass). Humanize copy.
- [ ] **Step 5: Commit** `feat(web): library pages`.

---

### Task 24: Admin requests queue and Activity

**Files:**
- Create: `web/src/pages/admin/RequestsQueuePage.tsx`, `web/src/components/ApproveDialog.tsx`, `web/src/components/DenyDialog.tsx`, `web/src/pages/admin/ActivityPage.tsx`, tests

**Interfaces:**
- Consumes: `GET /requests?all=1`, `POST /requests/{id}/approve|deny|abandon|retry`, `GET /activity`, `GET /search`, `<ReleaseTable>`.

Behavior:
- The queue shows pending requests first, then everything else, filterable by status. Each pending row shows the release, size, indexer, requester, comment, and age, with Approve and Deny.
- The approve dialog has a "Display title" field (placeholder: the release name) and a "Use a different release" section that runs a search prefilled with the release title and lets the admin pick a row from `<ReleaseTable>`. It sends `{displayTitle, replacement}`.
- The deny dialog has an optional reason.
- Activity shows approved, downloading, importing, and failed requests: title, requester, status, progress bar, a "Stalled" badge, and errors. Failed rows get Retry and Abandon. Approved and downloading rows get Abandon, with a confirmation ("Stop tracking this download? The torrent stays in the download client.").
- A 409 from any action shows the server message and refetches.

- [ ] **Step 1: Write the failing tests**

```ts
test('approve sends display title')
test('approve with replacement sends chosen release')
test('deny sends reason')
test('conflict shows message and refetches')
test('activity shows stalled badge and retry for failed')
```

- [ ] **Steps 2 to 4:** Run (fail), implement, run (pass). Humanize copy.
- [ ] **Step 5: Commit** `feat(web): admin requests queue and activity`.

---

### Task 25: Settings, Users, and System pages

**Files:**
- Create: `web/src/pages/settings/SettingsLayout.tsx` and one file per section: `GeneralSettings.tsx`, `AuthSettings.tsx`, `IndexerSettings.tsx`, `DownloadClientSettings.tsx`, `PathMappingSettings.tsx`, `LibrarySettings.tsx`, `NotificationSettings.tsx`, `UsersSettings.tsx`; `web/src/pages/admin/SystemPage.tsx`; tests

**Interfaces:**
- Consumes: Tasks 7, 10, 11, 13, 14, and 19 endpoints.

Behavior (each section saves independently, and every connection form has a Test button showing the result inline):
- General: external URL, API key (shown, copy button, Regenerate with confirmation), "Require a reason with every request" toggle, default retention days (0 = never), stall threshold hours.
- Authentication: OIDC enabled, issuer, client ID, client secret, scopes, button label, auto-provision, link by verified email (with a short warning about what it allows), admin claim name and value. Shows the redirect URI to copy (`{externalUrl}/api/v1/auth/oidc/callback`). Enabling is blocked until an external URL is set.
- Indexers: Prowlarr URL, API key, and categories (multi-select of the standard PC categories with free entry for others).
- Download Clients: list with an Add modal driven by `GET /downloadclients/types`, a qBittorrent form, enable toggle, and priority.
- Remote Path Mappings: table plus a modal.
- Library: library root path.
- Notifications: webhook list and modal (name, URL, secret, event checkboxes, enabled, Test).
- Users: table with username, email, sign-in type (local or SSO), role, auto-approve, and status. Create a local user. Approve pending. Toggle admin and auto-approve. Disable. Delete. The owner's row has no destructive controls and shows an "Owner" badge.
- System: health checks (colored by level), status info, and a log table (level filter, auto refresh every 10s).

- [ ] **Step 1: Write the failing tests**

```ts
test('OIDC cannot be enabled without external URL')
test('redirect URI shown from external URL')
test('download client test shows error inline')
test('owner row has no destructive controls')
test('webhook form sends selected events')
test('system page renders health levels')
```

- [ ] **Steps 2 to 4:** Run (fail), implement, run (pass). Humanize copy.
- [ ] **Step 5: Commit** `feat(web): settings, users, system pages`.

---

### Task 26: Docker image, README, and end-to-end smoke test

**Files:**
- Create: `Dockerfile`, `.dockerignore`, `docker/entrypoint.sh`, `docker/softwarr-wrapper.sh`, `deploy/compose.example.yaml`, `scripts/smoke.sh`
- Modify: `README.md`, `Makefile` (`docker`, `smoke` targets)

**Interfaces:**
- Consumes: everything.

The image follows the linuxserver convention: it starts as root, then drops to the user given by `PUID`/`PGID` so softwarr's files match the user qBittorrent runs as. Under Linux's default `fs.protected_hardlinks=1`, hardlinks fail when softwarr can't write the files it links.

Dockerfile:
1. Stage `web`: `node:24-alpine`, `npm ci && npm run build` in `/src/web`.
2. Stage `build`: `golang:1.27-alpine`, copy `web/dist` from stage 1, `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/softwarr ./cmd/softwarr`.
3. Final: `alpine` (pin the current 3.x minor), `apk add --no-cache su-exec ca-certificates tzdata`. Binary at `/app/softwarr`, `docker/softwarr-wrapper.sh` installed as `/usr/local/bin/softwarr`, `docker/entrypoint.sh` at `/entrypoint.sh`. `ENV SOFTWARR_DATA_DIR=/config PUID=1000 PGID=1000 UMASK=022`, `EXPOSE 7979`, `VOLUME /config`, `ENTRYPOINT ["/entrypoint.sh"]`. No `USER` line.

`docker/entrypoint.sh` (`#!/bin/sh`, `set -eu`):
- If running as root: create group `softwarr` with `PGID` and user `softwarr` with `PUID` (reuse an existing group or user with that ID instead of failing), `chown softwarr:softwarr /config` and its contents when the top-level owner differs, `umask "$UMASK"`, then `exec su-exec "$PUID:$PGID" /app/softwarr "$@"`.
- If not root (the user set `user:` in compose): skip all of that, apply `umask "$UMASK"`, and `exec /app/softwarr "$@"`.
- Print one startup line: `softwarr running as uid=<uid> gid=<gid> umask=<umask>`.

`docker/softwarr-wrapper.sh` makes `docker exec softwarr softwarr reset-password` safe. `docker exec` runs as root by default, so a root-run command could create SQLite `-wal`/`-shm` files the app can't write. The wrapper does the same root check and runs `su-exec "$PUID:$PGID" /app/softwarr "$@"` under the same umask.

`deploy/compose.example.yaml` runs softwarr, qBittorrent, and Prowlarr, all with the same `PUID`, `PGID`, and `UMASK`. The qBittorrent downloads folder and the softwarr library sit under one host `/data` mount so hardlinks work, and comments explain both choices.

`scripts/smoke.sh` builds the image and runs it with a temp `/config` and `PUID=1234 PGID=1234`, then:
- waits for `/api/v1/ping`, POSTs `/setup`, logs in, and checks `GET /system/health` returns JSON
- checks `docker exec <c> stat -c '%u:%g' /config/softwarr.db` prints `1234:1234`
- checks `docker exec <c> ps -o user,args` (or `/proc/1/status`) shows the softwarr process isn't running as root
- runs `docker exec <c> softwarr reset-password`, checks the output contains "Temporary password:", and checks every `/config/softwarr.db*` file is still owned by `1234:1234`
- runs a second container with `--user 1234:1234` against a fresh temp dir owned by 1234 and checks `/api/v1/ping`, to cover the non-root path
- removes the containers. It exits non-zero on any failure.

README (through the humanizer): what softwarr is, the quick start with the compose example, environment variables (including `PUID`, `PGID`, `UMASK`), volumes and the same-filesystem and same-user notes for hardlinks, owner recovery (`docker exec softwarr softwarr reset-password`), OIDC setup (external URL, redirect URI, admin group), the license, and a note that *arr projects are reference only.

- [ ] **Step 1: Write the failing check:** add `make smoke` and run it. Expect failure (no Dockerfile).
- [ ] **Step 2: Implement** the Dockerfile, compose example, and README.
- [ ] **Step 3: Run** `make smoke` and expect `smoke test passed` with exit code 0. Check `docker image ls softwarr` reports an image size under 50 MB.
- [ ] **Step 4: Run** the full suite: `go test ./... && (cd web && npx vitest run)`, and expect PASS.
- [ ] **Step 5: Commit** `feat: docker image, compose example, README, smoke test`.
