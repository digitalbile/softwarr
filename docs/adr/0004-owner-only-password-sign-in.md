# 0004. "Disable local auth" leaves password sign-in to the owner

- Status: Accepted
- Date: 2026-11-25
- Links: #17

## Context

Admins who send everyone through SSO wanted a login page without a username and password form that most people can't use. They also wanted local passwords to stop working, so the identity provider decides who gets in. The original design says the owner is always local and the local login form always stays available, so the owner can get in when SSO is down. Turning local auth off completely would break that.

Three narrower versions were considered:

- Hiding the form without changing the server. Anyone could still sign in by posting to `/api/v1/auth/login`.
- Blocking password sign-in only for accounts linked to SSO. Local-only accounts could still sign in through the API.
- Keeping password sign-in for every admin. An admin removed from the provider's admin group would keep softwarr admin rights, because the group claim is only checked when someone signs in with SSO.

Turning local auth back on automatically when the OIDC settings stop working was also considered. A provider that starts after softwarr, which is common in Docker Compose, or a short outage would reopen password sign-in for everyone without an admin noticing, and anyone who could take the provider down could cause it.

## Decision

A "Disable local auth" setting, which can only be turned on while SSO is enabled. While SSO is usable (enabled, with an issuer and external URL, and setup finished) and the setting is on:

- Only the owner can sign in with a password. Every other account gets 403, admins included. The check runs after the password, so a wrong password still gets 401.
- Sessions of accounts that aren't the owner and have no SSO identity end on their next request.
- The login page shows only the SSO button. `/login?local=1` shows the form again, for the owner.

If SSO isn't usable, the setting has no effect. A failing OIDC health check notifies admins and never changes how sign-in works. `auth.OIDC.LoginOptions` decides what the login page offers and whether password sign-in is limited, so the two can't disagree.

## Consequences

- The owner can always sign in, at `/login?local=1`, and `softwarr reset-password` still works.
- Everyone else depends on the identity provider. During an outage they wait for it to recover or for the owner to turn the setting off.
- Local users who aren't connected to SSO lose access when the setting is turned on. To bring them back, an admin turns on "link by verified email" or deletes their accounts so SSO can create new ones.
- Admins can still create local users while the setting is on, and those users can't sign in (see `docs/follow-ups.md`).
- This rules out removing local login entirely, and any path that blocks the owner's password sign-in.
