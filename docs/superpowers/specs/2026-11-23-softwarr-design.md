# softwarr design

Date: 2026-11-23
Status: Draft for review

## Intent

softwarr is a self-hosted app in the style of Sonarr and Radarr for requesting and downloading PC and macOS software from Torznab and Newznab sources. Sonarr and Radarr monitor a metadata catalog for new releases. No comparable catalog exists for software, so softwarr starts from search instead. A user searches the indexers, requests a specific release, and an admin approves it. softwarr then sends the release to a download client, imports the finished files into a shared library, and any user can download them from the web UI.

The *arr apps are effectively admin-only. softwarr has two audiences: regular users, who search, request, and download from the library, and admins, who approve requests and configure everything.

## Goals

- Search Prowlarr-managed indexers (Torznab and Newznab) from a web UI.
- A request and approval workflow, where the requester explains why they need the software.
- Send approved releases straight to a download client and track their progress, starting with qBittorrent.
- Import completed downloads into a library with hardlinks, so the library can be managed separately from the files being seeded.
- A shared library that every user can browse and download from, with retention set by admins.
- Local auth and OIDC, with a permanent local owner account that can't be locked out.
- One small Docker image, with as little configuration outside the UI as possible.

## Non-goals

- Monitoring, RSS auto-grabbing, or "wanted" lists. Search is manual.
- A software metadata catalog with icons, descriptions, or canonical names. Items keep their release name unless an admin sets a title.
- Managing seeding. softwarr never pauses, removes, or changes a torrent after adding it. Seeding rules belong to the download client.
- Request quotas.
- Following or joining another user's request. If a release is already requested, other users see that and wait for it to appear in the library (Seerr works the same way).
- Notification channels beyond in-app and a generic webhook. No email or Discord for now.
- Configuring indexers inside softwarr. Indexers are managed in Prowlarr.

## Anti-goals

Any of these would make the project a failure even if everything else works:

- Regular users seeing or reaching indexer, download client, or system settings.
- Any way for the owner (the first local admin) to be locked out, demoted, deleted, or converted to OIDC.
- Account takeover through OIDC email matching.
- softwarr deleting or modifying files the download client is seeding.
- GPL code from Sonarr, Radarr, or Prowlarr in this MIT-licensed repo. Their source can be read to learn behavior, such as API call order and edge cases. The implementations here are written from scratch.

## Constraints

- The license is MIT, so no GPL-derived code.
- The download client and softwarr usually run in separate containers and see different paths, so remote path mappings are required.
- Hardlinks only work within one filesystem. A copy fallback is required, and the admin has to be told when it's in use.
- Indexer download URLs can go stale between request and approval.

## Approaches considered

### Stack

| Option | Trade-off |
|---|---|
| Go backend + React SPA (chosen) | One static binary with the UI embedded, an image around 20 to 30 MB, and a small idle footprint. The auth rules are custom either way, and Go keeps them explicit. Sessions and migrations take more hand-written code. |
| C#/.NET + React (the *arr stack) | Cookie auth, OIDC, and roles come built in, and *arr contributors know the layout. Heavier image and more boilerplate. |
| TypeScript full stack | Fastest path to an MVP, in one language. Auth.js works against the rule that the local owner can never link to OIDC, and it needs a Node runtime. |

The user chose Go + React.

### Getting releases to a download client

| Option | Trade-off |
|---|---|
| Prowlarr's grab API, which pushes to Prowlarr's own clients | Least code. softwarr can't reliably see progress or completion, and import and the library depend on both. |
| Direct download client integration (chosen) | More code per client, with full control over the category, progress tracking, and the hand-off to import. |

The user chose direct integration: qBittorrent first, then other widely used clients.

### UI components

| Option | Trade-off |
|---|---|
| Mantine (chosen) | Dense tables, forms, modals, toasts, and a dark theme out of the box. Needs some theme overrides to look like an *arr app. |
| Tailwind + shadcn/ui | Full control over styling, at the cost of building more of the tables and settings layouts. |
| Hand-written CSS modules like Sonarr's | Closest to the *arr look, with a lot of component work that adds nothing to the product. |

The user chose Mantine.

## Architecture

A single Go binary serves:

- a REST JSON API under `/api/v1`
- the React SPA, built with Vite and embedded with `embed.FS`, for every other route
- a Server-Sent Events endpoint for live progress and notifications
- in-process background workers

Backend:

- Routing uses the Go standard library's `net/http` method and pattern matching.
- SQLite through `modernc.org/sqlite`. It's pure Go, so there's no cgo, and the binary is static and cross-compiles easily.
- `sqlc` for queries. `goose` for migrations, which run automatically at startup.
- Server-side sessions stored in SQLite and referenced by an HttpOnly, SameSite=Lax cookie, marked Secure when served over HTTPS.
- One admin-level API key, as in the *arrs, sent as `X-Api-Key`, for scripts and automation.

Frontend: React, Vite, TypeScript, Mantine, TanStack Query for server state, and React Router.

Deployment is one Docker image. Port, data directory, and URL base are environment variables and need a restart to change. Everything else is set in the UI and stored in SQLite. The container mounts `/config` for the database, the download client's completed-downloads path, and the library path.

### Suggested packages

- `auth`: local passwords, sessions, OIDC, setup and owner rules, password reset command
- `prowlarr`: search client
- `downloadclient`: the `Client` interface, the `qbittorrent` implementation, and a registry for later clients
- `requests`: request lifecycle and state machine
- `importer`: path mapping and hardlink, copy, or move into the library
- `library`: items, delivery (direct file or streamed zip), retention
- `notify`: in-app notifications and webhooks
- `events`: SSE fan-out
- `httpapi`: handlers and middleware for authentication, role checks, and CSRF

## Roles and accounts

The owner is the first account, created by a first-run setup wizard. It's always local and always an admin, and it can't be deleted, disabled, demoted, or linked to OIDC. The local login form always stays available so the owner can get in when OIDC is down.

Admins approve requests and manage settings and users. Their own requests are approved automatically.

Users search, request, download from the library, and see their own requests. An admin can give a user the auto-approve permission.

Users only see their own requests. Admins see all of them.

### Local auth

- Passwords are hashed with argon2id.
- Admins create local users. There's no self-registration for local accounts.

### Owner password recovery

Running `softwarr reset-password` inside the container (`docker exec softwarr softwarr reset-password`) sets a random one-time password on the owner account, prints it, and requires a new password at the next login. It also ends the owner's existing sessions. Anyone who can run that command already controls the host, so it opens no new way in. There is no environment variable or web-based recovery path.

### OIDC

- Authorization code flow with PKCE, using `coreos/go-oidc` and `golang.org/x/oauth2`.
- Phase 1 supports one provider: one OIDC configuration and one login button. Identities are still keyed on issuer plus `sub`, so more providers can be added later without a data migration.
- Settings: issuer URL, client ID and secret, scopes, and the label for the login button.
- External URL (Settings > General) is the public base URL of softwarr, for example `https://softwarr.example.com`, and must be set before OIDC can be enabled. The redirect URI (`{externalURL}/api/v1/auth/oidc/callback`) and the links in webhook payloads are built from it. Neither is ever derived from the request's Host or X-Forwarded-* headers.
- An identity is the pair (issuer, `sub`), stored in a separate `user_identities` table. Email is never used as the identity key.
- Auto-provisioning is on by default. The first login from an unknown (issuer, sub) creates a new user account.
  - If the email from the identity provider already belongs to an existing account, the login is rejected with a message saying so. Nothing is linked automatically.
  - With auto-provisioning off, an unknown identity creates a pending account, and an admin has to approve it before it can sign in.
- A local account can be linked to OIDC only from inside a signed-in session, using "Connect OIDC account" on the profile page. That proves the person controls both. The owner doesn't get this option.
- An optional "Link by verified email" setting, off by default, links a login to an existing non-owner account with the same email. It applies only when the identity provider asserts `email_verified=true`.
- Admin rights can optionally come from a group. An admin sets a claim name (for example `groups`) and a value (for example `softwarr-admins`). OIDC-linked users then gain or lose admin status from that group on every login. Local-only users aren't affected. When this is unset, admin status is managed in softwarr.

### Request security

- State-changing requests need the session cookie plus either a custom header (such as `X-Requested-With`) or a CSRF token. SameSite=Lax provides a baseline. The builder picks the mechanism.
- Every admin endpoint checks the role on the server. Hiding things in the UI doesn't count as access control.

## Indexers (Prowlarr)

- Settings: Prowlarr URL, API key, a Test button, and which categories to search.
- Search calls Prowlarr's search API (`/api/v1/search`) with the query and categories. Results include both Torznab (`protocol: torrent`) and Newznab (`protocol: usenet`) releases.
- Default categories are PC (4000), PC/0day (4010), PC/ISO (4020), and PC/Mac (4030). Admins can change them. The builder should check these IDs against the Newznab standard categories that Prowlarr exposes.
- Admins can also narrow a search to specific Prowlarr indexers.
- Each result shows the release title, indexer, protocol, size, age, seeders and leechers for torrents or grabs for Usenet, and category.
- A result that matches a library item shows an Available badge linking to it. A result that matches an open request shows a Requested badge, for every user. Matching uses the infohash for torrents, then the Prowlarr GUID, then the exact release title.
- When no enabled download client supports a result's protocol, as with Usenet before SABnzbd or NZBGet support exists, the result still appears but is marked "No compatible download client" and can't be requested.

## Requests

### Lifecycle

```
pending ──approve──▶ approved ──grab ok──▶ downloading ──complete──▶ importing ──▶ [scanning]* ──▶ available
   │                    │                       │                       │
   ├─deny──▶ denied     └─grab failed──▶ failed  └─client error──▶ failed └─import error──▶ failed
   └─cancel (by requester)──▶ cancelled

approved / downloading / failed ──abandon (admin)──▶ abandoned

* scanning is reserved for the malware-scanning phase; until then importing → available directly.
```

### Creating a request

- The user picks a search result and submits it with an answer to "Why do you need this software?"
- An admin setting makes that answer optional or required.
- softwarr saves a full snapshot of the release (title, indexer, protocol, size, GUID, download URL or magnet, infohash if known, categories), because a request can wait days for approval.
- Requests from admins and from users with auto-approve skip `pending`.
- While a request for a release is open, any new request for the same release (same match key as above) is rejected with "This release has already been requested." Each request has exactly one requester.

### Admin approval

The admin queue shows each pending request's release, size, indexer, requester, comment, and age. The admin can:

- Approve it, optionally giving the library item a display title. Without one, the item uses the release name.
- Approve it with a different release. The admin searches from inside the request and picks a replacement. The original release stays on the request as history.
- Deny it, optionally with a reason the requester will see.

### Grabbing

- For torrents, softwarr downloads the `.torrent` from the release's download URL (usually proxied by Prowlarr) and uploads it to the client, or passes the magnet link. It records the infohash, computed from the `.torrent` or read from the magnet, to track the download. Uploading the file means the download client never needs to reach Prowlarr.
- If the download URL is stale or broken, the request becomes `failed` with the error. The admin can retry or substitute another release.
- Retry first checks whether the client already has the infohash. If so, softwarr tracks the existing torrent instead of adding it again.

### Stuck downloads

- Nothing times out automatically. The Activity page marks a download as stalled after a set period with no progress (24 hours by default). There is no notification for stalled downloads.
- An admin can abandon a request that is `approved`, `downloading`, or `failed`. It becomes `abandoned`, polling stops, and the release can be requested again. The torrent stays in the client, and removing it is up to the admin in qBittorrent.

## Download clients

- Clients implement a Go interface, roughly `Test()`, `Add(release) → clientItemID`, `Get(clientItemID) → status/progress/savePath/files`, and `Protocol()`, and are registered by type.
- qBittorrent (phase 1) uses Web API v2: cookie login, `torrents/add` with a configurable category (default `softwarr`) and a tag, then `torrents/info` and `torrents/files` for tracking. Settings: host, port, TLS, URL base, username and password, category, and a Test button.
- softwarr only adds torrents and reads their state. It never pauses, removes, or recategorizes them.
- softwarr polls for completion, every 10 to 30 seconds while anything is active. A torrent is complete when its progress is 1 and its state shows it finished or seeding.
- Later clients, in order: SABnzbd and NZBGet, which make Newznab results requestable, then Transmission and Deluge.
- More than one client per protocol can be configured, in priority order. softwarr uses the first enabled client for the release's protocol.

### Remote path mappings

As in the *arrs, each mapping is a (download client host, remote path, local path) entry that turns the save path the client reports into a path softwarr can read. A health check flags paths softwarr can't read.

## Import

When a download completes, softwarr:

1. Resolves the completed files to local paths using the remote path mappings.
2. Creates `{libraryRoot}/{sanitized release name}/`, adding a suffix if the folder exists. The display title is only metadata, so changing it never moves files.
3. For torrents, hardlinks every file into that folder, keeping the relative structure. If hardlinking fails because the paths are on different filesystems, it copies the files instead, records `import_mode = copy`, and raises a health warning explaining that hardlinks aren't working and why.
4. For Usenet (later), moves the files, since nothing seeds them.
5. Records the library item and its file list with sizes, and sets the request to `available` (or `scanning`, once scanning exists).
6. Notifies the requester and fires the webhook event.

The source files are never modified or deleted.

## Library

### Items

Each item stores a display title (optional, falling back to the release name), the release name, total size, file list, protocol, indexer, the original request (requester, comment, approver), when it was imported and last downloaded, download count, import mode, and a retention override.

Every signed-in user can browse, search, filter, and download library items. Admins can also edit the title and retention override, and delete items.

### Delivery

- A single file that is already an archive or installer (`.zip`, `.7z`, `.rar`, `.dmg`, `.iso`, `.pkg`, `.exe`, `.msi`, and similar) is served as-is with range support through `http.ServeContent`, so interrupted downloads can resume.
- Anything else (folders, multiple files, split RARs) is streamed as a zip built on the fly with the Store method, so nothing is compressed. That needs no temp file or extra disk, and the download starts immediately. Streamed zips can't resume, which is acceptable.
- Each download updates `last_downloaded_at` and the download count.

### Retention

- The global default, set by an admin, deletes library items that haven't been downloaded in N days, counted from whichever is later: the import or the last download. Blank or 0 means items never expire.
- Each item can override it: blank uses the global default, `0` never expires, and N means N days.
- A daily job applies retention. Expiring an item deletes only the library copy (its hardlinks or copied files) and marks the item unavailable. Seeding data isn't touched, and the request history stays.
- Expiry fires an `item.expired` event.

## Notifications

- In-app notifications appear in a per-user list under a bell icon, delivered live over SSE.
  - Requesters hear about approval, denial (with the reason), failure, and availability.
  - Admins hear about new pending requests, failed downloads or imports, and health warnings.
- Admins can add one or more webhooks, each subscribed to chosen events: `request.created`, `request.approved`, `request.denied`, `request.failed`, `item.available`, `item.expired`, `health.warning`. Each event is a JSON POST with the event type, a timestamp, and the relevant request, item, and user fields. An optional shared secret adds an HMAC-SHA256 signature header. Each webhook has a Test button.

## UI

The UI follows the *arr look: dark theme, left sidebar, dense tables, settings split into sections, a toolbar above each table, and toasts for actions. Mantine is themed to match.

### Everyone

- Search: a query box and a results table with title, indexer, protocol, size, age, peers or grabs, and a status badge. Clicking a result opens the request dialog with the comment field.
- Library: available items with title, release name, size, date added, requester, and a download button, with filtering and sorting.
- My Requests: the user's requests with their status, live progress while downloading, and any denial reason. Pending requests can be cancelled.
- The notification bell, and a profile page for changing a local password or connecting OIDC (for eligible local accounts).

### Admins also get

- Requests: every request, pending first, with approve, approve with a different release, and deny.
- Activity: downloads and imports in progress with live progress, stalled downloads, and failures with retry and abandon.
- Settings:
  - General: external URL, API key, whether the comment is required, default retention
  - Authentication: OIDC, auto-provisioning, link by verified email, admin group
  - Indexers: Prowlarr and categories
  - Download Clients
  - Remote Path Mappings
  - Library: library root path
  - Notifications: webhooks
  - Users: create or disable local users, approve pending OIDC users, set admin and auto-approve
- System: health checks (Prowlarr and clients reachable, paths readable, hardlink fallback in use), logs, and version.

```
┌──────────┬────────────────────────────────────────────────────┐
│ softwarr │  Search  [ photoshop                      ] (⌕)   │
│          ├────────────────────────────────────────────────────┤
│ Search   │ Title                         Idx  Size   Age  S/L │
│ Library  │ Adobe.Photoshop.2025.v26.1…   TL   4.1G   3d  120/4│ [Available]
│ Requests │ Adobe.Photoshop.2025.macOS…   NZB  3.8G   5d   —   │ [No client]
│ ──────── │ Photoshop.2024.x64-TNT        RB   3.9G  40d  33/1 │ [Request]
│ Activity │                                                    │
│ Settings │                                                    │
│ System   │                                                    │
└──────────┴────────────────────────────────────────────────────┘
```

## Phases

1. MVP: setup wizard and owner account, owner password reset, local auth, OIDC with the rules above, Prowlarr search (Torznab and Newznab, with Newznab marked "No compatible download client"), requests and approval, qBittorrent, remote path mappings, import with hardlinks and copy fallback, library and delivery, retention, in-app and webhook notifications, health checks, and the Docker image.
2. SABnzbd and NZBGet (Usenet imports move files), then Transmission and Deluge.
3. Malware scanning. This adds a `scanning` state between importing and available. ClamAV runs as a clamd sidecar and scans the library copy, and an optional VirusTotal lookup checks file hashes without uploading anything by default. Admins see results per item and per file. A policy setting chooses between making clean items available automatically and holding flagged items for review. Cracks and keygens are routinely flagged as HackTool or PUA, so flagged items are held for review by default and never deleted automatically, and the UI shows detection names so admins can judge for themselves.

## Left to the builder

- The database schema, table names, and migration layout.
- CSRF mechanism, session lifetime, and cookie names.
- Poll intervals and SSE event shapes.
- The full list of archive and installer extensions served directly.
- Webhook payload field names.
- The logging library and log retention.
- The Docker base image, and whether to support linuxserver-style PUID/PGID or run as a fixed non-root UID.
- The test strategy. At minimum, unit tests for the request state machine, import, and auth rules. Integration tests against a real qBittorrent container are encouraged.
