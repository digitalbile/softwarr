# softwarr

softwarr is a self-hosted request and download manager for PC and macOS software, in the style of the *arr apps. Users search Torznab sources through Prowlarr and request what they want. Admins approve requests. softwarr sends approved torrents to qBittorrent, imports the finished download into a library, and lets users download the files from the web UI.

It is a single Go binary with the web UI built in, shipped as one Docker image. Settings and data are stored in SQLite.

Current limits:

- qBittorrent is the only download client in this release. Usenet (Newznab) results appear in search with the label "No compatible download client" until SABnzbd and NZBGet support is added.
- Search goes through Prowlarr. softwarr doesn't talk to indexers directly.

## Quick start

1. Copy `deploy/compose.example.yaml` to a folder on your server and adjust the host paths and the `PUID` and `PGID` values. The example runs softwarr, qBittorrent, and Prowlarr.
2. Build the image from a clone of this repository with `make docker`, or point the `image:` line at your own registry.
3. Run `docker compose -f compose.example.yaml up -d`.
4. Open `http://localhost:7979`. The first-run wizard creates the owner account.
5. In Settings, add Prowlarr and qBittorrent, then set the library root path.

In the example stack, use `http://prowlarr:9696` and `http://qbittorrent:8080` as the URLs.

## Environment variables

These need a container restart to change. Everything else is configured in the web UI.

| Variable | Default | Purpose |
| --- | --- | --- |
| `SOFTWARR_PORT` | `7979` | Port the server listens on. |
| `SOFTWARR_DATA_DIR` | `/config` | Directory for the SQLite database. |
| `SOFTWARR_URL_BASE` | empty | Path prefix when running behind a reverse proxy, for example `/softwarr`. |
| `PUID` | `1000` | User ID softwarr runs as. |
| `PGID` | `1000` | Group ID softwarr runs as. |
| `UMASK` | `022` | Permission mask for files softwarr creates. |

The image follows the linuxserver convention. It starts as root, creates a user from `PUID` and `PGID`, fixes the ownership of `/config` if needed, and then drops to that user. If you set `user:` in compose (or `--user` with `docker run`), the container skips this and runs as the user you gave. In that case `/config` must already be writable by that user.

## Volumes

- `/config` holds the database. Back this up.
- Your downloads and library folders. softwarr needs to read the finished downloads and write to the library.

softwarr does not change ownership of your downloads or library folders.

### Hardlinks

By default softwarr imports finished torrents by hardlinking them into the library, so the library copy takes no extra space and the torrent keeps seeding. This works only when:

- The download folder and the library are on the same filesystem, and in a container, under the same mount. The compose example mounts one `/data` folder and uses `/data/torrents` and `/data/library` inside it.
- softwarr runs as the same user as qBittorrent. Linux's default `fs.protected_hardlinks=1` refuses to hardlink files the linking user can't write. Give both containers the same `PUID`, `PGID`, and `UMASK`.

If hardlinking fails, softwarr copies the files instead and shows a warning on the System page.

Mount the same host folder at the same container path in qBittorrent and softwarr. Then the save paths qBittorrent reports are valid inside softwarr and you don't need a remote path mapping. If the paths differ, add a mapping under Settings.

## Recovering the owner account

If the owner loses their password, run this on the Docker host:

```
docker exec softwarr softwarr reset-password
```

It prints a temporary password, ends the owner's sessions, and asks for a new password at the next sign-in. There is no other recovery path. Anyone who can run this command already controls the host.

## OIDC sign-in

softwarr supports one OIDC provider alongside local accounts. The owner account is always local, and the local login form stays available in case your provider is down.

1. In Settings, General, set the External URL to the public address of softwarr, for example `https://softwarr.example.com`. OIDC can't be enabled without it.
2. In your identity provider, create an OIDC client and set the redirect URI to `<external URL>/api/v1/auth/oidc/callback`.
3. In Settings, Authentication, enter the issuer URL, client ID, and client secret, then turn on single sign-on.
4. Optional: to give admin rights through a group, set the claim name (for example `groups`) and the admin value (for example `softwarr-admins`). OIDC users gain or lose admin status from that group each time they sign in. Local-only users aren't affected.

## Development

```
make web      # build the web UI into web/dist
make build    # build the binary
make test     # Go tests
make docker   # build the Docker image
make smoke    # build the image and run the end-to-end check
```

The web UI tests run with `cd web && npx vitest run`.

## License

MIT. See `LICENSE`.

The Sonarr, Radarr, and Prowlarr projects are reference only. Their source was read to learn behavior, and no code from them is used here.
