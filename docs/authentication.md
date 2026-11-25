# Authentication

softwarr has local accounts and can add one OIDC provider. The first-run wizard creates the owner account, which is always local.

## OIDC sign-in

The local login form stays available alongside OIDC, in case your provider is down.

1. In Settings, General, set the External URL to the public address of softwarr, for example `https://softwarr.example.com`. If you set `SOFTWARR_URL_BASE`, include it, for example `https://example.com/softwarr`. OIDC can't be enabled without it, and webhook links use it too.
2. In your identity provider, create an OIDC client and set the redirect URI to `<external URL>/api/v1/auth/oidc/callback`.
3. In Settings, Authentication, enter the issuer URL, client ID, and client secret, then turn on single sign-on.
4. Optional: to give admin rights through a group, set the claim name (for example `groups`) and the admin value (for example `softwarr-admins`). Set both or neither. OIDC users gain or lose admin status from that group each time they sign in. Local-only users aren't affected.

## Recovering the owner account

If the owner loses their password, run this on the Docker host:

```
docker exec softwarr softwarr reset-password
```

It prints a temporary password, ends the owner's sessions, and asks for a new password at the next sign-in. There is no other recovery path. Anyone who can run this command already controls the host.
