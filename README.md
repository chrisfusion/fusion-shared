# fusion-shared

Shared Go packages and cross-project documentation for the fusion platform.

- **Module:** `github.com/chrisfusion/fusion-shared`
- **License:** GPL-3.0 (see [LICENSE](LICENSE))

## Contents

| Path | Purpose |
|---|---|
| `docs/multi-tenancy.md` | Ownership / owner-group plan shared by all fusion services: requirements, design, rollout |
| `docs/logging_principles.md` | Logging rules for all fusion services |
| `ownership/` | Owner-group scoping: names, trusted headers, scope decisions, config, resolver, SQL / label-selector builders, `net/http` middleware |
| `ownership/ginmw/` | Gin adapter for the `ownership` middleware |

## Usage

Services consume the module as a normal dependency and vendor it:

```bash
go get github.com/chrisfusion/fusion-shared@<tag>
make vendor   # commit vendor/ together with go.mod and go.sum
```

Do not commit a `replace` directive. For local co-development use a `go.work` file (gitignored).

## Quick start: `ownership`

```go
cfg, err := ownership.Load("/etc/fusion/ownership.yaml")
if err == nil { err = cfg.Validate(authEnabled) } // refuses enforcement without authentication
res, err := ownership.NewResolver(cfg, ownership.WithLogger(logger))

// after the service's own authentication (net/http or chi):
r.Use(res.Middleware(func(r *http.Request) string { return principalOf(r) }))
// or with Gin:  router.Use(ginmw.Middleware(res, func(c *gin.Context) string { return principalOf(c) }))

scope, _ := ownership.FromContext(req.Context()) // ginmw.Scope(c) with Gin

clause, args := ownership.SQLClause(scope, ownership.ColumnOwnerGroup, 1) // list: WHERE (clause)
err = scope.CheckWrite(owner)                                              // update / delete
owner, err := scope.ResolveCreateOwner(requested)                          // create
err = scope.CheckMove(from, to)                                            // move owner group
```

Map errors with `ownership.HTTPStatus(err)` and `ownership.ErrorMessage(err)`. The package documentation (`go doc ./ownership`) and the runnable examples describe every function; the rules are in [docs/multi-tenancy.md](docs/multi-tenancy.md).

## Development

`make test` (gofmt check, `go vet`, race tests), `make fuzz`, `make bench` and `make tidy` all run inside the `golang:1.25` Docker image.

## Versioning

Semantic versioning via git tags. Tags are created for Go code changes only; documentation-only changes are not tagged. The API is `v0.x` until all services have adopted it.

## Contributing

Keep libraries generic (no service-specific logic), write code and comments in English, and add tests. Do not commit internal hostnames or credentials; this repository is public.
