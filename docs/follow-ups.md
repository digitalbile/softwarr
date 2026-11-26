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
- If someone removes the last item's folder by hand, that item can't be deleted. The library folder is then empty while the database still lists an item under it, which looks the same as an unmounted drive. Delete keeps returning 409 until something else is in the library folder.
- When a request is imported again after the library folder has changed, its earlier import folder stays behind in the old location. The worker logs the path. An admin has to remove it by hand.
- The removal guard checks that an item's folder isn't a symlink, but it doesn't check the folders above it, because a library folder that is a symlink is a normal setup. Only softwarr writes the stored folder path, so this matters only if the database is edited by hand.
- A library folder at `/` can't delete items, because the guard refuses folders directly under `/`.
- While "Disable local auth" is on, an admin can still create local users, and those users can't sign in. The users page could warn about it.

## Planned features

- A "move library" action. Right now, changing the library folder in settings leaves existing items at their old location: they still download and delete there if their files haven't moved, and show as gone if they have. The action would check each item's files at the new location, then update the saved paths. Radarr handles this the same way, through its editor.

## Not yet tested against live services

- How qBittorrent 5 responds to adding a torrent it already has.
- Prowlarr's default category IDs for PC and macOS software.
- File ownership on bind mounts on a Linux host. The smoke test uses named volumes because Docker Desktop on macOS reports owners incorrectly.
