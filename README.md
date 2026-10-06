# fusion-shared

Shared Go packages and cross-project documentation for the fusion platform.

- **Module:** `github.com/chrisfusion/fusion-shared`
- **License:** GPL-3.0 (see [LICENSE](LICENSE))

## Contents

| Path | Purpose |
|---|---|
| `docs/multi-tenancy.md` | Ownership / owner-group plan shared by all fusion services: requirements, design, rollout |
| `docs/logging_principles.md` | Logging rules for all fusion services |
| `ownership/` | _Planned._ Scope, trusted-header parsing and SQL / label-selector builders for owner-group enforcement |

## Usage

Services consume the module as a normal dependency and vendor it:

```bash
go get github.com/chrisfusion/fusion-shared@<tag>
make vendor   # commit vendor/ together with go.mod and go.sum
```

Do not commit a `replace` directive. For local co-development use a `go.work` file (gitignored).

## Versioning

Semantic versioning via git tags. Tags are created for Go code changes only; documentation-only changes are not tagged. The API is `v0.x` until all services have adopted it.

## Contributing

Keep libraries generic (no service-specific logic), write code and comments in English, and add tests. Do not commit internal hostnames or credentials; this repository is public.
