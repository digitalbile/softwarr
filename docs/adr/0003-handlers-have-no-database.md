# 0003. HTTP handlers have no database handle and go through services

- Status: Accepted
- Date: 2026-11-25
- Links: #12

## Context

Most handlers in `internal/httpapi` called a domain service, but the download client, path mapping and search handlers queried `dbq` directly. Their validation and rules lived in the HTTP layer, where only HTTP tests could reach them. With both patterns in the code, new contributors would copy whichever they saw first.

## Decision

Handlers parse the request, call a service, and map its errors to status codes. They never query the database. `httpapi.Deps` doesn't carry the `*sql.DB` or `*dbq.Queries`, so a handler that tries won't compile.

A new resource gets a service or store in its domain package, constructed in `cmd/softwarr/main.go` and passed in through `Deps`. The domain package owns the JSON view type, the input type, the validation, and the sentinel errors, as `notify.Webhook`, `downloadclient.Store` and `pathmap.Store` do.

## Consequences

- Rules about what can change are in one place, and Go tests can cover them without HTTP.
- Every new resource needs a service type and a `Deps` field, even for plain CRUD.
- HTTP tests that need the database get it from the test server helper, which passes the queries to the hook in `newTestServerWith`.
