# Release workflow: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a manually run Release workflow. It uses git-cliff to work out the next version from conventional commits, runs CI, publishes the images, then tags and creates a GitHub release with notes.

**Architecture:** `cliff.toml` holds the version rules and the notes template. The image build moves from `cd.yml` into a reusable `publish.yml`, which both `cd.yml` and the new `release.yml` call. `release.yml` runs four jobs in order: version, then CI, then publish, then release. The tag is created last, by `gh release create`.

**Tech stack:** GitHub Actions, git-cliff 2.x through `orhun/git-cliff-action`, docker/metadata-action, and the `gh` CLI.

**Spec:** `docs/superpowers/specs/2026-11-25-release-workflow-design.md`

## Global constraints

- Pin every `uses:` to a full commit SHA with its version in a comment. Use `orhun/git-cliff-action@a9a95522b26fe6403f7bb24031f21fb573d0f5ff # v4.9.1` with `version: v2.14.2`.
- Read `docs/ci-and-releases.md` before changing `.github/`.
- Use only `GITHUB_TOKEN`, with no new secrets. Grant `contents: write` only to the job that creates the release.
- Tags match `vX.Y.Z`. Below 1.0, `feat` and breaking changes bump the minor version and `fix`/`perf` bump the patch. Nothing else bumps.
- Verification: workflow YAML can't run before it's on `main`, so each workflow task is checked with actionlint (`docker run --rm -v "$PWD":/repo -w /repo rhysd/actionlint:1.7.7 -color`). `cliff.toml` is checked with the git-cliff Docker image (`orhunp/git-cliff:2.10.1`) against this repo and throwaway repos. This is the TDD stand-in for configuration files.

## Review focus

1. A release run from a branch other than `main`. It must fail before computing anything.
2. Nothing to release (only `docs`/`chore` since the last tag). It must finish green with a summary and no tag.
3. A push to `main` after the refactor. It must still publish only the `main` image tag, and must not touch `latest`. Check the metadata-action `enable` and `flavor` expressions by reading them, because only a real push exercises them.
4. A tag pushed by hand (`v1.0.0`). `cd.yml` must still publish `1.0.0`, `1.0` and `latest`.
5. A failed image build. No tag or GitHub release may exist afterwards.

---

### Task 1: `cliff.toml`

**Files:** Create `cliff.toml`.

- [ ] **Step 1: Write `cliff.toml`.** It needs:
  - `[git]` with `conventional_commits = true`, `filter_unconventional = true`, `filter_commits = true`, and `tag_pattern = '^v[0-9]+\.[0-9]+\.[0-9]+$'`.
  - `commit_parsers` that put `^feat` under Features, `^fix` under Fixes and `^perf` under Performance, in that order using `<!-- n -->` prefixes, and skip everything else.
  - A breaking change of any type goes under a first "Breaking changes" group. Add a parser on `body = "BREAKING CHANGE"` and use `commit.breaking` in the template.
  - `[bump]` with `features_always_bump_minor = true`, `breaking_always_bump_major = false` and `initial_tag = "v0.1.0"`.
  - `[changelog]` with an empty header, and a body that prints `### <group>` followed by `- <scope>: <Message> (<short hash>)`.
- [ ] **Step 2: Verify against throwaway repos.** Expected results:

  | Commits since the last tag | Expected result |
  | --- | --- |
  | `docs`/`chore(deps)` only, after `v0.1.0` | `v0.1.0`, with the warning "nothing to bump" |
  | `fix`, after `v0.1.0` | `v0.1.1` |
  | `feat!:`, after `v0.1.0` | `v0.2.0` |
  | A `BREAKING CHANGE:` footer, after `v0.1.0` | `v0.2.0` |
  | `feat!:`, after `v1.2.3` | `v2.0.0` |
  | `fix`, with no tags | `v0.1.1` or `v0.1.0` (record which) |
- [ ] **Step 3: Verify against this repo.** `--bumped-version` gives `v0.2.0`. `--unreleased --bump --strip all` gives the eight `feat` commits and the two `fix` commits since `v0.1.0`, with no `docs` or `chore` lines.
- [ ] **Step 4: Commit** with `ci: add git-cliff config for release versions and notes`.

### Task 2: move the image build into `publish.yml`

**Files:** Create `.github/workflows/publish.yml`. Modify `.github/workflows/cd.yml`.

**Interfaces:** `publish.yml` takes `on: workflow_call` with inputs `version` (string, default `""`) and `release` (boolean, default `false`). It needs `permissions: contents: read, packages: write`.

- [ ] **Step 1: Write `publish.yml`** with the current `publish` job's steps.
  - The `version` step uses `inputs.version` when it's set, otherwise `git describe --tags --always`.
  - metadata-action tags:
    - `type=ref,event=branch,enable=${{ !inputs.release }}`
    - `type=semver,pattern={{version}},value=${{ inputs.version }},enable=${{ inputs.release }}`
    - the same with `{{major}}.{{minor}}`
  - metadata-action `flavor: latest=${{ inputs.release }}`.
- [ ] **Step 2: Change `cd.yml`.** Keep `ci`. Replace `publish` with `uses: ./.github/workflows/publish.yml`, `needs: ci`, the same permissions, and these inputs:
  - `release: ${{ startsWith(github.ref, 'refs/tags/v') }}`
  - `version: ${{ startsWith(github.ref, 'refs/tags/v') && github.ref_name || '' }}`

  Update the header comment.
- [ ] **Step 3: Run actionlint.** Expected: no findings.
- [ ] **Step 4: Commit** with `ci: move the image build into a reusable workflow`.

### Task 3: `release.yml`

**Files:** Create `.github/workflows/release.yml`.

- [ ] **Step 1: Write it.**
  - `on: workflow_dispatch` with input `dry_run` (boolean, default `false`, described as "Only show the version and notes").
  - `concurrency: { group: release, cancel-in-progress: false }`.
  - Top-level `permissions: contents: read`.
- [ ] **Step 2: The `version` job.**
  - Fail with `::error::Run the release from main.` unless `github.ref == 'refs/heads/main'`.
  - Check out with `fetch-depth: 0`.
  - Run git-cliff-action with `args: --unreleased --bump --strip all` and `config: cliff.toml`.
  - Get the latest tag with `git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true`.
  - Set the outputs `version`, `notes` (the action's `content`, through a heredoc delimiter) and `release` (true when the version differs from the latest tag and this isn't a dry run).
  - Write the version and notes to `$GITHUB_STEP_SUMMARY`, or "Nothing to release since <tag>."
- [ ] **Step 3: The later jobs.**
  - `ci`: `needs: version`, `if: needs.version.outputs.release == 'true'`, calls `ci.yml`.
  - `publish`: `needs: [version, ci]`, the same `if`, calls `publish.yml` with `version` and `release: true`.
  - `release`: `needs: [version, publish]`, the same `if`, `permissions: contents: write`. It writes the notes to a file and runs `gh release create "$VERSION" --target "$GITHUB_SHA" --title "$VERSION" --notes-file notes.md`, with `GH_TOKEN: ${{ github.token }}`.
- [ ] **Step 4: Run actionlint.** Expected: no findings.
- [ ] **Step 5: Commit** with `ci: add a release workflow that versions from conventional commits`.

### Task 4: docs

**Files:** Modify `docs/ci-and-releases.md`.

- [ ] **Step 1: Rewrite "Images and releases".**
  - Explain how to run Release from the Actions tab, and the dry run.
  - Give the version rules and say they live in `cliff.toml`.
  - Say the tag and the GitHub release come after the images.
  - Say a tag pushed by hand still publishes through `cd.yml`, which is how to go to 1.0.
  - Say a tag made by the workflow doesn't trigger `cd.yml`, which is why the release workflow publishes the images itself.
- [ ] **Step 2: Humanize the new prose**, then commit with `docs: explain the release workflow`.
- [ ] **Step 3: Open the PR.** The description notes that `release.yml` and the `cd.yml` refactor can only be exercised on `main`. After the merge, check the CD run on `main` (only the `main` image tag), then do a Release dry run, which should report `v0.2.0`.
