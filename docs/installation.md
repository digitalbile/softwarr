# Installation

softwarr runs as one Docker container next to your qBittorrent and Prowlarr.

## Docker Compose

Add this service to your compose file:

```yaml
services:
  softwarr:
    image: ghcr.io/digitalbile/softwarr:latest
    container_name: softwarr
    environment:
      PUID: "1000"
      PGID: "1000"
      UMASK: "022"
      TZ: Etc/UTC
    volumes:
      - ./config/softwarr:/config
      - /srv/data:/data
    ports:
      - "7979:7979"
    restart: unless-stopped
```

Change `/srv/data` to the host folder that holds your downloads, and mount the same folder at `/data` in qBittorrent. Save torrents to `/data/torrents` in qBittorrent and set softwarr's library root path to `/data/library`. Give softwarr the same `PUID`, `PGID`, and `UMASK` as qBittorrent. [Hardlinks](#hardlinks) explains why both matter.

Start it with `docker compose up -d` and open `http://localhost:7979`. The first-run wizard creates the owner account. Then add Prowlarr and qBittorrent in Settings. If they're in the same compose file, use their service names as hosts, such as `http://prowlarr:9696` and `http://qbittorrent:8080`.

## Image tags

Images are published to `ghcr.io/digitalbile/softwarr` for amd64 and arm64.

| Tag | What it is |
| --- | --- |
| `latest` | The newest release |
| `0.1`, `0.1.0` | A release line, or one exact release |
| `main` | The current main branch |

To build the image yourself, run `make docker` in a clone of this repository.

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

## Hardlinks

By default softwarr imports finished torrents by hardlinking them into the library, so the library copy takes no extra space and the torrent keeps seeding. This works only when:

- The download folder and the library are on the same filesystem, and in a container, under the same mount. The compose service above mounts one `/data` folder and uses `/data/torrents` and `/data/library` inside it. Don't mount `/data/torrents` and `/data/library` as separate volumes, because hardlinks fail across mounts.
- softwarr runs as the same user as qBittorrent. Linux's default `fs.protected_hardlinks=1` refuses to hardlink files the linking user can't write. Give both containers the same `PUID`, `PGID`, and `UMASK`.

If hardlinking fails, softwarr copies the files instead and shows a warning on the System page.

## Paths between qBittorrent and softwarr

Mount the same host folder at the same container path in qBittorrent and softwarr. Then the save paths qBittorrent reports are valid inside softwarr and you don't need a remote path mapping. If the paths differ, add a mapping under Settings.

## Changing the library folder

Changing the library folder in Settings doesn't move existing items. They stay where they were imported and are still served from there. Moving them is planned as a separate action.

If the library folder is missing, or empty while softwarr has items in it, softwarr treats it as unmounted: imports wait, items are kept, and the System page shows a warning.
