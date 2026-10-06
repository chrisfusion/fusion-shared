// SPDX-License-Identifier: GPL-3.0-or-later

package ownership_test

import (
	"fmt"
	"net/http"

	"github.com/chrisfusion/fusion-shared/ownership"
)

func ExampleParse() {
	cfg, err := ownership.Parse([]byte(`
enforce: true
legacyGroup: shared
principals:
  - name: sa/system:serviceaccount:fusion:fusion-bff
    trustedProxy: true
  - name: sa/system:serviceaccount:fusion:ci
    groups: [team-data, shared]
    writableGroups: [team-data]
`))
	fmt.Println(err, cfg.Enforce, len(cfg.Principals))
	fmt.Println(cfg.Validate(false)) // enforcing without authentication must not start
	// Output:
	// <nil> true 2
	// ownership enforcement requires authentication to be enabled
}

func ExampleSQLClause() {
	scope := ownership.Scope{Read: []string{"alice", "shared"}, Write: []string{"alice"}}
	clause, args := ownership.SQLClause(scope, ownership.ColumnOwnerGroup, 1)
	fmt.Println(clause)
	fmt.Println(args...)
	// Output:
	// owner_group = ANY($1)
	// [alice shared]
}

func ExampleLabelSelector() {
	scope := ownership.Scope{Read: []string{"alice", "shared"}}
	selector, kind := ownership.LabelSelector(scope, ownership.LabelOwnerGroup)
	fmt.Println(selector, kind)
	// Output: fusion-platform.io/owner-group in (alice,shared) labels
}

func ExampleFilter() {
	type run struct{ name, owner string }
	runs := []run{{"a", "alice"}, {"b", "other"}, {"c", "shared"}}
	scope := ownership.Scope{Read: []string{"alice", "shared"}}
	for _, r := range ownership.Filter(scope, runs, func(r run) string { return r.owner }) {
		fmt.Println(r.name)
	}
	// Output:
	// a
	// c
}

func ExampleScope_ResolveCreateOwner() {
	user := ownership.Scope{
		Read: []string{"alice", "shared"}, Write: []string{"alice"}, DefaultGroup: "alice",
	}
	owner, err := user.ResolveCreateOwner("")
	fmt.Println(owner, err)

	_, err = user.ResolveCreateOwner("shared") // read-only group
	fmt.Println(err, ownership.HTTPStatus(err))
	// Output:
	// alice <nil>
	// write access to the owner group denied 403
}

func ExampleScope_CheckWrite() {
	user := ownership.Scope{Read: []string{"alice", "shared"}, Write: []string{"alice"}}
	for _, owner := range []string{"alice", "shared", "other"} {
		err := user.CheckWrite(owner)
		fmt.Println(owner, err, ownership.HTTPStatus(err))
	}
	// Output:
	// alice <nil> 200
	// shared write access to the owner group denied 403
	// other resource not visible 404
}

func ExampleFormatHeaders() {
	// What the BFF sets on a call to an upstream service.
	h := http.Header{}
	groups, _ := ownership.CapGroups([]string{"shared", "team-data", "alice"}, "alice")
	err := ownership.FormatHeaders(h, ownership.Headers{
		UserID: "u-1", Groups: groups, Writable: []string{"alice"}, DefaultGroup: "alice",
	})
	fmt.Println(err)
	for _, k := range []string{
		ownership.HeaderUserID, ownership.HeaderGroups, ownership.HeaderWritableGroups,
		ownership.HeaderAllGroups, ownership.HeaderDefaultGroup,
	} {
		fmt.Printf("%s: %q\n", k, h.Get(k))
	}
	// Output:
	// <nil>
	// X-User-ID: "u-1"
	// X-User-Groups: "alice,shared,team-data"
	// X-User-Writable-Groups: "alice"
	// X-User-All-Groups: ""
	// X-User-Default-Group: "alice"
}

// Wiring with net/http or chi: install the middleware after authentication.
func ExampleResolver_Middleware() {
	cfg, _ := ownership.Parse([]byte("enforce: true\nprincipals:\n  - {name: sa/bff, trustedProxy: true}\n"))
	res, _ := ownership.NewResolver(cfg)

	principal := func(r *http.Request) string { return r.Header.Get("X-Authenticated-Principal") }
	handler := res.Middleware(principal)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, _ := ownership.FromContext(r.Context())
		_ = scope.CheckRead("alice") // 404 via ownership.WriteError when not visible
	}))
	_ = handler
}
