# OIDC local auth switch and settings check: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Disable local auth" setting that hides the login form (shown again with `?local=1`), turns password sign-in off for everyone but the owner, and ends the sessions of local accounts that aren't linked to SSO. Also add a "Test" button that checks the OIDC settings.

**Architecture:** The setting is a new field on `settings.OIDC`. The `auth.OIDC` service decides whether it's in effect, using the same rule as the SSO button. It also decides whether a user may sign in with a password, and whether a user's session is still allowed. It runs the settings check too. Handlers and middleware only call those methods. The UI reads `localAuthDisabled` from `/auth/oidc/config` to decide whether to show the form.

**Tech Stack:** Go (`coreos/go-oidc/v3`, `oauth2-proxy/mockoidc` in tests), React with Mantine, Vitest with MSW.

**Spec:** `docs/superpowers/specs/2026-11-25-oidc-local-auth-and-check-design.md`

## Global constraints

- Setting JSON name: `disableLocalAuth`. UI label: "Disable local auth".
- Query parameter that shows the form again: `local=1` (`/login?local=1`).
- Error for a blocked password login: `Password sign-in is turned off. Sign in with SSO.` (HTTP 403).
- While the setting is in effect, only the owner may sign in with a password. Other admins are included in the block.
- While the setting is in effect, sessions of accounts that aren't the owner and have no OIDC identity end. Linked accounts keep their sessions.
- "In effect" means OIDC is enabled, the issuer is set, the external URL is set, setup is finished, and `disableLocalAuth` is on. If any of those is missing, the form shows and every password login works.
- The test endpoint is `POST /api/v1/settings/oidc/test`, wrapped in `requireAdmin`. It returns `{ok, error}` with status 200 and never handles the client secret.
- User-facing copy goes through `humanizer:humanizer` before it's committed (AGENTS.md, "Writing").
- Change `web/src/api/types.ts` in the same commit as the Go field (`apitypes_test.go`).

## Review focus

1. OIDC is set to "disable local auth," then the issuer is cleared or SSO breaks. The login form has to come back, password sign-in has to work for everyone, and sessions must stop being ended. Task 2 tests this by clearing the issuer.
2. Someone posts directly to `/auth/login` without the form. Only the owner gets in. A non-owner admin gets 403. Task 2 tests this at the API level.
3. A wrong password while the setting is on. The response has to be 401, not 403, so the 403 doesn't confirm the account exists. Task 2 tests this.
4. Turning SSO off in the settings form while "Disable local auth" is on. The save has to succeed, with the switch cleared too. Task 4 tests this.
5. An issuer typed with a trailing slash, when the provider's discovery document says otherwise. The check has to report the mismatch the way sign-in would hit it, without "fixing" the URL. Task 3 tests this with the mismatch case.

---

### Task 1: the `disableLocalAuth` setting

**Files:**
- Modify: `internal/settings/settings.go` (the `OIDC` struct and `validate`)
- Modify: `web/src/api/types.ts` (`OIDCSettings`)
- Modify: `web/src/test/server.ts` (`makeSettings` default)
- Test: `internal/settings/settings_test.go`

**Interfaces:**
- Produces: `settings.OIDC.DisableLocalAuth bool` (JSON `disableLocalAuth`), and `disableLocalAuth: boolean` on the `OIDCSettings` TypeScript type.

- [ ] **Step 1: Write the failing test** `TestDisableLocalAuthNeedsOIDC`. Saving with `OIDC.Enabled=false` and `DisableLocalAuth=true` returns an input error (`settings.IsInputError`) with the message `Enable OIDC before disabling local auth.` Saving with `Enabled=true`, `ExternalURL` set and `DisableLocalAuth=true` works, and `Get` returns `DisableLocalAuth == true`. The defaults have it false.
- [ ] **Step 2: Run it and confirm it fails to compile.** Run `go test ./internal/settings -run TestDisableLocalAuthNeedsOIDC`. It should fail because `DisableLocalAuth` doesn't exist yet.
- [ ] **Step 3: Implement it.** Add the field after `Enabled`. In `validate`, right after the external URL check, reject `DisableLocalAuth && !Enabled` with that message.
- [ ] **Step 4: Update the UI types and the test fixture.** Add `disableLocalAuth: boolean` to `OIDCSettings` in `types.ts`. Add `disableLocalAuth: false` to the `oidc` block of `makeSettings` in `web/src/test/server.ts`.
- [ ] **Step 5: Run the tests.** Run `go test ./internal/settings ./internal/httpapi -run 'DisableLocalAuth|APITypes'` and `cd web && npx tsc --noEmit`. Both should pass.
- [ ] **Step 6: Commit** with `feat(settings): add the disable local auth setting`.

### Task 2: enforce it on the server

**Files:**
- Modify: `internal/auth/oidc.go` (new methods and error)
- Modify: `internal/httpapi/oidc_handlers.go` (`handleOIDCConfig`)
- Modify: `internal/httpapi/auth_handlers.go` (`handleLogin`)
- Modify: `internal/httpapi/middleware.go` (`authenticate`)
- Test: `internal/httpapi/oidc_handlers_test.go`

**Interfaces:**
- Consumes: `settings.OIDC.DisableLocalAuth` (Task 1).
- Produces:
  - `var auth.ErrPasswordSignInOff = errors.New("Password sign-in is turned off. Sign in with SSO.")`
  - `func localAuthOff(st settings.Settings) bool` (unexported) returns `OIDC.Enabled && OIDC.Issuer != "" && ExternalURL != "" && OIDC.DisableLocalAuth`.
  - `func (o *OIDC) LocalAuthDisabled(ctx context.Context) (bool, error)` returns true when `o.config(ctx)` succeeds and `DisableLocalAuth` is set. `o.config` adds the setup-finished check. It treats `ErrOIDCDisabled` and `ErrSetupNeeded` as false, not as errors.
  - `func (o *OIDC) CheckPasswordLogin(ctx context.Context, u User) error` returns `ErrPasswordSignInOff` when `LocalAuthDisabled` is true and `u.IsOwner` is false. Otherwise it returns nil.
  - `func (o *OIDC) SessionAllowed(ctx context.Context, u User) (bool, error)` returns false when `localAuthOff(settings)` is true, `u.IsOwner` is false, and `u.HasOIDC` is false. It reads only the cached settings and runs no query.
  - `GET /api/v1/auth/oidc/config` adds a `localAuthDisabled` bool to its response.

- [ ] **Step 1: Write the failing login test** `TestDisableLocalAuthPasswordSignIn` in `oidc_handlers_test.go`. Set it up with `ensureOwner`, `newMockOIDC`, and `enableOIDC(m, "", func(o *settings.OIDC) { o.DisableLocalAuth = true })`. Create a non-owner admin with `CreateLocal("admin2", "", testPassword, true)` and a regular user through `loginAsUser`'s setup. Then assert:
  - `GET /auth/oidc/config` returns `localAuthDisabled == true`.
  - `owner` signs in with 200.
  - `admin2` and `user` each get 403. The error body is `auth.ErrPasswordSignInOff.Error()`, and no session cookie is set.
  - `user` with a wrong password gets 401.
  - After saving the settings with `Issuer = ""`, `localAuthDisabled` is false and `admin2` signs in with 200.
- [ ] **Step 2: Write the failing session test** `TestDisableLocalAuthEndsLocalSessions`. Before turning the setting on:
  - Sign in client `cu` as the local `user`.
  - Create `linked` with `CreateLocal("linked", "", testPassword, false)`, sign client `cl` in with its password, then link it with `ts.Q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: id, Issuer: m.Issuer(), Subject: "sub-linked", CreatedAt: 1})`.
  - Sign client `co` in as the owner.

  Then save `DisableLocalAuth = true` and assert:
  - `GET /auth/me` returns 401 for `cu`.
  - It still returns 401 for `cu` after the setting is turned off again, because the session was deleted, not just refused.
  - It returns 200 for `cl` and `co`.
- [ ] **Step 3: Add the default case.** Extend `TestOIDCDisabled` to assert `localAuthDisabled == false` while OIDC is off.
- [ ] **Step 4: Run them and confirm they fail.** Run `go test ./internal/httpapi -run 'TestDisableLocalAuth|TestOIDCDisabled'`.
- [ ] **Step 5: Implement it.**
  - Add `ErrPasswordSignInOff`, `localAuthOff` and the three methods to `internal/auth/oidc.go`. Add `ErrPasswordSignInOff` to the list in `OIDCErrorMessage`.
  - In `handleOIDCConfig`, set `localAuthDisabled` from `s.OIDC.LocalAuthDisabled`.
  - In `handleLogin`, after `Authenticate` succeeds and before `startSession`, call `s.OIDC.CheckPasswordLogin`. Return `ErrPasswordSignInOff` as a 403 with its message, and send any other error to `serverError`.
  - In `authenticate`, right after the `user.Status != "active"` branch, call `s.OIDC.SessionAllowed`. If it returns false, delete the session and continue unauthenticated, the same way that branch does. Send an error to `serverError`.
- [ ] **Step 6: Run the tests.** Run `go test ./internal/auth ./internal/httpapi`. Everything should pass.
- [ ] **Step 7: Commit** with `feat(auth): limit password sign-in to the owner when local auth is off`.

### Task 3: the settings check

**Files:**
- Create: `internal/auth/oidc_check.go`
- Create: `internal/auth/oidc_check_test.go`
- Modify: `internal/httpapi/oidc_handlers.go` (route and handler)
- Test: `internal/httpapi/oidc_handlers_test.go`

**Interfaces:**
- Produces:
  - `func (o *OIDC) Check(ctx context.Context, externalURL, issuer, clientID string) error`. It trims spaces from all three. It returns an error whose message is meant for an admin, and nil when the settings look usable.
  - `POST /api/v1/settings/oidc/test` takes the body `{"externalUrl", "issuer", "clientId"}` and returns `{"ok": bool, "error": string}` with status 200. A body that doesn't decode gets a 400.

Checks, each returning its message the first time it fails (run the copy through the humanizer):

1. The external URL is empty: `Set the external URL in General settings first.`
2. The issuer is empty: `Enter the issuer URL.`
3. The client ID is empty: `Enter the client ID.`
4. `oidc.NewProvider(oidc.ClientContext(ctx, o.client), issuer)` fails: `Couldn't load the provider's settings from <issuer>: <err>`. This covers an unreachable issuer, a bad document, and an issuer mismatch, because go-oidc compares the issuer itself.
5. `p.Endpoint().AuthURL` or `TokenURL` is empty: `The provider doesn't list an authorization or token endpoint.`
6. `p.Claims` into `struct{ ResponseTypes []string \`json:"response_types_supported"\` }`. If the list isn't empty and doesn't contain `code`: `The provider doesn't support the authorization code flow.`

`Check` doesn't touch `o.provider` or `o.issuer`, and it doesn't read settings, so `NewOIDC(nil, nil, nil, nil)` works in tests.

- [ ] **Step 1: Write the failing table test** `TestOIDCCheck` in `oidc_check_test.go`. The cases are:
  - A `mockoidc.Run()` provider with an external URL and client ID returns nil.
  - Each empty field returns its message.
  - An `httptest` server whose `/.well-known/openid-configuration` reports a different `issuer` returns an error containing `Couldn't load the provider's settings` and the issuer URL.
  - The mock's issuer with a `/` added returns the same kind of error. This is review focus 5.
  - A closed `httptest` server's URL returns an error containing `Couldn't load`.
  - A discovery document with endpoints and `response_types_supported: ["id_token"]` returns the authorization code message.
  - A document without the `response_types_supported` field returns nil.
- [ ] **Step 2: Write the failing handler test** `TestOIDCSettingsTest`. A regular user gets 403. An admin gets `{ok: true}` against `newMockOIDC`, and `{ok: false, error: "Enter the client ID."}` without a client ID.
- [ ] **Step 3: Run them and confirm they fail.** Run `go test ./internal/auth ./internal/httpapi -run 'TestOIDCCheck|TestOIDCSettingsTest'`.
- [ ] **Step 4: Implement `Check`, the route and the handler.** Register the route in `registerOIDCRoutes` with `requireAdmin`. The handler follows `handleProwlarrTest` in `search_handlers.go`.
- [ ] **Step 5: Run the tests.** Run `go test ./internal/auth ./internal/httpapi`. Everything should pass.
- [ ] **Step 6: Commit** with `feat(auth): add a check for the OIDC settings`.

### Task 4: the settings page

**Files:**
- Modify: `web/src/pages/settings/AuthSettings.tsx`
- Test: `web/src/pages/settings/AuthSettings.test.tsx`

**Interfaces:**
- Consumes: `OIDCSettings.disableLocalAuth` (Task 1) and `POST /settings/oidc/test` (Task 3).

- [ ] **Step 1: Write the failing tests.**
  - `disable local auth needs SSO`: with SSO off, the `Disable local auth` switch is disabled.
  - `turning SSO off clears disable local auth`: start with `oidc: {enabled: true, disableLocalAuth: true}` and an external URL. Turn off `Enable single sign-on`, save, and assert that the PUT body has `oidc.disableLocalAuth === false`. This is review focus 4.
  - `test button checks the form values`: type an issuer, click `Test`, and assert that the POST body is `{externalUrl, issuer, clientId}` with no `clientSecret` key. Reply `{ok: false, error: 'Enter the client ID.'}` and assert that the message shows in `role="status"`.
- [ ] **Step 2: Run them and confirm they fail.** Run `cd web && npx vitest run src/pages/settings/AuthSettings`.
- [ ] **Step 3: Implement it.**
  - Add a `Switch` labeled `Disable local auth` right after the SSO switch, with `disabled={!o.enabled}`. Its `description` is the humanized form of: "Only the owner can sign in with a password, at /login?local=1. Everyone else signs in with SSO. Local accounts that aren't connected to SSO are signed out."
  - The SSO switch's `onChange` also sets `disableLocalAuth` to false when it turns SSO off.
  - Pass `actions={<TestButton okText="The provider's settings look right." run={...} />}` to `SectionForm`. It posts `{ externalUrl: settings.externalUrl, issuer: o.issuer, clientId: o.clientId }`.
  - Change the section description from "Local accounts keep working." to something true either way, such as "Let people sign in with an OpenID Connect provider."
- [ ] **Step 4: Run the tests.** Run `cd web && npx vitest run src/pages/settings/AuthSettings`. They should pass.
- [ ] **Step 5: Commit** with `feat(web): add the disable local auth switch and the OIDC test button`.

### Task 5: the login page

**Files:**
- Modify: `web/src/pages/LoginPage.tsx`
- Modify: `web/src/test/server.ts` (the default `oidc/config` reply adds `localAuthDisabled: false`)
- Test: `web/src/App.test.tsx`

**Interfaces:**
- Consumes: `localAuthDisabled` from `GET /auth/oidc/config` (Task 2).
- Produces: `OIDCConfig.localAuthDisabled: boolean` in `LoginPage.tsx`.

- [ ] **Step 1: Write the failing tests** in `App.test.tsx`.
  - `login hides the form when local auth is disabled`: the config is `{enabled: true, buttonLabel: 'SSO', localAuthDisabled: true}`. The `SSO` link shows, and there's no `Username` field and no `Sign in` button.
  - `login?local=1 shows the form again`: with the same config at `/login?local=1`, both the SSO link and the `Username` field show.
  - `login shows error when local auth is disabled`: at `/login?error=x` with that config, the message `x` shows.
- [ ] **Step 2: Run them and confirm they fail.** Run `cd web && npx vitest run src/App.test.tsx`.
- [ ] **Step 3: Implement it.**
  - Add `localAuthDisabled` to `OIDCConfig`.
  - Show `FullPageLoader` while `oidc.isPending`, so the form doesn't flash and then disappear. If the config request fails, show the form.
  - Hide the form and its submit button when `oidc.data?.localAuthDisabled && params.get('local') !== '1'`. The title, the error alert and the SSO button stay either way.
- [ ] **Step 4: Run all the UI checks.** Run `cd web && npm test && npm run lint`. Everything should pass.
- [ ] **Step 5: Commit** with `feat(web): hide the local login form when local auth is disabled`.

### Task 6: docs (after PR #14 merges)

PR #14 adds `docs/adr/` and AGENTS.md. Rebase this branch onto `main` once it has merged.

**Files:**
- Create: `docs/adr/0004-hidden-local-login-form.md`
- Modify: `docs/adr/README.md` (the index)
- Modify: `AGENTS.md` ("Keep the owner able to sign in")
- Modify: `docs/superpowers/specs/2026-11-23-softwarr-design.md` (the owner paragraph under "Roles and accounts," and the OIDC settings list)

- [ ] **Step 1: Write the ADR** in the format of the existing ADRs. It records that "Disable local auth" leaves password sign-in to the owner alone, at `/login?local=1`. Other accounts, admins included, sign in with SSO. Local accounts without SSO lose their sessions. A broken OIDC setup turns the switch off in practice. It also records the rejected alternatives and why:
  - Removing local login completely: it locks out the owner.
  - Keeping password sign-in for all admins: an admin removed from the provider's group keeps admin rights.
  - Blocking only linked accounts: local users could still sign in through the API.
- [ ] **Step 2: Update AGENTS.md, the design spec and the follow-ups.** The rule becomes: the local login form stays reachable at `/login?local=1` when local auth is disabled, and the owner's password login always works. Add the follow-up from the spec's Docs section to `docs/follow-ups.md`: admins can still create local users who can't sign in while the setting is on.
- [ ] **Step 3: Humanize and run the full checks.** Run the new prose through `humanizer:humanizer`. Then run `make lint && make test && (cd web && npm test)`. Everything should pass.
- [ ] **Step 4: Commit** with `docs: record how disabling local auth keeps the owner able to sign in`.
- [ ] **Step 5: Open the PR.** Its description says the wiki's authentication page needs a section on "Disable local auth," `?local=1`, and the Test button.
