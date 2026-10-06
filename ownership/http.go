// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

type scopeKey struct{}

// WithScope returns a context carrying the scope.
func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, s)
}

// FromContext returns the scope stored by the middleware. When there is none it
// returns the zero Scope, which sees and writes nothing, and false.
func FromContext(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(scopeKey{}).(Scope)
	return s, ok
}

// Attach resolves the caller's scope and returns a copy of req that carries it
// in its context and has the trusted X-User-* headers stripped (the original
// request is not modified). principal is whatever the service's own
// authentication produced for this request.
//
// Errors map to HTTP with HTTPStatus: ErrNoPrincipal (401, not authenticated) and
// ErrUnknownPrincipal (403, enforcement is on and the caller has no entry in the
// ownership config).
func (r *Resolver) Attach(req *http.Request, principal string) (*http.Request, error) {
	scope, err := r.Resolve(principal, req.Header)
	if err != nil {
		return nil, err
	}
	if r.cfg.Enforce {
		if _, known := r.cfg.Lookup(principal); !known {
			r.log.LogAttrs(req.Context(), slog.LevelWarn, "ownership: caller is not configured, request denied",
				slog.String("principal", principal))
			return nil, ErrUnknownPrincipal
		}
	}
	out := req.Clone(WithScope(req.Context(), scope))
	StripHeaders(out.Header)
	return out, nil
}

// MiddlewareOption customises Middleware.
type MiddlewareOption func(*middlewareConfig)

type middlewareConfig struct {
	onError func(http.ResponseWriter, *http.Request, error)
}

// WithErrorHandler replaces the default JSON error response of Middleware.
func WithErrorHandler(f func(w http.ResponseWriter, r *http.Request, err error)) MiddlewareOption {
	return func(c *middlewareConfig) {
		if f != nil {
			c.onError = f
		}
	}
}

// Middleware returns a standard net/http middleware (usable with chi, ServeMux
// and others) that attaches the caller's scope to the request context and strips
// the trusted headers. Handlers read the scope with FromContext. Install it after
// the service's authentication, so principal can return the authenticated caller.
func (r *Resolver) Middleware(principal func(*http.Request) string, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := middlewareConfig{onError: func(w http.ResponseWriter, _ *http.Request, err error) { WriteError(w, err) }}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			out, err := r.Attach(req, principal(req))
			if err != nil {
				cfg.onError(w, req, err)
				return
			}
			next.ServeHTTP(w, out)
		})
	}
}

// ErrorMessage returns a short, fixed message for an ownership error. It never
// echoes the error text, so nothing about groups or configuration leaks.
func ErrorMessage(err error) string {
	switch {
	case errors.Is(err, ErrNoPrincipal):
		return "authentication required"
	case errors.Is(err, ErrUnknownPrincipal):
		return "caller is not allowed to use this service"
	case errors.Is(err, ErrNotVisible):
		return "not found"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrOwnerRequired):
		return "owner group required"
	case errors.Is(err, ErrInvalidGroupName):
		return "invalid owner group name"
	default:
		return "internal error"
	}
}

// WriteError answers an ownership error with HTTPStatus and a JSON body
// {"error": ErrorMessage(err)}.
func WriteError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(HTTPStatus(err))
	_ = json.NewEncoder(w).Encode(map[string]string{"error": ErrorMessage(err)})
}
