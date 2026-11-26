# OIDC: disable local auth, and check the settings

## Why

Admins who run everyone through SSO want the login page to show only the SSO button. A username and password form that most people can't use just confuses them. Admins also want to know whether their OIDC settings work before relying on them, especially before they hide the password form.

## Goals

- A "Disable local auth" setting that hides the local login form and turns off password sign-in for everyone except the owner.
- A "Test" button on the authentication settings page that checks the OIDC settings in the form, without saving them.

## Non-goals

- A test sign-in that goes through the provider and reports the claims that come back.
- Removing local login from the server. The owner still signs in locally.

## Anti-goals

- Any way for the owner to be locked out. The owner account stays local, and its password login always works.
- Hiding the form when OIDC can't actually be used (it's off, or the issuer or external URL is missing). People would then see a login page with nothing to sign in with.

## Approaches

For the test:

1. Settings check: discovery, issuer match, and that the required fields are filled in. This is fast and works on unsaved values. It can't catch a wrong client secret or a redirect URI the provider hasn't registered.
2. Settings check plus a test sign-in through the provider, which would show the claims and what softwarr would do with them. This catches more mistakes, but it needs a migration and a separate branch in the callback.

The user picked 1.

For local auth, the user chose to hide the form behind a query parameter, and to turn password sign-in off for everyone but the owner, other local admins included. Turning local auth off completely was ruled out because it would lock out the owner (design spec, anti-goals; AGENTS.md, "Keep the owner able to sign in"). Two narrower versions were also considered and dropped:

- Blocking only linked accounts. Local-only users could still sign in by posting to the API.
- Keeping password sign-in for all admins. An admin removed from the provider's admin group would keep softwarr admin rights, because the group claim is only checked at SSO sign-in.

## Design

### Setting

- Add `DisableLocalAuth bool` (`disableLocalAuth`) to `settings.OIDC`, defaulting to false.
- Validation rejects `disableLocalAuth` when `enabled` is false, so the stored value always makes sense.
- "In effect" means OIDC is usable, by the same rule `handleOIDCConfig` uses (enabled, issuer set, external URL set, setup finished), and `disableLocalAuth` is on. Every check below uses this, so a broken OIDC setup never hides the form.

### Settings page (`AuthSettings.tsx`)

- Add a "Disable local auth" switch below "Enable single sign-on", disabled while SSO is off. Turning SSO off in the form turns this switch off too.
- Subtext (to be humanized): "Only the owner can sign in with a password, at /login?local=1. Everyone else signs in with SSO. Local accounts that aren't connected to SSO are signed out."

### Login page

- `GET /api/v1/auth/oidc/config` adds `localAuthDisabled`, which is true when the setting is in effect.
- When it's true, `LoginPage` shows only the SSO button and hides the form. With `?local=1` in the URL, the form shows as usual, along with the SSO button.
- Update `OIDCConfig` in `LoginPage.tsx`, and `web/src/api/types.ts` and the `apiTypes` map if the type lives there.

### Password login

- While the setting is in effect, `POST /api/v1/auth/login` refuses every account except the owner, other admins included. It returns 403 with "Password sign-in is turned off. Sign in with SSO." The check happens after the password is verified, so a wrong password still gets 401 and the 403 doesn't confirm that an account exists.
- The check belongs in the `auth` package, so the handler doesn't read settings to make an auth decision. The builder chooses its exact form.

### Sessions

- Sessions extend themselves while they're in use, so a local user who is already signed in would otherwise never be cut off. While the setting is in effect, the session middleware ends the session of any account that isn't the owner and has no OIDC identity, then treats the request as signed out. This works the same way as for a disabled account.
- Linked accounts keep their sessions, since they could sign in again with SSO anyway.
- The check uses the settings and the user the middleware has already loaded, so it adds no query. It skips the setup-finished part of "in effect," because a session can't exist before setup.
- Local users who are cut off can get back in through SSO. An admin can turn on "link by verified email" so their first SSO sign-in lands on their existing account, or delete the account and let SSO create a new one.

### Settings check

- `POST /api/v1/settings/oidc/test`, wrapped in `requireAdmin`. Its body is the unsaved OIDC settings plus the external URL from the form. It returns `{ok, error}`, the same shape as the Prowlarr test, and reports the first problem it finds.
- Checks, in order:
  1. The external URL is set.
  2. The issuer and client ID are set.
  3. Discovery works and the issuer matches. `oidc.NewProvider` does both.
  4. The provider lists authorization and token endpoints.
  5. If the provider publishes `response_types_supported`, it includes `code`.
- Missing PKCE or scopes from the discovery document don't count as failures. Many providers leave them out, and the custom scope `groups` often isn't listed.
- The check is a method on `auth.OIDC` and uses its timed `http.Client`. It doesn't use or replace the cached provider.
- Error text is for an admin, so it can include the issuer URL, but never the client secret.
- The settings page gets a "Test" button that shows the result inline, like the Prowlarr settings page.

### Docs

- Add an ADR explaining how this fits the "local form stays available" rule: the form is still there, behind `?local=1`, and the owner can always use it.
- Add to `docs/follow-ups.md`: while the setting is on, an admin can still create local users who can't sign in. The users page could warn about it.
- Update the AGENTS.md safety rule to mention `?local=1`.
- The PR names the wiki page to update (authentication / OIDC).

## Left to the builder

- Where the login check lives in `auth`, and the names of the new functions.
- Test layout, following the existing `httptest` fakes for the OIDC provider.
