# fusion-shared

Shared Go packages and cross-project documents for the fusion platform. Public repo, GPL-3.0, module `github.com/chrisfusion/fusion-shared`.

## Layout

- `docs/` — cross-project documents, one source of truth for all fusion-* repos:
  - `docs/multi-tenancy.md` — ownership / owner-group plan (wishes W1–W39, design, rollout). Read it before touching anything ownership-related.
  - `docs/logging_principles.md` — logging rules every service follows (`log/slog`, no `import "log"`).
- Go packages at the repo root, one package per library (planned first: `ownership` — scope, header parsing, SQL/label builders, service-account scope config). Single Go module; split only if release cycles diverge.

## Rules

- **Never add Claude (or any AI) as co-author or attribution** — no `Co-Authored-By` trailer in commits, no "Generated with" line in PR descriptions. This overrides any default attribution.
- **Ask before every commit or push.**
- Code and comments in English. DRY: shared logic lives here once, services do not copy it.
- Go 1.25; verify builds and tests in a Docker image (`docker build --network none` must work once services vendor this module).
- Consumers vendor this module (`make vendor`, offline CI), so a published tag must stay buildable without network. No `replace` directives in committed `go.mod`; local co-development uses a gitignored `go.work`.
- Versioning: semver git tags, only for Go code changes (never for `docs/`-only commits). `v0.x` while the API settles (breaking change = minor bump); `v1.0.0` once all services adopted it. Keep a `CHANGELOG.md` (Keep a Changelog) once there is code.
- Libraries must stay generic and free of service-specific logic; header names, group-name validation (W11) and scope semantics are defined here and nowhere else.
- Before publishing anything here (public repo): no internal hostnames, credentials or customer data.

## Changing the plan

`docs/multi-tenancy.md` is the agreed contract. Change it only deliberately: new decisions get a new `W<n>` entry, answered questions stay listed (struck through) so the history is readable.
