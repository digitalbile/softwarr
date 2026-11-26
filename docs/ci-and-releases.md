# CI and releases

## CI

`.github/workflows/ci.yml` runs on every pull request. Its jobs run in parallel:

- `go`: `go vet`, `go test`, `go mod tidy -diff`, and `sqlc diff`
- `lint`: golangci-lint
- `web`: Biome, the UI tests, and the UI build
- `smoke`: `make smoke`, which builds the Docker image and checks that it starts

The `check` job passes only when the others do, so branch protection needs to require only `check`. To add a job, list it in `check`'s `needs`. To let one fail or skip without failing `check`, list it in `allowed-failures` or `allowed-skips`.

Every `uses:` is pinned to a full commit SHA with its version in a comment, and base images are pinned by digest.

## Dependency updates

Renovate opens pull requests for dependency updates and keeps the pins current. It merges digest, patch, and minor updates once CI passes. Major updates wait for a review. Renovate also updates the golangci-lint version in `ci.yml`, through the `# renovate:` comment above it.

## Images and releases

`.github/workflows/cd.yml` runs CI again on each push to `main`, then publishes the `main` image. The image build itself is in `publish.yml`, which `cd.yml` and `release.yml` both call.

To cut a release, open the Actions tab, pick Release, and run it on `main`. It works out the next version from the conventional commits since the last tag, runs CI, publishes `X.Y.Z`, `X.Y` and `latest` for amd64 and arm64, and then creates the tag and a GitHub release with notes. The tag comes last, so a failed build leaves no release. Tick "Only show the version and notes" for a dry run, which writes both to the run summary and stops there.

[git-cliff](https://git-cliff.org) works out the version, using the rules in `cliff.toml`:

- `feat` bumps the minor version.
- `fix` and `perf` bump the patch, including Renovate's `fix(deps)` updates.
- A breaking change (`feat!:` or a `BREAKING CHANGE:` footer) bumps the major version, or the minor version while the version is below 1.0.
- Other types don't need a release. If nothing since the last tag bumps the version, the run ends green without tagging.

The release notes list the breaking changes, features, fixes and performance commits, grouped by type.

A tag that the Release workflow creates doesn't start `cd.yml`, because GitHub doesn't start workflows from events made with `GITHUB_TOKEN`. That's why `release.yml` publishes the images itself. A tag pushed by hand still goes through `cd.yml` and publishes the same image tags. That's how to go to 1.0: push `v1.0.0` on a commit in `main` once its CD run has passed.
