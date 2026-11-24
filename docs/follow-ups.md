# Follow-ups

Known work left over from phase 1. None of it blocks a release.

## Performance

These came from the code review of the phase-1 branch.

- Every authenticated request runs three queries: `GetSession`, `GetUserByID`, and `UserHasIdentity`. The middleware never uses the identity check, which `auth.toUser` runs every time. `Users.List` runs it once per user, so the users page makes N+1 queries. Fix it with a lighter lookup for the middleware, or an `EXISTS` subquery in the user query.
- `library.statusWriter` embeds only `http.ResponseWriter`, so `http.ServeContent` can't reach the underlying `io.ReaderFrom`. Single-archive downloads go through user space in 32 KB chunks instead of `sendfile`. Fix it by giving `statusWriter` a `ReadFrom` method that passes through to the wrapped writer.
- `ApplyRetention` calls `List`, which loads every file row of every available item through `ListAllAvailableLibraryFiles`. Retention only needs the item rows. The library page does the same on every load. Fix it with an item-only query for retention, and page the file lookups.

## Smaller issues

- A request with a wrong API key and no CSRF header gets a CSRF error. "Invalid API key" with a 401 would be clearer.
- `redactURL` keeps the URL path. A tracker that puts its passkey in the path, not the query string, could still show it in a request's error text.
- If an install already has an admin claim name saved with an empty value, it can't save settings until that pair is fixed. The error message explains what to change.
- If an admin resets a user's password while that user is signed in, the user's live updates keep retrying every 30 seconds. The user only reaches the change-password page on their next ordinary request. The reconnect check could call `refresh()` when `/auth/me` reports `mustChangePassword`.
- The `.softwarr-item` marker holds only a request ID. Say a seeding folder came from a torrent of someone else's softwarr library, and the library folder were later pointed at the downloads folder. A marker in that seeding folder could then match a small request ID. A per-install UUID in both markers would close this.
- Settings writes the library folder's `.softwarr-library` marker before it validates the rest of the save. A save that fails later, for example on an OIDC setting, has already created and marked the new folder.

## Not yet tested against live services

- How qBittorrent 5 responds to adding a torrent it already has.
- Prowlarr's default category IDs for PC and macOS software.
- File ownership on bind mounts on a Linux host. The smoke test uses named volumes because Docker Desktop on macOS reports owners incorrectly.
