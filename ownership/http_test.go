// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// principalFromHeader lets tests choose the authenticated principal.
func principalFromHeader(r *http.Request) string { return r.Header.Get("X-Test-Principal") }

func request(principal string, kv ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/things", nil)
	if principal != "" {
		r.Header.Set("X-Test-Principal", principal)
	}
	for i := 0; i < len(kv); i += 2 {
		r.Header.Set(kv[i], kv[i+1])
	}
	return r
}

type seen struct {
	called  bool
	scope   Scope
	hasOK   bool
	headers http.Header
}

func serve(t *testing.T, res *Resolver, req *http.Request, opts ...MiddlewareOption) (*httptest.ResponseRecorder, *seen) {
	t.Helper()
	got := &seen{}
	h := res.Middleware(principalFromHeader, opts...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.called = true
		got.scope, got.hasOK = FromContext(r.Context())
		got.headers = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, got
}

func TestFromContext(t *testing.T) {
	if s, ok := FromContext(context.Background()); ok || !s.IsEmpty() {
		t.Fatalf("no scope in context must give the zero scope, got %+v %v", s, ok)
	}
	want := userScope()
	got, ok := FromContext(WithScope(context.Background(), want))
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip failed: %+v %v", got, ok)
	}
}

func TestMiddleware_TrustedProxyScopeAndStripping(t *testing.T) {
	res := newTestResolver(t, true)
	req := request(pBFF,
		HeaderUserID, "u-1", HeaderUserEmail, "a@corp.com",
		HeaderGroups, "alice,shared", HeaderWritableGroups, "alice", HeaderDefaultGroup, "alice",
	)
	rec, got := serve(t, res, req)

	if rec.Code != http.StatusNoContent || !got.called || !got.hasOK {
		t.Fatalf("code=%d called=%v scope-in-context=%v", rec.Code, got.called, got.hasOK)
	}
	s := got.scope
	if s.UserID != "u-1" || s.Email != "a@corp.com" || !s.CanWrite("alice") || s.CanWrite("shared") || !s.CanRead("shared") {
		t.Fatalf("unexpected scope: %+v", s)
	}
	for _, k := range []string{HeaderUserID, HeaderUserEmail, HeaderGroups, HeaderWritableGroups, HeaderAllGroups, HeaderDefaultGroup} {
		if got.headers.Get(k) != "" {
			t.Errorf("handler still sees %s", k)
		}
	}
	if got.headers.Get("X-Test-Principal") != pBFF {
		t.Error("unrelated headers must be kept")
	}
	if req.Header.Get(HeaderGroups) == "" {
		t.Error("the original request was modified")
	}
}

func TestMiddleware_ClientHeadersFromNonProxyDoNotLeak(t *testing.T) {
	res := newTestResolver(t, true)
	rec, got := serve(t, res, request(pCI, HeaderGroups, "victim", HeaderAllGroups, "true", HeaderWritableGroups, "victim"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d", rec.Code)
	}
	if got.scope.All || got.scope.CanRead("victim") || got.headers.Get(HeaderAllGroups) != "" || got.headers.Get(HeaderGroups) != "" {
		t.Fatalf("client-supplied headers leaked: %+v %v", got.scope, got.headers)
	}
	if !got.scope.CanWrite("team-data") {
		t.Fatalf("configured scope missing: %+v", got.scope)
	}
}

func TestMiddleware_Rejections(t *testing.T) {
	enforced := newTestResolver(t, true)

	tests := []struct {
		name       string
		res        *Resolver
		req        *http.Request
		wantStatus int
		wantMsg    string
	}{
		{"no principal", enforced, request(""), http.StatusUnauthorized, "authentication required"},
		{"unknown principal while enforcing", enforced, request(pUnknown, HeaderAllGroups, "true"), http.StatusForbidden, "caller is not allowed to use this service"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec, got := serve(t, tc.res, tc.req)
			if got.called {
				t.Fatal("the handler must not run")
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Fatalf("content type = %q", ct)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != tc.wantMsg {
				t.Fatalf("body = %q (%v)", rec.Body.String(), err)
			}
		})
	}
}

func TestMiddleware_UnknownPrincipalPassesWhileUnenforced(t *testing.T) {
	rec, got := serve(t, newTestResolver(t, false), request(pUnknown))
	if rec.Code != http.StatusNoContent || !got.called || !got.scope.Unenforced {
		t.Fatalf("code=%d called=%v scope=%+v", rec.Code, got.called, got.scope)
	}
}

func TestMiddleware_CustomErrorHandler(t *testing.T) {
	var gotErr error
	rec, got := serve(t, newTestResolver(t, true), request(""), WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, err error) {
		gotErr = err
		w.WriteHeader(http.StatusTeapot)
	}))
	if rec.Code != http.StatusTeapot || got.called || gotErr != ErrNoPrincipal {
		t.Fatalf("code=%d called=%v err=%v", rec.Code, got.called, gotErr)
	}
}

func TestErrorMessageNeverEchoesDetails(t *testing.T) {
	_, err := userScope().ResolveCreateOwner("Secret Team Name")
	if msg := ErrorMessage(err); msg != "invalid owner group name" || strings.Contains(msg, "Secret") {
		t.Fatalf("message = %q", msg)
	}
	if ErrorMessage(ErrNotVisible) != "not found" || ErrorMessage(ErrForbidden) != "forbidden" ||
		ErrorMessage(ErrOwnerRequired) != "owner group required" || ErrorMessage(http.ErrAbortHandler) != "internal error" {
		t.Fatal("unexpected message")
	}
	if HTTPStatus(ErrUnknownPrincipal) != http.StatusForbidden {
		t.Fatal("ErrUnknownPrincipal must map to 403")
	}
}
