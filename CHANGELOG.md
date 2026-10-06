# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/); versions follow
[Semantic Versioning](https://semver.org/). Tags are created for Go code changes only.

## [Unreleased]

### Added
- Go module `github.com/chrisfusion/fusion-shared` (Go 1.25) with the empty `ownership` package.
- `Makefile` with Docker-based `test`, `vet`, `fmt-check` (own sources only) and `bench` targets.
- `ownership` names: `ValidGroupName` and `NormalizePersonalName` (1-63 chars, `a-z0-9._-`, alphanumeric at both ends, ASCII-only lower-casing so look-alike characters can never collapse into an ASCII name).
- `ownership` headers: trusted header constants, `ParseHeaders` (narrowing parse: invalid, duplicate and surplus groups dropped, all-groups flag only for exact `true`), `StripHeaders`, `FormatHeaders` (replaces client values, rejects unsafe values without partial writes) and `CapGroups` (deterministic cap of 100 groups, priority groups first).
- `ownership` config: `Config`/`PrincipalEntry`, `Load`/`Parse` (strict YAML: unknown keys, empty file and multiple documents are errors) and `Lookup`. Principal modes: `trustedProxy`, `allGroups`, `groups` + `writableGroups` (subset), `assertableGroups` (read-only, for job pods). All problems are reported together; `Validate(authEnabled)` returns `ErrAuthRequired` when enforcement is on and authentication is off. Dependency: `gopkg.in/yaml.v3`.
- `ownership` scope: `Scope` (zero value sees nothing) with `CanRead`/`CanWrite`/`CanCreate`/`CanMove`, `CheckRead`/`CheckWrite`/`CheckMove` (invisible → `ErrNotVisible` 404, read-only → `ErrForbidden` 403), `ResolveCreateOwner` (users fall back to their default group, service callers must name the group), `IsEmpty`, `Sees`, `HTTPStatus`. Unowned resources are visible only to an all-groups scope.
- `ownership` resolver: `NewResolver`, `Resolve(principal, headers)` with `WithLogger`. Unknown principals get an empty scope; trusted proxies take the user's scope from the headers (write narrowed to read, no headers = empty scope); configured principals ignore headers; assertable principals (job pods) get the asserted read scope limited to their configured groups and never write; with enforcement off every scope passes all checks but creates still record an owner (requested, default, else legacy group). Log output carries counts only, never group names.
- `make fuzz` and `make tidy` targets; fuzz tests for names, header parsing, config parsing and the resolver, benchmarks for `ParseHeaders` and `CanRead`.
- Cross-project documents in `docs/`: multi-tenancy plan, ownership package plan, logging principles.
