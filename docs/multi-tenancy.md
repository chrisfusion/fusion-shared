# Multi-tenancy / Ownership — Fusion Platform

Shared across all fusion-* repos. One understanding, one source of truth.
Each repo's `CLAUDE.md` points here; implementation details stay in the repo.

Status: **Wishes W1–W35 agreed. Design (section 4) and rollout (section 5) drafted; Q1–Q25, Q27–Q30, Q32–Q34 answered; open: Q31 (housekeeping, in progress).**

## 1. Wishes (requirements)

Written by the owner, in their own words. No design decisions in this section.

Model: "like Linux".

- W1. Every resource has exactly one owner group.
- W2. A user belongs to one or many groups.
- W3. Every user always has a default group, named like the user.
- W4. A user can only see resources owned by one of their groups.
- W5. A user's permissions (roles) are global: they apply to all groups the user is in.
- W6. The personal group replaces the shared `default` group (no shared group everyone is in).
- W7. Admins see everything (like root), regardless of group membership.
- W8. The personal group is auto-created on the user's first login (not provisioned manually, not via IdP groups).
- W9. The OIDC field that names the personal group is configurable. The operator is responsible for choosing a field that is unique per user.
- W10. Collision rule (agreed):
  - The personal group is bound to the user's stable `user_id` when first created.
  - If the field value is already used by a group bound to a different `user_id`, or equals the name of a non-personal owner group, the login is rejected (403, clear error, logged with both ids). No suffixing, no sharing.
  - If the same `user_id` logs in again and the field value changed, the existing group name is kept.
  - If the field is missing or empty in the token, the login is rejected.
- W11. Personal group names are lower-case, 1-63 characters, limited to `a-z 0-9 . _ -`, and must start and end with `a-z` or `0-9` (compatible with Kubernetes label values). The field value is lower-cased; if it still violates these rules the login is rejected (like a collision), never silently altered or truncated.
- W12. Sharing with everyone uses an ordinary owner group (e.g. `shared`) that the operator maps for everyone via the reserved wildcard mapping `*` in `owner_group_oidc_mappings` (the IdP has no all-users group and cannot be changed; see W33). No built-in public group and no per-resource public flag.
- W13. A dedicated permission (name fixed in design, e.g. `admin:all-groups:access`) grants read, write and delete on resources of every owner group, regardless of membership. Granted to the `admin` role by default but independent of it, so an RBAC admin without it cannot see other teams' resources. Refines W7.
- W14. Creating a resource (Linux-style): the creator picks the owner group from their own groups. If none is given, the preferred owner group is used, otherwise the personal group. A group the user is not a member of is rejected (403). Holders of the all-groups permission (W13) may name any group. A resource never exists without an owner group.
- W15. Changing the owner group (like `chgrp`): allowed for a user who can write the resource and is a member of the target group. Holders of the all-groups permission may move any resource to any group. It is an explicit action with its own endpoint and permission (e.g. `<service>:owner:change`), not a field of a normal update, and is audit-logged with old and new group. The GUI should warn that the user may lose access.
- W16. Visibility and ownership are enforced in each service (not in the BFF). The BFF resolves identity, owner groups and the all-groups permission and forwards them as trusted headers (`X-User-Groups`, plus a new header for W13). Services trust them only from the BFF service account. The shared enforcement logic lives in the shared repo `fusion-shared` (W21), designed to host further shared libraries later.
- W17. List endpoints never check resources one by one: the caller's scope (groups, or all-groups) is pushed into the storage query (SQL `owner_group = ANY(...)`, or a Kubernetes label selector / informer index on the owner label), before pagination. Where the selector would become too long, filter in memory over the cached list. The shared library provides the scope type and the query/selector builders.
- W18. Service-account callers (pod-to-pod) are explicit principals:
  - Each service config maps an allowed service account to a scope: a list of owner groups, or all-groups. An account without an entry gets nothing. All-groups for a service account is granted explicitly and rarely.
  - `X-User-Groups` and the all-groups header are honoured only from accounts marked as trusted proxy in the service config (BFF; possibly weave). From any other account they are stripped and ignored.
  - A service account creating a resource must name the owner group explicitly, within its scope; there is no personal-group fallback.
  - The shared library resolves user and service-account callers into the same scope object, so handlers never branch on caller type.
- W19. The personal group is persisted and has a lifecycle:
  - It is a row in `owner_groups` with a kind (`personal`/`regular`) and the bound `user_id` (unique), created on first login and never renamed. The user's membership is implicit, derived from the bound `user_id`.
  - A personal group has exactly one member. The admin API refuses OIDC mappings and direct members on it; sharing goes through regular groups (W12). Kind is part of the collision check (W10), also when an admin creates a regular group.
  - When a user leaves nothing happens automatically (the BFF cannot see IdP removals, and deleting would lose data). Admins with the all-groups permission (W13) list the group's resources, move them with the explicit move action (W15), then mark the group retired. A retired group's name stays reserved; the same `user_id` returning gets the group back unchanged.
- W20. Migration of existing resources:
  - All resources without an owner are assigned to a configured legacy group, default the shared group of W12 (e.g. `shared`). Everyone keeps seeing what they see today; teams and admins later move resources to the right group (W15).
  - Idempotent, per service: a SQL migration for Postgres-backed services, a one-off job setting the owner label for CRD-backed ones (weave).
  - Fail closed: a resource without an owner is visible only to holders of the all-groups permission (W13), never public.
  - Enforcement is behind a per-service switch that is enabled only after that service's migration has run. Rollout order: migrate, then enable.
  - Creator-based assignment (personal group of `creator_id`) is out of scope for now.
- W21. Shared repo `fusion-shared` (renamed from `fusion-shared`; holds shared Go packages and cross-project docs):
  - A new repo on GitHub, created by hand by the owner. Public repo (exists, created by the owner), module path `github.com/chrisfusion/fusion-shared` (so no `GOPRIVATE`/credentials needed). Single Go module; each library is a package at the repo root (`ownership` first: scope, header parsing, SQL/label builders). `docs/` holds cross-project documents: this plan as `docs/multi-tenancy.md`; other shared documents may follow. Services point to `../fusion-shared/docs/...` from their `CLAUDE.md`. Version tags only on Go code changes. Split into modules only if release cycles diverge. GPL-3, built and tested in Docker, own `CHANGELOG.md`.
  - Consumed as a normal `require`, vendored and committed in each service (offline CI unchanged). No committed `replace`; local co-development via a gitignored `go.work`.
  - Semver git tags; `v0.x` while the API settles (breaking change = minor bump), `v1.0.0` once all services adopted it.
  - Update rollout per service: bump version, `make vendor`, commit; `make check-vendor` catches drift. A helper script in `fusion-shared/scripts/` may loop over services (the local parent directory is not a remote repo and is not used).
- W22. Cross-resource references:
  - Checked at write time only (create/update of the referencing resource), never on read/list; no cache or central registry. The referenced resource must be visible in the caller's scope. Existing references are not re-validated at runtime and are not broken by a move (W15).
  - Same-service references are checked locally; cross-service references (weave→index artifact, wizard→forge/index/weave) are checked from the start.
  - fusion-weave (API server) and fusion-wizard are trusted proxies (W18): they forward the caller's `X-User-ID`, `X-User-Groups` and the all-groups header when calling forge/index/weave, and the target services list them as trusted proxy in their config.
  - Limits: the weave operator reconciles without a user, so the check must happen in the weave API server at create time; CRs created directly with kubectl bypass it (Kubernetes RBAC concern).
- W23. Wizard starts with a group chosen by the user (W14: one of the user's own groups; default preferred group, else personal group). That group owns the WizardRun and every resource the wizard creates. While running in the background the wizard forwards only that group as scope, so references (W22) must be visible to that group (owned by it or by the shared group).
- W24. Group mode: each owner group is `rw` (default) or `ro`.
  - Members of an `ro` group can see, use and reference its resources; only holders of the all-groups permission (W13) can create, change or delete them. The operator sets the shared group (W12) to `ro`, so admins maintain common resources.
  - The BFF forwards two lists: groups the user can see (`X-User-Groups`) and the subset the user can write to (new header, e.g. `X-User-Writable-Groups`). The library scope object carries both.
  - Creating a resource in a group (W14) and moving one into a group (W15) require write scope on the target group. Personal groups are always `rw`.
  - One new column on `owner_groups`; no per-group roles (W5 stays: roles are global).
- W25. OIDC group mapping admin (BFF). All users log in through the same OIDC client, so every token carries the same claim fields; mappings therefore key on claim values (group names), never on individual users.
- W26. Seen OIDC groups: the BFF records at login which user carries which OIDC group (new table: oidc group, `user_id`, first seen, last seen). The admin GUI gets real distinct-user counts per OIDC group and can list the affected users, to pick groups and to spot unmapped ones. Replaces the earlier counter-only variant.
  - Data protection: the table stores only the `user_id` claim value and timestamps (no email/name). Counts include users seen within a configurable window (default 90 days). Records older than a configurable retention (default 365 days) are deleted. Removing a user's data also removes their records.
- W27. Combined overview endpoint, e.g. `GET /bff/admin/owner-group-overview`: each owner group with kind, mode (W24), OIDC mappings and direct members, plus the seen OIDC groups with their mapping state, in one call.
- W28. The mapping admin API also supports update (PATCH) and offers search/filter and pagination on its lists.
- W29. Dry-run endpoint, e.g. `GET /bff/admin/owner-group-resolve?email=...`: shows which owner groups a user would get (mappings, direct membership, personal group) without changing anything.
- W30. OIDC group names are normalised (leading `/` stripped, as Keycloak sends them) before storing and matching, so the same group cannot be entered twice.
- W31. Seed data (BFF Helm values, applied by an extended seed job; idempotent):
  - Shared group name (default `shared`), set to mode `ro` (W24), also the legacy group for migration (W20): configurable via Helm.
  - The wildcard mapping `*` → shared group (W33): the source and target are configurable via Helm.
  - The existing admin seed (`rbacSeed.adminGroup` → role `admin`) stays; `DEFAULT_OWNER_GROUP` is removed or repurposed as the shared/legacy group name (W6).
  - New permissions and route rules in `rbac.yaml` and `deployment/rbac.yaml`: all-groups (W13) to `admin`, `<service>:owner:change` (W15) to `admin` and `engineer`.
  - Not seeded: personal groups (created at first login, W8) and memberships (come from OIDC mappings).
- W32. Per-service service-account scope config (W18) via Helm. Today forge, index and wizard only have a flat `AUTH_ALLOWED_SA` list of `namespace/name` (Helm `allowedServiceAccounts`); forge/index treat an empty list as "any valid SA token". Needed: a structured list (mounted as a file, not CSV) with per entry `name`, `trustedProxy`, `groups`, `writableGroups` or `allGroups`, plus `ownership.enforce` and `ownership.legacyGroup`. Once enforcement is on, an account without an entry gets nothing, so an empty list must not mean "any account". Weave's current SA config still to be checked.
- W33. Wildcard mapping: a reserved OIDC group `*` in `owner_group_oidc_mappings` means every authenticated user who passed login and allowlist. Seeded via Helm (default `*` → `shared`), visible and editable in the admin overview and dry-run like any mapping (W25-W30); services only see the resulting group in `X-User-Groups`. `*` is reserved and exempt from normalisation (W30). Replaces an IdP-side all-users group, which does not exist.
- W34. Performance under load: with thousands of resources per service and heavy usage the system must keep responding properly. Everything in this plan is built and tested against load targets (see Q20). Known hot spots to design for:
  - Login: upsert of per-user OIDC group records (W26) must be batched and `last seen` writes throttled; login bursts must not exhaust the DB pool.
  - Bearer path resolves groups per request: needs a short-TTL cache, no DB round-trip per call.
  - Lists (W17): index on the owner column; label selector / informer index for CRDs; keyset pagination; filter before pagination.
  - Header size: `X-User-Groups` and the writable list (W24) for users in many groups must stay within header limits; define a cap and behaviour above it.
  - Write-time reference checks (W22): one extra cross-service call per reference; wizard creates many resources, so use timeouts, parallel calls and no per-item chatter.
  - Admin overview (W27): distinct-user counts over per-user records (W26) need indexes, pagination and cheap aggregation; retention cleanup runs in small batches.
  - Migration (W20): batched updates without long table locks; CRD relabelling in batches.
- W35. Load targets (design and test baseline, adjustable later):
  - 50,000 resources per service, 500 users, 20 groups per typical user.
  - p95 under 300 ms for a list call at those sizes.
  - Added by Claude, not yet confirmed: 100 concurrent users at peak, and a stress case of one user with 100 groups (tests the header cap and selector length).
  - Tests: the shared library gets benchmarks for the scope/query builders; each service gets a load test with the seeded volume; the BFF gets a login-burst test.
- W36. Job pods (runner, code-loader init container) download artifacts from fusion-index with their pod service-account token and forward the run's owner group:
  - Weave injects the run's owner group into the pod (e.g. env `WEAVE_OWNER_GROUP`); runner and code-loader send the pod's SA token plus that group as read scope (no write scope).
  - fusion-index lists the pod service account(s) in its ownership config with an `assertableGroups` limit: the group a pod asserts is honoured only if it is in that list. Unlike a trusted proxy (W18), which may forward any scope.
  - Limit: protection is only as strong as the SA/group split. Pods sharing one service account can assert any group in that account's list; per-owner-group service accounts would tighten this.
- W37. fusion-ext-system-bff (BFF for external systems such as opencode): each external system (OAuth2 client id or API key name) gets predefined owner groups, configured via Helm values (read groups, write groups). The ext BFF is a trusted proxy towards fusion-index and forwards the resulting scope; the public download endpoint has its own configured group. No per-resource public flag (W12).
- W38. Weave callers (answers Q34):
  - Normal path: the user comes through the BFF. The BFF authenticates to weave with its service-account token; weave's TokenReview identifies it as `sa/system:serviceaccount:<ns>:<name>`. That principal is marked `trustedProxy` in weave's ownership config and has no groups of its own: weave takes the user's scope (`X-User-Groups`, `X-User-Writable-Groups`, `X-User-All-Groups`, `X-User-Default-Group`) from the headers. Weave does not read any `X-User-*` header today and must be extended; headers from any other caller are stripped and ignored.
  - Other callers (API keys `apikey/<name>`, OIDC subjects `oidc/<sub>`, other service accounts `sa/...`) get a scope only through an explicit entry in the ownership config, keyed by weave's existing principal string; without an entry they get nothing.
  - Roles (`viewer`/`editor`/`admin`) stay separate from scope (W5); the weave `admin` role does not imply all-groups.
  - Fail fast: with `ownership.enforce=true` a service refuses to start if authentication is disabled (weave `ALLOW_UNAUTHENTICATED=true`, index/forge auth off). The check lives in `fusion-shared`'s config validation, shared by all services.
- W39. Names (accepted, answers Q30):
  - Permissions: `admin:all-groups:access` (W13); `forge:owner:change`, `index:owner:change`, `weave:owner:change`, `wizard:owner:change` (W15).
  - Headers: `X-User-Groups` (read scope), `X-User-Writable-Groups`, `X-User-All-Groups` (`true`), `X-User-Default-Group`.
  - Endpoints: `GET /bff/admin/owner-group-overview`, `GET /bff/admin/owner-group-resolve?email=`, `GET /bff/admin/oidc-groups-seen`, `POST /bff/admin/owner-groups/{id}/retire`, and `PUT /<resource>/{id}/owner` in each service.
  - Data: CR label `fusion-platform.io/owner-group`; DB column `owner_group`; JSON field `ownerGroup` in services using camelCase, `owner_group` in BFF-owned endpoints; `owner_groups` columns `kind`, `mode`, `bound_user_id`, `retired_at`; table `oidc_group_seen`; `groups_truncated` in `/bff/userinfo`.
  - Config: `PERSONAL_GROUP_CLAIM` (Helm `config.personalGroupClaim`, default `preferred_username`); `ownership.enforce`, `ownership.legacyGroup`; service-account entry keys `trustedProxy`, `groups`, `writableGroups`, `allGroups`, `assertableGroups`; Helm `sharedGroupName` (default `shared`); env `WEAVE_OWNER_GROUP` injected into job pods (W36).

## 2. Current state (facts only)

- fusion-bff (0.13.x): owner groups, default owner group, preferred owner group.
  Resolves membership at login (OIDC group mapping, email, `user_id`) and forwards
  `X-User-Groups` to all upstreams. Enforces nothing by owner group.
- fusion-index (main branch): no owner column. Artifacts have a namespaced full name `org.team.name`, a naming convention only (not enforced, not tied to any group).
- fusion-forge: builds store `creator_id` / `creator_email` (audit info: who created). Forge and weave also set the label `fusion-platform.io/managed-by` (`manual`, `wizard`, ...): a classification of how a resource is managed.
- None of these is an owner group. They stay as they are; the owner group (W1) is a new, separate field.
- fusion-weave (repo `fusion-flux`): CRs `WeaveChain`, `WeaveJobTemplate`, `WeaveServiceTemplate`, `WeaveTrigger` (incl. batch/kafka variants), `WeaveRun`. API server auth: API key (Secret), OIDC JWT, or SA token (TokenReview); role (`viewer`/`editor`/`admin`) from Secret annotation, JWT claim or SA label `fusion-platform.io/role`. No service-account allowlist like `AUTH_ALLOWED_SA`. `ALLOW_UNAUTHENTICATED=true` grants admin.
- fusion-forge: one DB table `venv_build` (all build types incl. app builds; handlers venvs/builds/gitbuilds/appbuilds), plus two CRs created through its API: `CIBuild` and `GitWatcher` (GitWatcher CRUD is K8s CR only, no DB). A watcher triggers builds by itself.
- fusion-runner / code-loader: job pods download artifacts from fusion-index over REST; the runner client sends no Authorization header.
- fusion-ext-system-bff: machine clients (OAuth2 client credentials, API key, or open) forwarded to fusion-index only; identity forwarded as `X-System-ID`/`X-System-Name`, no groups; has an unauthenticated public index endpoint for one artifact type.
- fusion-content: no database, global content (changelog, help, videos), gated only by BFF permissions. No owned resources.
- fusion-fleet (Flux manifests), fusion-grafana (dashboards), fusion-testcases (sample apps): no API, no owned resources. fusion-weave-testbed creates resources for end-to-end tests and needs owner handling in its test data. fusion-spectra: GUI work only.

## 3. Open questions

- ~~Q1~~ answered: W6. ~~Q3~~ answered: W7.
- ~~Q9~~ answered: W12, W24.
- ~~Q10~~ answered: W13.
- ~~Q2~~ answered: W8-W11.
- ~~Q11~~ answered: W19. Handover is one move per resource (W15) plus a list of a group's resources for admins; a bulk action can be added later.
- ~~Q3~~ answered: W7, W13.
- ~~Q4~~ answered: W20.
- ~~Q5~~ answered: W18 (follow-up answered: W22, weave and wizard are trusted proxies).
- ~~Q6~~ answered: W15.
- ~~Q7~~ answered: W14.
- ~~Q8~~ answered: W16.
- ~~Q12~~ answered: W21.
- ~~Q13~~ answered: public repo under chrisfusion (W21).
- ~~Q14~~ answered: W22.
- ~~Q17~~ answered: W23.
- ~~Q18~~ answered: W26 data protection defaults.
- ~~Q19~~ answered: W33.
- ~~Q20~~ answered: W35 (concurrent users and the 100-group stress case are my assumptions, to confirm).
- ~~Q15~~ answered: W17.
- ~~Q16~~ answered: W11 tightened.
- ~~Q21~~ accepted, see 4.6 and W23 (wizard forwards chosen group + shared groups as read scope; write scope = chosen group).
- ~~Q22~~ accepted: names stay unique across groups, 409 without details, revisit if teams collide.
- ~~Q23~~ accepted: invisible resource 404, read-only write 403 (4.5).
- ~~Q24~~ answered: default claim `preferred_username` (configurable via `PERSONAL_GROUP_CLAIM`).
- ~~Q26~~ answered: load assumptions kept (100 concurrent users, stress user with 100 groups).
- ~~Q27~~ answered by investigation, see section 2 (weave) and Q34.
- ~~Q28~~ answered by investigation, see section 2: no owner needed in content, fleet, grafana, testcases, runner (but see Q32); ext-system-bff see Q33; spectra GUI only; testbed test data.
- ~~Q29~~ answered by investigation: owner on DB table `venv_build` and on the CRs `CIBuild` and `GitWatcher`; builds triggered by a watcher inherit the watcher's owner and a build's CR copies the build's owner (explicit copy, like weave triggers→runs).
- ~~Q30~~ answered: W39.
- Q31. Housekeeping: first commit and push of `fusion-shared` (plan + logging principles), plan pointer in each service's `CLAUDE.md`, and what to do with the parent directory's local git (staged changes of its own).
- ~~Q32~~ answered: W36.
- ~~Q33~~ answered: W37.
- ~~Q34~~ answered: W38.
- ~~Q25~~ accepted: header cap 100 with truncation and `groups_truncated`, plus `X-User-Default-Group` (4.1).

## 4. Design (DRAFT for review)

Derived from W1-W35. Items marked **(proposal)** are design choices not yet confirmed; they are listed again in section 3 as open questions.

### 4.1 Header contract (BFF → services)

| Header | Meaning | Source |
|---|---|---|
| `X-User-ID`, `X-User-Email` | caller identity | existing |
| `X-User-Groups` | owner groups the caller may **see** (comma-separated) | existing |
| `X-User-Writable-Groups` | subset of the above the caller may **write** to (W24) | new |
| `X-User-All-Groups` | `true` only if the caller holds the all-groups permission (W13) | new |
| `X-User-Default-Group` | preferred owner group, else personal group; used when a create request names no group (W14) | new **(proposal)**: services do not know the preference, so the BFF must send it |

- The BFF strips all of these from incoming client requests, then sets them from the session / token.
- Services honour them only from service accounts marked `trustedProxy` (W18). From any other account they are stripped and ignored; that account's own configured scope applies instead.
- Cap: at most 100 groups per header (W35 stress case). Above the cap the BFF truncates deterministically (personal group, preferred group, then alphabetical), logs a warning and sets `groups_truncated: true` in `/bff/userinfo`. Truncating only ever reduces access **(proposal)**.

### 4.2 Permissions and routes

- `admin:all-groups:access` (W13): granted to `admin` by default, independent of the role.
- `<svc>:owner:change` for `forge`, `index`, `weave`, `wizard` (W15): granted to `admin` and `engineer`.
- Move endpoint per resource: `PUT /<resource>/{id}/owner` with body `{"ownerGroup": "..."}` (services use camelCase; BFF-owned endpoints use `owner_group`). Own rule in `rbac.yaml` before the broader PUT/PATCH rules (same precedent as weave `/runs/*/stop`). Both `rbac.yaml` copies stay in sync.
- Where a service lacks a matching permission check for sub-paths, route order decides (first match wins).

### 4.3 BFF data model and behaviour

- `owner_groups` gains `kind` (`personal`|`regular`, default `regular`), `mode` (`rw`|`ro`, default `rw`), `bound_user_id` (unique, nullable), `retired_at` (nullable). Constraint: `personal` requires `bound_user_id` and `rw`. An existing group named `default` stays a regular group; admins decide its fate.
- `owner_group_oidc_mappings` accepts the reserved value `*` (W33).
- New table `oidc_group_seen (oidc_group, user_id, first_seen, last_seen)`, PK `(oidc_group, user_id)`, index `(oidc_group, last_seen)` (W26). Retention job deletes in small batches.
- Login (after token validation and allowlist):
  1. Read the configured claim (`PERSONAL_GROUP_CLAIM`, Helm `config.personalGroupClaim`; default `preferred_username`), lower-case, validate against W11.
  2. Get or create the personal group bound to `user_id` with `INSERT ... ON CONFLICT`; handle name collision (W10) as 403. Concurrent first logins of the same user converge on the unique constraints. A retired group of the same `user_id` is reactivated (W19).
  3. Resolve groups: wildcard mapping `*`, normalised OIDC mappings (W30), direct members (email, `user_id`), personal group.
  4. Writable = groups with mode `rw`; everything if the all-groups permission is held.
  5. Upsert `oidc_group_seen` in one batched statement, and only when `last_seen` is older than 1 hour (W34).
  6. Store read/write groups, default group and all-groups flag in the session.
- Bearer path: same resolution with a short-TTL cache keyed by `user_id` (default 60 s), no DB call per request (W34).
- Admin API (all `admin:roles:manage`, audit-logged): existing owner-group, mapping and member endpoints plus `PATCH` for groups (mode, description) and mappings (W28); `POST /bff/admin/owner-groups/{id}/retire`; `GET /bff/admin/owner-group-overview` (paginated, W27); `GET /bff/admin/oidc-groups-seen` (counts within the window, affected users, W26); `GET /bff/admin/owner-group-resolve?email=...` (W29). The admin API refuses mappings and members on personal groups (W19).
- Seed job extends `rbacSeed` (W31): shared group (mode `ro`), mapping `*` → shared, idempotent.

### 4.4 `fusion-shared` package `ownership` (W21)

- `Scope{UserID, Read []string, Write []string, All bool, DefaultGroup string}`, built by `FromRequest(r, Config)`. For a trusted proxy it parses the headers; for any other service account it uses that account's configured scope; otherwise it fails closed.
- Decisions: `CanRead(owner)`, `CanWrite(owner)`, `CanCreate(owner)`, `CanMove(from, to)`, `ResolveCreateOwner(requested)` (applies `DefaultGroup` for users; service accounts must name the group explicitly, W18).
- Query builders (W17): `SQLClause(col, argIndex)` → `col = ANY($n)` (or no filter when `All`); `LabelSelector(key)` → `key in (a,b,c)`, with a flag when it exceeds the length limit so the caller filters in memory.
- Constants: header names, label key `fusion-platform.io/owner-group`, `ValidGroupName` (W11).
- `Config` loaded from a mounted file (W32): `enforce`, `legacyGroup`, service-account entries (`name`, `trustedProxy`, `groups`, `writableGroups` | `allGroups`). An empty list means no service account is allowed, never "any account".
- Gin middleware in a sub-package (all current services use Gin handlers). Benchmarks for the builders (W35).

### 4.5 Behaviour every service implements

- **Storage:** Postgres services add `owner_group TEXT NOT NULL` with an index (composite with the usual sort key for keyset pagination). CRD services set the label `fusion-platform.io/owner-group` on every resource created through their API.
- **Create:** owner from the request (or the default), checked with `CanCreate`; a resource never exists without an owner (W14).
- **List:** the scope is applied in the query, before pagination (W17).
- **Get/update/delete:** a resource outside the read scope answers **404**, not 403, so existence is not revealed **(proposal)**; read-only access to a write operation answers 403.
- **Move:** the owner endpoint of 4.2, audit-logged with old and new group (slog), checks `CanMove`.
- **References (W22):** checked at create/update using the caller's scope; across services the caller's headers are forwarded by the trusted proxy.
- **Switch:** `ownership.enforce=false` keeps today's behaviour but still records the owner on create (default group, else the legacy group) so migration stays short; `true` enforces everything (W20).

### 4.6 Service specifics

- **fusion-index:** owner on `registry_artifact`; versions, files, tags and types inherit through `artifact_id` (join / `EXISTS`). `full_name` is globally unique, see Q22.
- **fusion-forge:** owner on each resource type (builds, gitwatchers, ...); the full list is enumerated in forge's own rollout step. `creator_id`/`creator_email` stay as audit fields.
- **fusion-weave:** owner label on all CRs created via the API server; list via label selector (or cache index). `createRun`, `createBatchRun` and `createKafkaRun` in the trigger controller must copy the owner label explicitly: custom labels on a WeaveTrigger do not propagate to its runs. The check of references happens in the API server (W22 limits). CR names are unique per namespace across all groups, see Q22.
- **fusion-wizard:** owner = group chosen at start, stored on the WizardRun (W23); background steps forward only that group, see Q21.
- **fusion-spectra (GUI):** group picker on every create form (default from `preferred_owner_group`, prefilled), admin overview, dry-run, retire action, warning before a move (W15). Specified here, built in spectra's own plan.

### 4.7 Security and performance notes

- Headers are stripped at the BFF edge and ignored from non-proxy callers; services never trust client-supplied scope.
- Audit logs (slog, structured) for moves, retire, mapping changes and collision rejections.
- Performance design follows W34/W35: owner indexes, keyset pagination, batched throttled login writes, Bearer cache, header cap, batched migrations.

## 5. Rollout plan (DRAFT for review)

Order matters: each step is forward-compatible, so nothing breaks while others catch up. Enforcement is the last step per service (W20).

0. **Setup:** the public repo `fusion-shared` exists; this plan lives in it as `docs/multi-tenancy.md`; each service `CLAUDE.md` gets a pointer to this plan.
1. **`fusion-shared` v0.1.0:** scope, header parsing, builders, config, tests and benchmarks (W35).
2. **fusion-bff (next minor, e.g. 0.14.0):** data model, personal group, wildcard mapping, new headers, admin endpoints, seed job, new permissions in both `rbac.yaml` copies, Helm values (`sharedGroupName`, wildcard mapping, `personalGroupClaim`, seed). Forward-only: services ignore the new headers until they adopt the library. Docs per the BFF rules: README, ARCHITECTURE, EXAMPLE, `openapi.yaml`, CHANGELOG, Chart version.
3. **fusion-index:** adopt the library, column + migration (legacy group), SA scope config with trusted proxies BFF, weave, wizard; enforcement off.
4. **fusion-forge:** same pattern.
5. **fusion-weave:** label on creation, trigger→run label propagation, relabel job for existing CRs, API-server reference checks, SA config; weave becomes a trusted proxy towards forge/index (needs steps 3-4 config first).
6. **fusion-wizard:** owner on runs, trusted proxy towards forge/index/weave, reference checks, SA config.
7. **Other repos:** assess content, fleet, runner, ext-system-bff, spectra for owned resources (content is probably global: help, changelog). Spectra GUI work follows the BFF API.
8. **Enable enforcement:** per service, dev → staging → prod, only after that service's migration ran; run the load tests of W35 first. Then remove `DEFAULT_OWNER_GROUP` from the BFF.

Per-service checklist: library bump + `make vendor`; column/label + migration; list/get/create/update/delete/move through the scope; reference checks; SA scope config in Helm; enforcement flag; tests (including the large-volume test); CHANGELOG, Chart version, docs.
