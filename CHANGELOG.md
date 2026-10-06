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
- `make fuzz` and `make tidy` targets; fuzz tests for names, header parsing and config parsing, benchmark for `ParseHeaders`.
- Cross-project documents in `docs/`: multi-tenancy plan, ownership package plan, logging principles.
