// SPDX-License-Identifier: GPL-3.0-or-later

// Package ownership implements owner-group scoping for fusion services. It turns
// "who is calling, with which trusted headers" into a Scope and answers who may
// see, create, change or move a resource. It does no authentication, no storage
// access and no Kubernetes calls. The rules are defined in docs/multi-tenancy.md.
//
// # Model
//
// Every resource has exactly one owner group. A user sees the groups they belong
// to and writes to those that are read-write; roles stay global and are not part
// of this package. A scope that holds the all-groups permission sees and writes
// everything. The zero Scope sees nothing, and so does a resource without an
// owner (a not yet migrated one) for everybody except an all-groups scope.
//
// # Wiring a service
//
//  1. Load the config once at start-up and refuse to start on an unsafe setup:
//
//     cfg, err := ownership.Load("/etc/fusion/ownership.yaml")
//     if err == nil { err = cfg.Validate(authEnabled) } // ErrAuthRequired if enforcing without auth
//     res, err := ownership.NewResolver(cfg, ownership.WithLogger(logger))
//
//  2. Install the middleware after the service's own authentication. The
//     principal is whatever that authentication produced (a service account, an
//     API key, an OIDC subject). Use Resolver.Middleware for net/http and chi, or
//     ginmw.Middleware for Gin. The middleware strips the X-User-* headers from
//     the request, so handlers must use the Scope, not the headers.
//
//  3. In handlers, read the scope (FromContext, or ginmw.Scope) and use it:
//
//     - list:   SQLClause or LabelSelector, applied in the query and before
//     pagination, so pages and counts are right. Fall back to Filter when
//     LabelSelector reports SelectInMemory.
//     - get:    scope.CheckRead(owner), 404 when not visible.
//     - update and delete: scope.CheckWrite(owner), 404 when not visible and
//     403 when read-only.
//     - create: owner, err := scope.ResolveCreateOwner(requested).
//     - move:   scope.CheckMove(from, to), on a dedicated endpoint.
//     - errors: HTTPStatus and ErrorMessage map every ownership error to a status
//     and a fixed message.
//
// Resources store the owner in the column ColumnOwnerGroup or the label
// LabelOwnerGroup (see OwnerFromLabels and WithOwnerLabel).
//
// # Who may call
//
// Callers are configured in the ownership file (see Config and PrincipalEntry):
// a trusted proxy (the BFF, weave, wizard) forwards the end user's scope in the
// trusted headers; other principals have a fixed scope of their own, or may only
// assert a read scope limited to configured groups (job pods). An unknown
// principal gets nothing, and with enforcement on the middleware answers 403.
// With enforcement off every check passes, but creates still record an owner.
//
// # Headers
//
// The BFF and other trusted proxies set the headers with FormatHeaders and
// CapGroups when they call a service; services never honour them from any other
// caller. ParseHeaders only ever narrows access: invalid names, duplicates and
// entries beyond MaxGroups are dropped.
package ownership
