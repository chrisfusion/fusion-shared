# Plan: package `ownership` (fusion-shared)

Step 1 of the rollout in `multi-tenancy.md` (section 5). Status: **steps 1-8 implemented and reviewed; tag `v0.1.0` pending the owner's OK.** Wish numbers (W..) refer to `multi-tenancy.md`.

## 1. Scope

- Pure logic: no authentication, no storage access, no Kubernetes calls. Services keep authenticating callers as today; the package turns "who is calling, with which headers" into decisions.
- Covers W11 (names), W13/W16/W24 (scope, headers), W17 (query builders), W18/W32/W36/W38 (service-account config), W20 (enforcement switch), W35 (benchmarks).

## 2. Findings that shape the design

- Frameworks differ: weave and wizard use `chi`, the BFF, forge, index and ext-system-bff use Gin. The core is framework-neutral (`net/http`) with a thin Gin adapter.
- All services already vendor `gopkg.in/yaml.v3` (config format). The core needs no pgx: the SQL builder returns a string and arguments.
- All services use Go 1.25.

## 3. Layout (package at the repo root)

| File | Content |
|---|---|
| `names.go` | `ValidGroupName`, `NormalizePersonalName` (W11: lower-case, 1-63 chars, `a-z0-9._-`, alphanumeric at both ends) |
| `headers.go` | header constants, `ParseHeaders`, `FormatHeaders` (BFF sets them, services read them), cap of 100 groups |
| `config.go` | `Config`, service-account entries (`trustedProxy`, `groups`, `writableGroups`, `allGroups`, `assertableGroups`), `Load(path)`, `Validate(authEnabled)` |
| `scope.go` | `Scope` and its decisions |
| `resolve.go` | `Resolver`: principal + request headers → `Scope` |
| `query.go` | `SQLClause`, `LabelSelector`, in-memory filter, label key constant |
| `http.go` | `net/http` middleware: scope into the context, strips `X-User-*` from the request |
| `ginmw/` | Gin adapter |
| `doc.go` | package documentation with examples |

## 4. API sketch

```go
type Scope struct { UserID string; Read, Write []string; All bool; DefaultGroup string; Service bool }
func (s Scope) CanRead(owner string) bool
func (s Scope) CanWrite(owner string) bool
func (s Scope) CanCreate(owner string) bool
func (s Scope) CanMove(from, to string) bool
func (s Scope) ResolveCreateOwner(requested string) (string, error)

func (r *Resolver) Resolve(principal string, h http.Header) (Scope, error)
func SQLClause(s Scope, col string, argPos int) (clause string, args []any)
func LabelSelector(s Scope, key string) (sel string, kind SelectorKind) // All | None | Selector
```

When a selector would be too long, the caller filters in memory with `CanRead`.

## 5. Behaviour (locked in by tests)

- Fail closed: unknown principal → empty scope; trusted proxy without headers → empty scope (not an error); empty owner (unmigrated resource) visible only with `All` (W20).
- Headers honoured only from `trustedProxy` principals. `X-User-All-Groups` counts only if exactly `true`. Invalid group names are dropped (only reduces access).
- Write is a subset of read, enforced by intersection and when loading config (bad file rejected).
- Pods (W36): a principal with `assertableGroups` gets the asserted groups intersected with that list, read only, never write.
- Create: a user's missing owner falls back to `DefaultGroup`; a service account must name the group explicitly (`ErrOwnerRequired`).
- Move: write on the source and write on the target, or `All`.
- Enforcement switch (W20): with `enforce=false` the scope behaves as all-groups for visibility, creates still record an owner (requested, else default, else legacy group).
- Fail fast (W38): `Validate(authEnabled)` errors when enforcement is on and authentication is off.
- Errors: `ErrNotVisible` → 404, `ErrForbidden` → 403 (Q23); helper `HTTPStatus(err)`.
- Logging: `log/slog` only, optional logger in the config (see `logging_principles.md`).

## 6. Tests and checks

- Table-driven unit tests for every rule above, config-file error cases.
- Fuzz test for header parsing; property test: write ⊆ read.
- Benchmarks (W35): SQL clause, label selector with 100 groups, header parsing, `CanRead`. Scopes are immutable with a map built once (O(1) lookups).
- Docker (global rule): `make test` runs `go vet` and `go test -race ./...` in `golang:1.25`. The library itself is not vendored; services vendor it (W21).

## 7. Work steps (each ends with a commit the owner approves)

1. Go module, Makefile and Docker test target, `CHANGELOG.md`.
2. `names.go` and `headers.go` with tests.
3. `config.go` with tests.
4. `scope.go` and `resolve.go` with tests.
5. `query.go` with tests and benchmarks.
6. `http.go` and `ginmw/` with tests.
7. `doc.go`, README usage section, benchmark results in the changelog.
8. Review, then tag `v0.1.0` (tag and push need the owner's OK).

## 8. Confirmed decisions

1. Gin adapter lives in the same module (`ownership/ginmw`).
2. The service passes the principal in as an opaque string (e.g. weave's `sa/system:serviceaccount:<ns>:<name>`); the package does not authenticate.
3. Config is loaded once at startup; a changed ConfigMap needs a pod restart, no file watching.
4. A trusted proxy without headers gets an empty scope, not an error.
5. This plan is kept as a file in `docs/`.
6. (Step 3) The config list is called `principals`, not `serviceAccounts`: it also holds API keys and OIDC subjects (W38). Entry modes are exclusive: `trustedProxy`, `allGroups`, `groups` (+ `writableGroups`) or `assertableGroups`.

## 9. Implementation notes (deviations from the sketch above)

- Config key `principals` (decision 6). `SQLClause` and `LabelSelector` panic on a malformed column or label key (constants by contract) instead of returning an error.
- Added: `Scope.Email`, `Scope.Headers()` (proxies forward the user's scope), `Resolver.Attach` (shared by the net/http and Gin middleware), `ErrUnknownPrincipal` (403 while enforcing for callers without a config entry), `Scope.Sees()`, `Filter`, label/column constants and helpers, `make fuzz` / `make tidy`.
- Review findings fixed before v0.1.0: pods with `assertableGroups` can no longer claim a user id; an unenforced trusted proxy keeps the forwarded scope so it can pass it on while upstream services already enforce.
