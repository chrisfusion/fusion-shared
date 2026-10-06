// SPDX-License-Identifier: GPL-3.0-or-later

// Package ginmw adapts the ownership package to Gin. It is a separate package so
// services that do not use Gin never import it.
package ginmw

import (
	"github.com/gin-gonic/gin"

	"github.com/chrisfusion/fusion-shared/ownership"
)

// Middleware attaches the caller's scope to the request context and strips the
// trusted X-User-* headers (see ownership.Resolver.Attach). principal returns
// the caller the service's own authentication found; install the middleware
// after that authentication. Failures abort with the status and JSON error from
// ownership.HTTPStatus and ownership.ErrorMessage.
func Middleware(r *ownership.Resolver, principal func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		req, err := r.Attach(c.Request, principal(c))
		if err != nil {
			Abort(c, err)
			return
		}
		c.Request = req
		c.Next()
	}
}

// Scope returns the caller's scope stored by Middleware. Without the middleware
// it returns the zero Scope, which sees and writes nothing.
func Scope(c *gin.Context) ownership.Scope {
	s, _ := ownership.FromContext(c.Request.Context())
	return s
}

// Abort answers an ownership error (from the Scope decisions) with the right
// status and a fixed JSON message, and stops the handler chain.
func Abort(c *gin.Context, err error) {
	c.AbortWithStatusJSON(ownership.HTTPStatus(err), gin.H{"error": ownership.ErrorMessage(err)})
}
