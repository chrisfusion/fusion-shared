# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/); versions follow
[Semantic Versioning](https://semver.org/). Tags are created for Go code changes only.

## [Unreleased]

## [0.1.0] — 2026-10-06

First release: the `ownership` package (owner-group scoping for fusion services).

### Added
- Go module `github.com/chrisfusion/fusion-shared` (Go 1.25).
- `Makefile` with Docker-based `test`, `vet`, `fmt-check` (own sources only) and `bench` targets.
- `ownership` names: `ValidGroupName` and `NormalizePersonalName` (1-63 chars, `a-z0-9._-`, alphanumeric at both ends, ASCII-only lower-casing so look-alike characters can never collapse into an ASCII name).
- `ownership` headers: trusted header constants, `ParseHeaders` (narrowing parse: invalid, duplicate and surplus groups dropped, all-groups flag only for exact `true`), `StripHeaders`, `FormatHeaders` (replaces client values, rejects unsafe values without partial writes) and `CapGroups` (deterministic cap of 100 groups, priority groups first).
- `ownership` config: `Config`/`PrincipalEntry`, `Load`/`Parse` (strict YAML: unknown keys, empty file and multiple documents are errors) and `Lookup`. Principal modes: `trustedProxy`, `allGroups`, `groups` + `writableGroups` (subset), `assertableGroups` (read-only, for job pods). All problems are reported together; `Validate(authEnabled)` returns `ErrAuthRequired` when enforcement is on and authentication is off. Dependency: `gopkg.in/yaml.v3`.
- `ownership` scope: `Scope` (zero value sees nothing) with `CanRead`/`CanWrite`/`CanCreate`/`CanMove`, `CheckRead`/`CheckWrite`/`CheckMove` (invisible → `ErrNotVisible` 404, read-only → `ErrForbidden` 403), `ResolveCreateOwner` (users fall back to their default group, service callers must name the group), `IsEmpty`, `Sees`, `HTTPStatus`. Unowned resources are visible only to an all-groups scope.
- `ownership` resolver: `NewResolver`, `Resolve(principal, headers)` with `WithLogger`. Unknown principals get an empty scope; trusted proxies take the user's scope from the headers (write narrowed to read, no headers = empty scope); configured principals ignore headers; assertable principals (job pods) get the asserted read scope limited to their configured groups and never write; with enforcement off every scope passes all checks but creates still record an owner (requested, default, else legacy group). Log output carries counts only, never group names.
- `ownership` queries: `SQLClause` (`TRUE` / `FALSE` / `col = ANY($n)` with one `[]string` argument; NULL owners never match), `LabelSelector` (`key in (a,b)` with `SelectorKind` All / None / Labels / InMemory, 4096-byte limit before falling back to in-memory), generic `Filter`, and the shared names `LabelOwnerGroup` (`fusion-platform.io/owner-group`), `ColumnOwnerGroup` (`owner_group`), `OwnerFromLabels`, `WithOwnerLabel`. Column and label key are constants by contract: malformed ones panic so a mistake shows in the first test run.
- `ownership` HTTP: `Resolver.Attach` (clones the request, stores the scope in its context, strips the trusted headers), `Resolver.Middleware` (net/http, chi), `WithErrorHandler`, `FromContext`/`WithScope`, `WriteError`/`ErrorMessage` (fixed messages, nothing echoed), `ErrUnknownPrincipal` (403 when enforcing and the caller has no config entry). `ownership/ginmw`: `Middleware`, `Scope`, `Abort` (Gin v1.9.1, the lowest version used by the services).
- `Scope.Email` and `Scope.Headers()` so proxies (weave, wizard) can pass the user's scope on with `FormatHeaders`; an unenforced trusted proxy keeps the forwarded scope for the same reason.
- Package documentation (`doc.go`), runnable examples and a README quick start.
- `make fuzz` and `make tidy` targets; fuzz tests for names, header parsing, config parsing and the resolver, benchmarks for `ParseHeaders`, `CanRead`, `SQLClause`, `LabelSelector` and `Filter` (50,000 items, 20 groups).
- Benchmarks on the reference machine (AMD Ryzen 7 9800X3D, Go 1.25): `ParseHeaders` 1.9 µs (20 groups) / 7.7 µs (100); `SQLClause` 65 ns; `LabelSelector` 102 ns (20) / 374 ns (100); `Filter` over 50,000 items with a 20-group scope 0.53 ms; `CanRead` 33 ns, no allocations. All far below the 300 ms list target (W35).
- Statement coverage of both packages is 100% (this is not branch coverage).
- Cross-project documents in `docs/`: multi-tenancy plan, ownership package plan, logging principles.

### Security
- Job pods that assert a read scope (`assertableGroups`) cannot claim a user identity: the asserted `X-User-ID` is ignored.
