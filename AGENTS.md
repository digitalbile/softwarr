# AGENTS.md

Guidance for AI coding agents working on softwarr, and for the people reviewing their work. The contribution workflow (issues, branches, commits, pull requests) is in [CONTRIBUTING.md](CONTRIBUTING.md). How the code is laid out, and where new code goes, is in [ARCHITECTURE.md](ARCHITECTURE.md). Read both before a change of any size.

softwarr is a self-hosted request manager for PC and macOS software. Users search indexers through Prowlarr and request releases, admins approve them, and softwarr sends approved torrents to qBittorrent and imports the finished files into a library that users download from. It runs next to the user's own download client and media folders, so a bug can do damage outside softwarr: delete or move files the user cares about, disturb torrents they are seeding, or leak an indexer passkey. The safety rules below exist for that reason.

## Commands

```bash
make test                       # Go tests
make lint                       # golangci-lint and Biome; golangci-lint must be installed
make generate                   # regenerate internal/db/dbq after editing internal/db/queries
make web                        # build the UI into web/dist (the Go binary embeds it)
make build                      # build the binary
make smoke                      # build the Docker image and run the end-to-end check

cd web && npm test              # UI tests (Vitest)
cd web && npm run lint          # Biome lint and format check
cd web && npm run format        # fix formatting and import order
golangci-lint fmt               # fix Go formatting and import grouping
```

While iterating, run the tests of the package you changed (`go test ./internal/requests`, or `npx vitest run src/pages/settings` in `web/`). Run `make lint`, `make test` and `cd web && npm test` before you call the work done.

CI runs all of these on every pull request, along with `go mod tidy -diff`, `sqlc diff`, the UI build, and `make smoke`. A pull request can't merge until the `check` job passes.

## Safety rules

- Download clients are add and read only. softwarr adds downloads and reads their progress. It never pauses, removes, rechecks, moves or recategorizes a download, because the user may be seeding it for a private tracker. The `downloadclient.Client` interface has no method for any of that; don't add one.
- Delete only inside a library item's own folder. Removal goes through `checkFolder` in `internal/library/service.go`, which refuses relative paths, the filesystem root and folders directly under it, and symlinks. Anything new that deletes files must call it. Never delete or move anything in the download folders.
- Never change ownership or permissions of the downloads or the library. The container fixes ownership of `/config` only (`docker/entrypoint.sh`).
- Download links stay on the server. Indexer and tracker URLs often carry an API key or passkey. `prowlarr.Release` leaves its links out of JSON (`json:"-"`), and the browser gets an encrypted token from `releasetoken` instead. Text that reaches users or the logs shows a URL only after `redactURL` (in `internal/requests`) has removed its credentials, query and fragment.
- Users and admins see different errors. A request's `error` is a sentence for the requester. Paths, URLs and the download client's own words go in `errorDetail`, which only admins see. Follow `fail` and `msgGrabFailed` in `internal/requests/worker.go`.
- Keep the owner able to sign in. The owner account is always local and can always sign in with a password. When "Disable local auth" is on, the login page hides the form and `/login?local=1` shows it again ([ADR 0004](docs/adr/0004-owner-only-password-sign-in.md)). Don't add a path that disables, demotes or deletes the owner, or that blocks the owner's password sign-in.
- Every API route is wrapped in `requireUser` or `requireAdmin`. The only public routes are setup, login, the OIDC flow and `/api/v1/ping`. A route that returns user data checks that the data belongs to the caller unless the caller is an admin.
- Never test against a live Prowlarr, indexer or qBittorrent. Fake them with `httptest` servers, as the existing tests do. The qBittorrent integration test runs only with `-tags integration` and `QBT_URL` set to a test instance; CI doesn't run it.
- Bound calls to outside services. The worker ticks every 15 seconds and the health checks every 5 minutes. A change that calls Prowlarr, an indexer or the download client once per item on those paths needs a limit.

## Conventions

These rules have a check that enforces them. When the check fails, fix the code; don't weaken the check.

- Handlers call services and never query the database. `httpapi.Deps` has no database handle, so the compiler enforces this. A new resource gets a service or store in its domain package (ARCHITECTURE.md, "Handlers").
- `internal/db/dbq` is generated. Edit the SQL in `internal/db/queries` and run `make generate`. CI runs `sqlc diff`.
- `web/src/api/types.ts` is written by hand. When a response changes in Go, change it in the same pull request. `internal/httpapi/apitypes_test.go` fails if the field names differ, and a new interface needs an entry in its `apiTypes` map. See [ADR 0001](docs/adr/0001-hand-written-api-types.md).
- Go is checked by golangci-lint (`.golangci.yml`) and the UI by Biome (`web/biome.json`). A `//nolint` or `biome-ignore` comment says why, as the existing ones do.

These are conventions without a check:

- Every bug fix comes with a test that fails without the fix.
- Migrations are numbered in sequence (`internal/db/migrations`). If `main` gains your number while your branch is open, renumber yours. Never edit a migration that has been released; add a new one.
- Services get their clock (`now func() time.Time`) and `*http.Client` through their constructor. Don't call `time.Now()` or `http.DefaultClient` inside a service.
- Error strings that users see are full sentences, which is why golangci-lint's ST1005 check is turned off.
- Before writing a helper, look for one that already does the job, and extend it if it nearly does.
- Keep a pull request to one logical change. Pull requests are rebase-merged, so each commit should build and pass tests on its own.

## Writing

Run all copy through the [humanizer](https://github.com/blader/humanizer) plugin before it ships: UI text, error messages, docs, commit messages and pull request descriptions. In Claude Code, install the plugin and use its `humanizer:humanizer` skill on the text you wrote. Leave code, paths, commands and identifiers as they are.

The plugin's rules that come up most here: no em dashes, no "not X but Y" contrasts, sentence-case headings, and no bold labels at the start of list items. UI text and error messages are plain and short.

The README stays short: what softwarr is, the current limits, the quick start, and links. Documentation for people running softwarr lives in the [wiki](https://github.com/digitalbile/softwarr/wiki), and `docs/` is for people and agents working on the code. The wiki isn't versioned with the code, so a page should describe the latest release. When a change affects how people run or configure softwarr, say in the pull request which wiki page needs updating; the maintainer updates it when the change is released.

## CI, images and releases

Read [docs/ci-and-releases.md](docs/ci-and-releases.md) before changing anything under `.github/`, the `Dockerfile`, or `docker/`.

- Every `uses:` in a workflow is pinned to a full commit SHA with its version in a comment, and base images are pinned by digest. Renovate updates the pins. Keep new ones in the same form.
- A person cuts a release by running the Release workflow, which works out the version from the conventional commits (`cliff.toml`), publishes the images, and then tags. Agents never run the Release workflow, push tags, publish releases, or push to `main`.
- `docs/follow-ups.md` lists known work. Check it before starting, and add to it anything you find but leave out of scope.

## Git

- Work on a branch, never on `main`.
- Commit messages follow [CONTRIBUTING.md](CONTRIBUTING.md#commit-messages).
- Don't add `Co-authored-by` trailers or "Generated with" lines to commits or pull requests.
- Never force-push a branch someone else is working on, and never rewrite `main`.

## Decisions

[docs/adr/](docs/adr/README.md) records decisions someone might otherwise reopen. Read the relevant ones before proposing a change they rule out, and add one when you make a decision like that. `docs/superpowers/specs/2026-11-23-softwarr-design.md` is the original design.

## Scratch work

Implementation plans and other working notes go in `docs/superpowers/plans/`. Anything that should outlive the pull request belongs in its description, an issue, an ADR, or `docs/follow-ups.md`.
