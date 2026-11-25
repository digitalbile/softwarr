# softwarr

softwarr is a self-hosted request and download manager for PC and macOS software, in the style of the *arr apps. Users search Torznab sources through Prowlarr and request what they want. Admins approve requests. softwarr sends approved torrents to qBittorrent, imports the finished download into a library, and lets users download the files from the web UI.

It is a single Go binary with the web UI built in, shipped as one Docker image. Settings and data are stored in SQLite.

Current limits:

- qBittorrent is the only download client. Usenet (Newznab) results appear in search with the label "No compatible download client" until SABnzbd and NZBGet support is added.
- Search goes through Prowlarr. softwarr doesn't talk to indexers directly.

## Quick start

1. Copy `deploy/compose.example.yaml` to a folder on your server and adjust the host paths and the `PUID` and `PGID` values. The example runs softwarr, qBittorrent, and Prowlarr.
2. Run `docker compose -f compose.example.yaml up -d`.
3. Open `http://localhost:7979`. The first-run wizard creates the owner account.
4. In Settings, add Prowlarr (`http://prowlarr:9696`) and qBittorrent (`http://qbittorrent:8080`), then set the library root path.

## Documentation

- [Installation](docs/installation.md): image tags, environment variables, volumes, permissions, and hardlinks
- [Authentication](docs/authentication.md): OIDC sign-in and recovering the owner account
- [Contributing](CONTRIBUTING.md): setting up for development and opening a pull request

## License

MIT. See `LICENSE`.

The Sonarr, Radarr, and Prowlarr projects are reference only. Their source was read to learn behavior, and no code from them is used here.
