// SPDX-License-Identifier: GPL-3.0-or-later

package ginmw_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/chrisfusion/fusion-shared/ownership"
	"github.com/chrisfusion/fusion-shared/ownership/ginmw"
)

const (
	bff = "sa/bff"
	ci  = "sa/ci"
)

func engine(t *testing.T, enforce bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	res, err := ownership.NewResolver(&ownership.Config{
		Enforce:     enforce,
		LegacyGroup: "shared",
		Principals: []ownership.PrincipalEntry{
			{Name: bff, TrustedProxy: true},
			{Name: ci, Groups: []string{"team-data"}, WritableGroups: []string{"team-data"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(ginmw.Middleware(res, func(c *gin.Context) string { return c.GetHeader("X-Test-Principal") }))
	r.GET("/things/:owner", func(c *gin.Context) {
		s := ginmw.Scope(c)
		if err := s.CheckWrite(c.Param("owner")); err != nil {
			ginmw.Abort(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"user": s.UserID, "groupsHeader": c.GetHeader(ownership.HeaderGroups), "allHeader": c.GetHeader(ownership.HeaderAllGroups),
		})
	})
	return r
}

func do(r *gin.Engine, path string, kv ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i < len(kv); i += 2 {
		req.Header.Set(kv[i], kv[i+1])
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestMiddleware_ScopeFromTrustedProxy(t *testing.T) {
	rec := do(engine(t, true), "/things/alice",
		"X-Test-Principal", bff,
		ownership.HeaderUserID, "u-1", ownership.HeaderGroups, "alice", ownership.HeaderWritableGroups, "alice",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["user"] != "u-1" {
		t.Fatalf("user = %q", body["user"])
	}
	if body["groupsHeader"] != "" || body["allHeader"] != "" {
		t.Fatalf("handler still sees trusted headers: %v", body)
	}
}

func TestMiddleware_Decisions(t *testing.T) {
	r := engine(t, true)
	tests := []struct {
		name string
		path string
		kv   []string
		want int
	}{
		{"writable group", "/things/alice", []string{"X-Test-Principal", bff, ownership.HeaderGroups, "alice,shared", ownership.HeaderWritableGroups, "alice"}, http.StatusOK},
		{"read-only group is forbidden", "/things/shared", []string{"X-Test-Principal", bff, ownership.HeaderGroups, "alice,shared", ownership.HeaderWritableGroups, "alice"}, http.StatusForbidden},
		{"foreign group is not found", "/things/other", []string{"X-Test-Principal", bff, ownership.HeaderGroups, "alice"}, http.StatusNotFound},
		{"all-groups header", "/things/anything", []string{"X-Test-Principal", bff, ownership.HeaderAllGroups, "true"}, http.StatusOK},
		{"no principal", "/things/alice", nil, http.StatusUnauthorized},
		{"unknown principal", "/things/alice", []string{"X-Test-Principal", "sa/intruder", ownership.HeaderAllGroups, "true"}, http.StatusForbidden},
		{"configured service account ignores headers", "/things/victim", []string{"X-Test-Principal", ci, ownership.HeaderAllGroups, "true", ownership.HeaderGroups, "victim"}, http.StatusNotFound},
		{"configured service account within its scope", "/things/team-data", []string{"X-Test-Principal", ci}, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rec := do(r, tc.path, tc.kv...); rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

func TestMiddleware_UnenforcedLetsEverythingThrough(t *testing.T) {
	if rec := do(engine(t, false), "/things/whatever", "X-Test-Principal", "sa/anyone"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestScope_WithoutMiddlewareIsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"empty": ginmw.Scope(c).IsEmpty()})
	})
	rec := do(r, "/")
	if rec.Body.String() != `{"empty":true}` {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestAbort_ErrorBodyIsFixed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/", func(c *gin.Context) {
		_, err := ownership.Scope{}.ResolveCreateOwner("Secret Team")
		ginmw.Abort(c, err)
	})
	rec := do(r, "/")
	if rec.Code != http.StatusBadRequest || rec.Body.String() != `{"error":"invalid owner group name"}` {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
}
