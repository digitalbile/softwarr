# Contributing to softwarr

This covers how to propose a change and get it merged. [ARCHITECTURE.md](ARCHITECTURE.md) explains how the code is organized, and [AGENTS.md](AGENTS.md) has the rules for working in it safely. Those rules apply to people too.

## Before you start

Look through the [issues](https://github.com/digitalbile/softwarr/issues) for one that covers your change. For anything bigger than a small fix, open an issue first and describe what you want to do, so we can agree on the approach before you write the code. Typo fixes and small bug fixes can go straight to a pull request.

Read the [decision records](docs/adr/README.md) that touch your change. If you disagree with one, say why in an issue.

Report security problems privately through [GitHub's vulnerability reporting](https://github.com/digitalbile/softwarr/security/advisories/new), never in a public issue.

## Setting up

You need:

- Go, at the version in `go.mod`
- Node.js 24
- [golangci-lint](https://golangci-lint.run/) v2, for `make lint`
- Docker, for `make smoke`

```bash
git clone https://github.com/digitalbile/softwarr.git
cd softwarr
make web        # install the UI dependencies and build web/dist
make build
mkdir -p data
SOFTWARR_DATA_DIR=./data ./softwarr
```

softwarr is then at `http://localhost:7979`. To work on the UI with hot reload, keep the server running and start Vite in another terminal with `cd web && npm run dev`. Vite sends `/api` requests to port 7979.

To test against real services, run test instances of Prowlarr and qBittorrent and point softwarr at them in Settings. Never use ones that seed for a private tracker.

## Making a change

Branch from `main` with a short kebab-case name, such as `request-settings` or `fix-import-retry`. Keep each pull request to one logical change. If you find something else that needs fixing, open an issue for it or add it to `docs/follow-ups.md`.

What a change needs in tests:

| Change | Tests |
| --- | --- |
| Bug fix | A test that fails without the fix |
| New API behavior | Go tests in the package that owns it, and a handler test for new routes |
| New UI behavior | A Vitest test that drives the page the way a user would |
| Refactor | The existing tests pass unchanged |
| Copy or styling only | None, but say in the pull request what you checked by hand |

Before you push, run the same checks as CI:

```bash
make lint
make test
cd web && npm test && npm run build
```

`make smoke` builds the Docker image and checks it starts. Run it when you change the `Dockerfile`, anything under `docker/`, or how the binary starts.

## Commit messages

Commits follow [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): summary

Body explaining why, when the summary doesn't make it obvious.
```

- The type is `feat`, `fix`, `perf`, `refactor`, `test`, `docs`, `ci` or `chore`. It sets the next version: `feat` bumps the minor version, `fix` and `perf` bump the patch, and the others don't need a release. Mark a breaking change with `!` after the type (`feat!:`) or a `BREAKING CHANGE:` footer.
- The scope is optional. Use the package or area the change is in, such as `requests`, `library`, `auth`, `settings` or `web`. Renovate uses `deps`.
- Write the summary in the imperative mood, in lowercase, with no period at the end, and keep it under 72 characters.
- Use the body to explain why. The diff already shows what changed.
- Don't add `Co-authored-by` trailers or "Generated with" lines.

Pull requests are rebase-merged, so every commit lands on `main` as it is. Each one should build and pass the tests on its own. Squash fixup commits before you ask for review.

## Pull requests

Open the pull request against `main` and fill in the template. CI runs the linters, the Go and UI tests, the UI build and the smoke test. The `check` job has to pass before the pull request can merge. To update the branch, push more commits or rebase on `main` and push with `--force-with-lease`.

Changes to the API need the matching change in `web/src/api/types.ts` in the same pull request (see ARCHITECTURE.md). Changes to the schema need a new migration. Never edit a migration that has been released.

## Writing

Run UI text, error messages, docs, commit messages and pull request descriptions through the [humanizer](https://github.com/blader/humanizer) plugin. User-facing text is plain and short and uses no em dashes. Error messages that users see are full sentences.

Keep the README short. Documentation for people running softwarr lives in the [wiki](https://github.com/digitalbile/softwarr/wiki), and describes the latest release. If your change affects it, say in the pull request which page needs updating.

## Using AI tools

AI coding agents are welcome here. Point yours at [AGENTS.md](AGENTS.md); Claude Code reads it through `CLAUDE.md`. Whoever opens the pull request is responsible for every line in it:

- Read and understand the whole diff before you open it, and be ready to explain any part of it.
- Run the checks yourself and try the change in the app. Agents often write tests that pass whether or not the code works, so check that each new test fails when the code it covers is broken.
- Say in the pull request what the agent did and how you checked its work.

## Reporting bugs

Search the issues first. A useful report has:

- the softwarr version (in the System page, or the image tag)
- how you run it (Docker image, compose, or a binary you built)
- what you did, what happened, and what you expected
- the relevant lines from the log on the System page, with URLs, API keys and paths you'd rather not share removed

## Releases

The maintainer cuts releases with the Release workflow, which works out the version from the conventional commits since the last release and publishes the images. [docs/ci-and-releases.md](docs/ci-and-releases.md) has the details. Don't change version numbers or push tags in a pull request.
