// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
)

// ErrNoPrincipal is returned by Resolve when the service passed no principal,
// meaning the caller was not authenticated. Map it to 401.
var ErrNoPrincipal = errors.New("no authenticated principal")

// Resolver turns an authenticated principal and the request headers into a
// Scope according to the service's ownership config.
type Resolver struct {
	cfg *Config
	log *slog.Logger
}

// ResolverOption customises a Resolver.
type ResolverOption func(*Resolver)

// WithLogger sets the logger used for anomalies such as a trusted proxy that
// sent no scope. The default is slog.Default().
func WithLogger(l *slog.Logger) ResolverOption {
	return func(r *Resolver) {
		if l != nil {
			r.log = l
		}
	}
}

// NewResolver returns a Resolver for cfg. The config is checked for structural
// problems; use Config.Validate(authEnabled) at start-up for the full check.
func NewResolver(cfg *Config, opts ...ResolverOption) (*Resolver, error) {
	if cfg == nil {
		return nil, errors.New("ownership: nil config")
	}
	if err := cfg.checkStructure(); err != nil {
		return nil, err
	}
	r := &Resolver{cfg: cfg, log: slog.Default()}
	for _, o := range opts {
		o(r)
	}
	return r, nil
}

// Enforcing reports whether enforcement is switched on.
func (r *Resolver) Enforcing() bool { return r.cfg.Enforce }

// Resolve computes the caller's Scope. principal is the opaque string the
// service's own authentication produced (for example weave's
// "sa/system:serviceaccount:<ns>:<name>"); it is looked up in the config.
//
//   - No principal: ErrNoPrincipal (the caller is not authenticated).
//   - Unknown principal: an empty scope, no error. Headers are ignored.
//   - Trusted proxy: the scope comes from the trusted headers, with Write
//     narrowed to Read. No headers means an empty scope.
//   - Other configured principals: the scope from the config; headers are
//     ignored, except for assertable principals (job pods), which may assert a
//     read scope limited to their configured groups.
//
// With enforcement off every authenticated caller gets an unenforced scope. Resolve
// never modifies h; call StripHeaders on the request separately.
func (r *Resolver) Resolve(principal string, h http.Header) (Scope, error) {
	if principal == "" {
		return Scope{}, ErrNoPrincipal
	}
	entry, known := r.cfg.Lookup(principal)

	if !r.cfg.Enforce {
		return r.unenforced(principal, entry, known, h), nil
	}
	if !known {
		r.log.LogAttrs(context.Background(), slog.LevelDebug, "ownership: principal not configured, empty scope",
			slog.String("principal", principal))
		return newScope(Scope{Principal: principal, Service: true}), nil
	}

	switch {
	case entry.TrustedProxy:
		return r.fromHeaders(principal, h), nil
	case entry.AllGroups:
		return newScope(Scope{Principal: principal, Service: true, All: true}), nil
	case len(entry.AssertableGroups) > 0:
		hdr := ParseHeaders(h)
		// Pods are not trusted for identity: the asserted user id is ignored so
		// it cannot be forged into audit logs.
		return newScope(Scope{
			Principal: principal,
			Service:   true,
			Read:      intersect(hdr.Groups, entry.AssertableGroups),
		}), nil
	default:
		return newScope(Scope{
			Principal: principal,
			Service:   true,
			Read:      append([]string(nil), entry.Groups...),
			Write:     append([]string(nil), entry.WritableGroups...),
		}), nil
	}
}

// fromHeaders builds the scope of an end user forwarded by a trusted proxy.
func (r *Resolver) fromHeaders(principal string, h http.Header) Scope {
	hdr := ParseHeaders(h)
	write := intersect(hdr.Writable, hdr.Groups)

	if hdr.Invalid > 0 || hdr.Truncated || len(write) != len(hdr.Writable) {
		r.log.LogAttrs(context.Background(), slog.LevelWarn, "ownership: trusted headers were narrowed",
			slog.String("principal", principal),
			slog.Int("invalid_entries", hdr.Invalid),
			slog.Bool("truncated", hdr.Truncated),
			slog.Int("writable_outside_read", len(hdr.Writable)-len(write)))
	}
	if len(hdr.Groups) == 0 && !hdr.All {
		r.log.LogAttrs(context.Background(), slog.LevelWarn, "ownership: trusted proxy sent no scope, empty scope",
			slog.String("principal", principal))
	}
	return newScope(Scope{
		Principal:    principal,
		UserID:       hdr.UserID,
		Email:        hdr.Email,
		Read:         hdr.Groups,
		Write:        write,
		All:          hdr.All,
		DefaultGroup: hdr.DefaultGroup,
	})
}

// unenforced builds the scope used while enforcement is off: every check passes,
// and creates still record an owner. Trusted proxies still supply the user, the
// default group and the forwarded scope.
func (r *Resolver) unenforced(principal string, entry PrincipalEntry, known bool, h http.Header) Scope {
	s := Scope{Principal: principal, Unenforced: true, LegacyGroup: r.cfg.LegacyGroup, Service: true}
	if known && entry.TrustedProxy {
		hdr := ParseHeaders(h)
		s.Service = false
		s.UserID = hdr.UserID
		s.Email = hdr.Email
		s.DefaultGroup = hdr.DefaultGroup
		// Keep the forwarded scope so a proxy that is not enforcing yet can still
		// pass it on to upstream services that are (see Scope.Headers).
		s.Read = hdr.Groups
		s.Write = intersect(hdr.Writable, hdr.Groups)
		s.All = hdr.All
	}
	return newScope(s)
}
