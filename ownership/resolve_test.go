// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const (
	pBFF     = "sa/system:serviceaccount:fusion:fusion-bff"
	pCI      = "sa/system:serviceaccount:fusion:ci"
	pAdmin   = "oidc/ops-admin"
	pJobs    = "sa/system:serviceaccount:fusion:jobs"
	pUnknown = "sa/system:serviceaccount:other:intruder"
)

func testConfig(enforce bool) *Config {
	return &Config{
		Enforce:     enforce,
		LegacyGroup: "shared",
		Principals: []PrincipalEntry{
			{Name: pBFF, TrustedProxy: true},
			{Name: pCI, Groups: []string{"team-data", "shared"}, WritableGroups: []string{"team-data"}},
			{Name: pAdmin, AllGroups: true},
			{Name: pJobs, AssertableGroups: []string{"team-data", "team-ml"}},
		},
	}
}

func newTestResolver(t *testing.T, enforce bool, opts ...ResolverOption) *Resolver {
	t.Helper()
	r, err := NewResolver(testConfig(enforce), opts...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func hdr(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestNewResolver(t *testing.T) {
	if _, err := NewResolver(nil); err == nil {
		t.Fatal("nil config must be rejected")
	}
	bad := &Config{Principals: []PrincipalEntry{{Name: "a"}}}
	if _, err := NewResolver(bad); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
	if !newTestResolver(t, true).Enforcing() || newTestResolver(t, false).Enforcing() {
		t.Fatal("Enforcing() does not follow the config")
	}
}

func TestResolve_NoPrincipal(t *testing.T) {
	for _, enforce := range []bool{true, false} {
		_, err := newTestResolver(t, enforce).Resolve("", hdr(HeaderGroups, "a", HeaderAllGroups, "true"))
		if !errors.Is(err, ErrNoPrincipal) || HTTPStatus(err) != http.StatusUnauthorized {
			t.Fatalf("enforce=%v: error = %v, want ErrNoPrincipal", enforce, err)
		}
	}
}

func TestResolve_UnknownPrincipalGetsNothing(t *testing.T) {
	r := newTestResolver(t, true)
	s, err := r.Resolve(pUnknown, hdr(HeaderGroups, "team-data", HeaderWritableGroups, "team-data", HeaderAllGroups, "true"))
	if err != nil {
		t.Fatal(err)
	}
	if !s.IsEmpty() || s.All || len(s.Write) != 0 || !s.Service {
		t.Fatalf("unknown principal must get an empty service scope, got %+v", s)
	}
	if s.CanRead("team-data") || s.CanCreate("team-data") {
		t.Fatal("headers from an unknown principal must be ignored")
	}
}

func TestResolve_TrustedProxy(t *testing.T) {
	r := newTestResolver(t, true)

	t.Run("user scope from headers", func(t *testing.T) {
		s, err := r.Resolve(pBFF, hdr(
			HeaderUserID, "u-1",
			HeaderGroups, "alice,team-data,shared",
			HeaderWritableGroups, "alice,team-data",
			HeaderDefaultGroup, "alice",
		))
		if err != nil {
			t.Fatal(err)
		}
		if s.Principal != pBFF || s.UserID != "u-1" || s.Service || s.All || s.DefaultGroup != "alice" {
			t.Fatalf("unexpected scope: %+v", s)
		}
		if !reflect.DeepEqual(s.Read, []string{"alice", "team-data", "shared"}) || !reflect.DeepEqual(s.Write, []string{"alice", "team-data"}) {
			t.Fatalf("read/write: %v %v", s.Read, s.Write)
		}
		if !s.CanRead("shared") || s.CanWrite("shared") || !s.CanWrite("alice") {
			t.Fatal("decisions do not follow the headers")
		}
	})

	t.Run("writable outside read is dropped", func(t *testing.T) {
		s, _ := r.Resolve(pBFF, hdr(HeaderGroups, "alice", HeaderWritableGroups, "alice,team-data"))
		if !reflect.DeepEqual(s.Write, []string{"alice"}) || s.CanWrite("team-data") {
			t.Fatalf("write must be a subset of read, got %v", s.Write)
		}
	})

	t.Run("all-groups flag", func(t *testing.T) {
		s, _ := r.Resolve(pBFF, hdr(HeaderAllGroups, "true"))
		if !s.All || !s.CanWrite("anything") {
			t.Fatalf("all-groups header not honoured: %+v", s)
		}
		s, _ = r.Resolve(pBFF, hdr(HeaderAllGroups, "yes", HeaderGroups, "alice"))
		if s.All {
			t.Fatal("only the exact value true may enable all-groups")
		}
	})

	t.Run("no headers means an empty scope, not an error", func(t *testing.T) {
		s, err := r.Resolve(pBFF, http.Header{})
		if err != nil || !s.IsEmpty() {
			t.Fatalf("got %+v, %v", s, err)
		}
	})

	t.Run("invalid and surplus entries narrow the scope", func(t *testing.T) {
		groups := append(manyGroups(MaxGroups+3), "Bad Name")
		s, _ := r.Resolve(pBFF, hdr(HeaderGroups, strings.Join(groups, ",")))
		if len(s.Read) != MaxGroups {
			t.Fatalf("len(Read) = %d", len(s.Read))
		}
	})

	t.Run("a default group outside the write scope cannot be created in", func(t *testing.T) {
		s, _ := r.Resolve(pBFF, hdr(HeaderGroups, "alice,shared", HeaderWritableGroups, "alice", HeaderDefaultGroup, "shared"))
		if _, err := s.ResolveCreateOwner(""); !errors.Is(err, ErrForbidden) {
			t.Fatalf("error = %v, want ErrForbidden", err)
		}
	})
}

func TestResolve_ConfiguredPrincipalsIgnoreHeaders(t *testing.T) {
	r := newTestResolver(t, true)
	evil := hdr(HeaderGroups, "victim", HeaderWritableGroups, "victim", HeaderAllGroups, "true", HeaderUserID, "root")

	s, err := r.Resolve(pCI, evil)
	if err != nil {
		t.Fatal(err)
	}
	if s.All || s.UserID != "" || !s.Service || s.CanRead("victim") {
		t.Fatalf("headers from a non-proxy leaked into the scope: %+v", s)
	}
	if !reflect.DeepEqual(s.Read, []string{"team-data", "shared"}) || !reflect.DeepEqual(s.Write, []string{"team-data"}) {
		t.Fatalf("scope must come from the config: %v %v", s.Read, s.Write)
	}
	if _, err := s.ResolveCreateOwner(""); !errors.Is(err, ErrOwnerRequired) {
		t.Fatalf("service must name the owner, got %v", err)
	}

	admin, _ := r.Resolve(pAdmin, http.Header{})
	if !admin.All || !admin.Service || !admin.CanWrite("any-group") {
		t.Fatalf("allGroups principal: %+v", admin)
	}
}

func TestResolve_ConfigSlicesAreNotShared(t *testing.T) {
	cfg := testConfig(true)
	r, _ := NewResolver(cfg)
	s, _ := r.Resolve(pCI, http.Header{})
	s.Read[0] = "tampered"
	if cfg.Principals[1].Groups[0] != "team-data" {
		t.Fatal("scope shares its slices with the config")
	}
}

func TestResolve_AssertableGroups(t *testing.T) {
	r := newTestResolver(t, true)

	s, _ := r.Resolve(pJobs, hdr(
		HeaderGroups, "team-data,team-other,team-ml",
		HeaderWritableGroups, "team-data",
		HeaderAllGroups, "true",
		HeaderUserID, "pod-1",
	))
	if !reflect.DeepEqual(s.Read, []string{"team-data", "team-ml"}) {
		t.Fatalf("asserted groups must be limited to the configured list, got %v", s.Read)
	}
	if len(s.Write) != 0 || s.All || s.CanWrite("team-data") || s.CanCreate("team-data") {
		t.Fatalf("assertable principals never write: %+v", s)
	}
	if !s.Service {
		t.Fatal("pods are service callers")
	}

	empty, _ := r.Resolve(pJobs, http.Header{})
	if !empty.IsEmpty() {
		t.Fatalf("no assertion means no access, got %+v", empty)
	}
}

func TestResolve_Unenforced(t *testing.T) {
	r := newTestResolver(t, false)

	user, _ := r.Resolve(pBFF, hdr(HeaderUserID, "u-1", HeaderDefaultGroup, "alice"))
	if !user.Unenforced || user.Service || user.UserID != "u-1" || user.LegacyGroup != "shared" {
		t.Fatalf("unexpected unenforced user scope: %+v", user)
	}
	if !user.CanRead("whatever") || !user.CanWrite("whatever") {
		t.Fatal("unenforced scope must pass every check")
	}
	if owner, err := user.ResolveCreateOwner(""); err != nil || owner != "alice" {
		t.Fatalf("owner = %q, %v; want the default group", owner, err)
	}

	service, _ := r.Resolve(pUnknown, http.Header{})
	if !service.Unenforced || !service.Service {
		t.Fatalf("unknown principal while unenforced: %+v", service)
	}
	if owner, err := service.ResolveCreateOwner(""); err != nil || owner != "shared" {
		t.Fatalf("owner = %q, %v; want the legacy group", owner, err)
	}

	// Headers from a non-proxy are still ignored while unenforced.
	ci, _ := r.Resolve(pCI, hdr(HeaderDefaultGroup, "victim", HeaderUserID, "root"))
	if ci.DefaultGroup != "" || ci.UserID != "" {
		t.Fatalf("headers from a non-proxy leaked: %+v", ci)
	}
}

func TestResolve_DoesNotModifyHeaders(t *testing.T) {
	h := hdr(HeaderGroups, "alice,Bad", HeaderAllGroups, "true")
	before := h.Clone()
	if _, err := newTestResolver(t, true).Resolve(pBFF, h); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h, before) {
		t.Fatal("Resolve modified the request headers")
	}
}

func TestResolve_LogsAnomalies(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := newTestResolver(t, true, WithLogger(logger))

	r.Resolve(pBFF, http.Header{})
	r.Resolve(pBFF, hdr(HeaderGroups, "alice,Bad Name", HeaderWritableGroups, "alice,x"))
	r.Resolve(pUnknown, http.Header{})

	out := buf.String()
	for _, want := range []string{"trusted proxy sent no scope", "trusted headers were narrowed", "principal not configured"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output does not contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Bad Name") || strings.Contains(out, "alice") {
		t.Errorf("log output must not contain group names:\n%s", out)
	}
}

func FuzzResolve(f *testing.F) {
	f.Add(0, "alice,team-data", "alice", "true", "alice")
	f.Add(1, "x,y", "x", "true", "")
	f.Add(2, "team-data,team-ml,zzz", "team-data", "true", "")
	f.Add(3, "", "", "", "")
	principals := []string{pBFF, pCI, pAdmin, pJobs, pUnknown}
	f.Fuzz(func(t *testing.T, idx int, groups, writable, all, def string) {
		if idx < 0 {
			idx = -idx
		}
		principal := principals[idx%len(principals)]
		h := http.Header{}
		h["X-User-Groups"] = []string{groups}
		h["X-User-Writable-Groups"] = []string{writable}
		h["X-User-All-Groups"] = []string{all}
		h["X-User-Default-Group"] = []string{def}

		r, _ := NewResolver(testConfig(true), WithLogger(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))))
		s, err := r.Resolve(principal, h)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Write is always a subset of Read.
		if !subset(s.Write, s.Read) {
			t.Fatalf("write %v not a subset of read %v", s.Write, s.Read)
		}
		switch principal {
		case pUnknown:
			if !s.IsEmpty() || s.All || len(s.Write) != 0 {
				t.Fatalf("unknown principal got access: %+v", s)
			}
		case pCI:
			if s.All || !reflect.DeepEqual(s.Read, []string{"team-data", "shared"}) {
				t.Fatalf("headers changed a configured scope: %+v", s)
			}
		case pJobs:
			if s.All || len(s.Write) != 0 || !subset(s.Read, []string{"team-data", "team-ml"}) {
				t.Fatalf("assertable principal exceeded its limit: %+v", s)
			}
		case pBFF:
			if s.All && strings.TrimSpace(all) != "true" {
				t.Fatalf("all-groups set for %q", all)
			}
		}
		// Whatever the scope says, an owner outside it must not be writable
		// unless all-groups is set.
		if !s.All && s.CanWrite("zz-not-in-any-list") {
			t.Fatal("scope writes a group it does not hold")
		}
	})
}
