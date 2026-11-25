# softwarr

softwarr is a self-hosted request and download manager for PC and macOS software, in the style of the *arr apps. Users search Torznab sources through Prowlarr and request what they want. Admins approve requests. softwarr sends approved torrents to qBittorrent, imports the finished download into a library, and lets users download the files from the web UI.

It is a single Go binary with the web UI built in, shipped as one Docker image. Settings and data are stored in SQLite.

Current limits:

- qBittorrent is the only download client. Usenet (Newznab) results appear in search with the label "No compatible download client" until SABnzbd and NZBGet support is added.
- Search goes through Prowlarr. softwarr doesn't talk to indexers directly.

## Quick start

You need qBittorrent and Prowlarr already running.

1. Add the softwarr service from the [installation guide](https://github.com/digitalbile/softwarr/wiki/Installation#docker-compose) to your compose file. Set its host paths and its `PUID` and `PGID` to match qBittorrent.
2. Run `docker compose up -d`.
3. Open `http://localhost:7979`. The first-run wizard creates the owner account.
4. In Settings, add Prowlarr and qBittorrent, then set the library root path.

## Documentation

The [wiki](https://github.com/digitalbile/softwarr/wiki) covers running softwarr:

- [Installation](https://github.com/digitalbile/softwarr/wiki/Installation): the compose service, image tags, environment variables, volumes, permissions, and hardlinks
- [Authentication](https://github.com/digitalbile/softwarr/wiki/Authentication): OIDC sign-in and recovering the owner account

To work on the code, start with [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT. See `LICENSE`.

The Sonarr, Radarr, and Prowlarr projects are reference only. Their source was read to learn behavior, and no code from them is used here.
