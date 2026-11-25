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

`.github/workflows/cd.yml` runs CI again on each push to `main`, then publishes the `main` image.

To publish a release, push a version tag such as `v0.1.0` on a commit in `main`. That publishes `0.1.0`, `0.1`, and `latest`, for amd64 and arm64. Wait for the CD run on `main` to pass before tagging.
