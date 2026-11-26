# Release workflow

## Why

Cutting a release meant choosing a version by hand and pushing a tag. The maintainer wants a button that works out the right version from the conventional commits since the last release and does the rest.

## Goals

- A manually run "Release" workflow on `main` that picks the next version, runs CI, publishes the images, and creates the tag and a GitHub release with notes.
- A dry run that only reports the version and the notes.

## Non-goals

- A release pull request or a `CHANGELOG.md` in the repo.
- Releasing automatically when commits land on `main`.

## Anti-goals

- A release whose images failed to build. The tag and the GitHub release come last.
- New secrets. Everything runs on `GITHUB_TOKEN`.

## Approaches

1. A Release button (`workflow_dispatch`) that does everything itself, with no new secrets. A person decides when to ship.
2. A release pull request kept up to date by release-please. It needs a GitHub App or personal token, because pull requests and tags made with `GITHUB_TOKEN` don't start other workflows.
3. A release on every merge. Renovate's automerged `fix(deps)` updates would produce a stream of patch releases nobody chose.

The user picked 1.

For the version tool, the user asked for a well-established tool. The options:

- git-cliff (chosen). It works out the next version and writes the notes, and it has settings for 0.x versions.
- semantic-release. It wants to run the whole release, tag included, so it doesn't fit a workflow that tags only after the images are built. It also needs Node in the workflow.
- svu. It works out versions only, so the notes would need a second tool.
- A small Go program written for this repo. It would be easy to test, but the user preferred a tool with a track record.

## Design

### Version rules

Read every commit since the newest `v*` tag reachable from `HEAD`, subject and body:

- A breaking change (`type!:` or `type(scope)!:`, or a `BREAKING CHANGE:` footer) bumps the major version, or the minor version while it's below 1.0.
- `feat` bumps the minor version.
- `fix` and `perf` bump the patch, including `fix(deps)`.
- Anything else (`docs`, `refactor`, `test`, `ci`, `chore`, `build`, `style`, or a subject that isn't a conventional commit) doesn't need a release on its own.
- The highest bump wins. If there's no bump, there's nothing to release, and the workflow ends successfully without tagging.

Going to 1.0 isn't automatic. When the maintainer wants it, they tag `v1.0.0` by hand once.

### The version tool

[git-cliff](https://git-cliff.org) works out the version and writes the notes, configured by `cliff.toml` at the repo root. The user asked for a well-established tool over a hand-written one. git-cliff is widely used, reads conventional commits, and has settings for the 0.x rules:

- `[bump] features_always_bump_minor = true` and `breaking_always_bump_major = false`, which only apply below 1.0.
- `[bump] initial_tag = "v0.1.0"`, so a repo with no tag still gets a version.
- `[git] commit_parsers` group `feat`, `fix` and `perf` commits and skip everything else, so other commits neither bump the version nor show up in the notes.
- `tag_pattern` matches `vX.Y.Z` only.

`git cliff --bumped-version` prints the next version. When nothing bumps, it prints the current tag and warns "There is nothing to bump". The workflow treats a version equal to the latest tag as nothing to release. `git cliff --unreleased --bump --strip all` prints the notes: sections for features, fixes and performance, one line per commit with its scope and short hash.

These results come from git-cliff 2.10.1's Docker image, run against this repo and some throwaway ones:

| Commits since the last tag | Result |
| --- | --- |
| `docs` and `chore(deps)` only, after `v0.1.0` | nothing to bump |
| `fix`, after `v0.1.0` | `v0.1.1` |
| `feat!:` or a `BREAKING CHANGE:` footer, after `v0.1.0` | `v0.2.0` |
| `feat!:`, after `v1.2.3` | `v2.0.0` |
| This repo's history after `v0.1.0` | `v0.2.0` |

The workflow runs it through `orhun/git-cliff-action`, pinned by SHA. Renovate keeps the pin current.

### Workflows

- `.github/workflows/publish.yml` (new, `workflow_call`) holds the image build that's in `cd.yml` now. Its inputs are a `version` and whether it's a release. A release publishes `X.Y.Z`, `X.Y` and `latest`. Otherwise it publishes `main`.
- `cd.yml` keeps running CI on pushes to `main` and calls `publish.yml`. Its tag trigger stays, so a tag pushed by hand (such as `v1.0.0`) still publishes.
- `.github/workflows/release.yml` (new, `workflow_dispatch`, with a `dry_run` checkbox that's off by default) runs these jobs in order:
  1. `version`: refuses to run from any branch but `main`, works out the version and notes, and writes both to the run summary. A dry run, or nothing to release, stops here.
  2. `ci`: calls `ci.yml`.
  3. `publish`: calls `publish.yml` with the version.
  4. `release`: creates the tag on the commit the run started from, and a GitHub release with the notes, using `gh` and `contents: write`.
- `concurrency: release`, so two runs can't race for the same version.
- Every `uses:` is pinned to a commit SHA with its version in a comment, as AGENTS.md requires.

### Docs

- `docs/ci-and-releases.md`: how to run the release, the version rules, where they're configured (`cliff.toml`), and that a tag pushed by hand still works.
- AGENTS.md keeps "agents never push tags or publish releases". The workflow is run by a person.

## Left to the builder

- How `publish.yml` gets docker/metadata-action to produce release tags from a version input instead of a tag ref.
- The exact notes layout in `cliff.toml`.
