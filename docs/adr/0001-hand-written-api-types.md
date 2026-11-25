# 0001. The frontend API types are written by hand, and a Go test checks their field names

- Status: Accepted
- Date: 2026-11-25
- Links: #12

## Context

`web/src/api/types.ts` mirrors the JSON the Go API sends. Nothing checked the two against each other, so a field added, renamed or removed on one side would go unnoticed until the UI broke.

Generating the TypeScript from the Go structs (with tygo or from an OpenAPI spec) was the obvious fix. Two things count against it here:

- The hand-written types are more precise than generated ones would be. `RequestStatus`, `SearchStatus` and `HealthLevel` are string unions. A generator turns the Go constants into a plain `string` type with separate constants.
- The Go types live in about 15 packages under names that differ from the UI's, such as `downloadclient.Saved` for `DownloadClient`. Generating would rename types across about 20 UI files or need a mapping layer, and it would add a tool and a CI step.

The common drift is in field names. Field types change less often, and TypeScript catches most type mistakes where the data is used.

## Decision

`types.ts` stays hand-written. `internal/httpapi/apitypes_test.go` maps each interface in it to the Go type the API encodes and fails when:

- their JSON field names differ, after flattening embedded structs and following `extends`
- `types.ts` declares an interface that has no entry in the map
- the map names an interface that `types.ts` doesn't declare

## Consequences

- A change to a response has to update `types.ts` in the same pull request, or CI fails.
- A new interface needs an entry in `apiTypes`.
- Field types and optionality aren't checked. A Go field that becomes a pointer, or a TypeScript field that wrongly becomes optional, still passes.
- If the API grows enough that keeping the map up to date costs more than generating would, revisit this.
