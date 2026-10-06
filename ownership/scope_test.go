// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// user: sees alice (own), team-data, shared (read-only); writes alice, team-data.
func userScope() Scope {
	return newScope(Scope{
		Principal: "sa/bff", UserID: "u-1",
		Read:         []string{"alice", "team-data", "shared"},
		Write:        []string{"alice", "team-data"},
		DefaultGroup: "alice",
	})
}

func TestScope_Decisions(t *testing.T) {
	scopes := map[string]Scope{
		"zero":       {},
		"user":       userScope(),
		"all":        newScope(Scope{All: true}),
		"unenforced": newScope(Scope{Unenforced: true}),
		"service":    newScope(Scope{Service: true, Read: []string{"a", "b"}, Write: []string{"a"}}),
	}

	type want struct{ read, write, create bool }
	tests := []struct {
		scope string
		owner string
		want  want
	}{
		// zero scope fails closed
		{"zero", "alice", want{false, false, false}},
		{"zero", "", want{false, false, false}},

		// user: own group, team group, read-only shared, foreign group, unowned, invalid
		{"user", "alice", want{true, true, true}},
		{"user", "team-data", want{true, true, true}},
		{"user", "shared", want{true, false, false}},
		{"user", "other-team", want{false, false, false}},
		{"user", "", want{false, false, false}},
		{"user", "Bad Name", want{false, false, false}},

		// all-groups: everything, including unowned; creates still need a valid name
		{"all", "other-team", want{true, true, true}},
		{"all", "", want{true, true, false}},
		{"all", "Bad Name", want{true, true, false}},

		// unenforced behaves like all for checks
		{"unenforced", "other-team", want{true, true, true}},
		{"unenforced", "", want{true, true, false}},

		// service principal
		{"service", "a", want{true, true, true}},
		{"service", "b", want{true, false, false}},
		{"service", "c", want{false, false, false}},
	}
	for _, tc := range tests {
		t.Run(tc.scope+"/"+tc.owner, func(t *testing.T) {
			s := scopes[tc.scope]
			got := want{s.CanRead(tc.owner), s.CanWrite(tc.owner), s.CanCreate(tc.owner)}
			if got != tc.want {
				t.Fatalf("read/write/create = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestScope_HandBuiltWorksLikeBuilt(t *testing.T) {
	built := userScope()
	literal := Scope{Read: built.Read, Write: built.Write} // no lookup sets
	for _, owner := range []string{"alice", "team-data", "shared", "other", ""} {
		if built.CanRead(owner) != literal.CanRead(owner) ||
			built.CanWrite(owner) != literal.CanWrite(owner) ||
			built.CanCreate(owner) != literal.CanCreate(owner) {
			t.Fatalf("hand-built scope differs for %q", owner)
		}
	}
}

func TestScope_Move(t *testing.T) {
	s := userScope()
	tests := []struct {
		name     string
		from, to string
		can      bool
		check    error
	}{
		{"own to team", "alice", "team-data", true, nil},
		{"team to own", "team-data", "alice", true, nil},
		{"read-only source", "shared", "alice", false, ErrForbidden},
		{"foreign source is invisible", "other-team", "alice", false, ErrNotVisible},
		{"into read-only group", "alice", "shared", false, ErrForbidden},
		{"into foreign group", "alice", "other-team", false, ErrForbidden},
		{"unowned source", "", "alice", false, ErrNotVisible},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.CanMove(tc.from, tc.to); got != tc.can {
				t.Fatalf("CanMove = %v, want %v", got, tc.can)
			}
			if err := s.CheckMove(tc.from, tc.to); !errors.Is(err, tc.check) && err != tc.check {
				t.Fatalf("CheckMove = %v, want %v", err, tc.check)
			}
		})
	}

	if err := s.CheckMove("alice", "Not Valid"); !errors.Is(err, ErrInvalidGroupName) {
		t.Fatalf("CheckMove to invalid name = %v, want ErrInvalidGroupName", err)
	}
	admin := newScope(Scope{All: true})
	if !admin.CanMove("a", "b") || admin.CheckMove("a", "b") != nil {
		t.Fatal("an all-groups scope may move anything")
	}
}

func TestScope_CheckReadAndWrite(t *testing.T) {
	s := userScope()
	if err := s.CheckRead("shared"); err != nil {
		t.Fatalf("CheckRead(shared) = %v", err)
	}
	if err := s.CheckRead("other-team"); err != ErrNotVisible {
		t.Fatalf("CheckRead(other-team) = %v, want ErrNotVisible", err)
	}
	if err := s.CheckWrite("alice"); err != nil {
		t.Fatalf("CheckWrite(alice) = %v", err)
	}
	if err := s.CheckWrite("shared"); err != ErrForbidden {
		t.Fatalf("CheckWrite(shared) = %v, want ErrForbidden", err)
	}
	if err := s.CheckWrite("other-team"); err != ErrNotVisible {
		t.Fatalf("CheckWrite(other-team) = %v, want ErrNotVisible", err)
	}
}

func TestScope_ResolveCreateOwner(t *testing.T) {
	roShared := newScope(Scope{Read: []string{"shared"}, DefaultGroup: "shared"})

	tests := []struct {
		name      string
		scope     Scope
		requested string
		want      string
		wantErr   error
	}{
		{"user names a writable group", userScope(), "team-data", "team-data", nil},
		{"user without request gets the default", userScope(), "", "alice", nil},
		{"user names a read-only group", userScope(), "shared", "", ErrForbidden},
		{"user names a foreign group", userScope(), "other-team", "", ErrForbidden},
		{"user names an invalid group", userScope(), "Bad Name", "", ErrInvalidGroupName},
		{"default group is read-only", roShared, "", "", ErrForbidden},
		{"no default and no request", newScope(Scope{Write: []string{"a"}, Read: []string{"a"}}), "", "", ErrOwnerRequired},
		{"zero scope", Scope{}, "a", "", ErrForbidden},

		{"service must name the group", newScope(Scope{Service: true, Read: []string{"a"}, Write: []string{"a"}}), "", "", ErrOwnerRequired},
		{"service ignores a default group", newScope(Scope{Service: true, DefaultGroup: "a", Read: []string{"a"}, Write: []string{"a"}}), "", "", ErrOwnerRequired},
		{"service names a writable group", newScope(Scope{Service: true, Read: []string{"a"}, Write: []string{"a"}}), "a", "a", nil},
		{"service names a read-only group", newScope(Scope{Service: true, Read: []string{"a"}}), "a", "", ErrForbidden},

		{"all-groups may name any valid group", newScope(Scope{All: true}), "anything", "anything", nil},
		{"all-groups still needs a valid name", newScope(Scope{All: true}), "Bad Name", "", ErrInvalidGroupName},

		{"unenforced uses the requested group", newScope(Scope{Unenforced: true, LegacyGroup: "shared"}), "team-data", "team-data", nil},
		{"unenforced user falls back to default", newScope(Scope{Unenforced: true, LegacyGroup: "shared", DefaultGroup: "alice"}), "", "alice", nil},
		{"unenforced user without default gets legacy", newScope(Scope{Unenforced: true, LegacyGroup: "shared"}), "", "shared", nil},
		{"unenforced service gets legacy", newScope(Scope{Unenforced: true, LegacyGroup: "shared", Service: true, DefaultGroup: "alice"}), "", "shared", nil},
		{"unenforced without any owner", newScope(Scope{Unenforced: true}), "", "", ErrOwnerRequired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.scope.ResolveCreateOwner(tc.requested)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				if got != "" {
					t.Fatalf("owner %q returned with an error", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestScope_IsEmptyAndSees(t *testing.T) {
	if !(Scope{}).IsEmpty() {
		t.Fatal("zero scope must be empty")
	}
	if userScope().IsEmpty() || newScope(Scope{All: true}).IsEmpty() || newScope(Scope{Unenforced: true}).IsEmpty() {
		t.Fatal("non-empty scopes reported empty")
	}
	if userScope().Sees() || !newScope(Scope{All: true}).Sees() || !newScope(Scope{Unenforced: true}).Sees() {
		t.Fatal("Sees() wrong")
	}
}

func TestScope_DuplicatesAreHarmless(t *testing.T) {
	s := newScope(Scope{Read: []string{"a", "a", "b"}, Write: []string{"a", "a"}})
	if len(s.Read) != 2 || len(s.Write) != 1 {
		t.Fatalf("not deduplicated: %+v %+v", s.Read, s.Write)
	}
}

func TestHTTPStatus(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, http.StatusOK},
		{ErrNotVisible, http.StatusNotFound},
		{ErrForbidden, http.StatusForbidden},
		{ErrOwnerRequired, http.StatusBadRequest},
		{ErrInvalidGroupName, http.StatusBadRequest},
		{ErrNoPrincipal, http.StatusUnauthorized},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		if got := HTTPStatus(tc.err); got != tc.want {
			t.Errorf("HTTPStatus(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
	wrapped := errors.Join(errors.New("ctx"), ErrForbidden)
	if HTTPStatus(wrapped) != http.StatusForbidden {
		t.Error("HTTPStatus must see through wrapped errors")
	}
	_, err := userScope().ResolveCreateOwner("Bad Name")
	if HTTPStatus(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "invalid owner group name") {
		t.Errorf("unexpected invalid-name error: %v", err)
	}
}

func BenchmarkScope_CanRead(b *testing.B) {
	groups := manyGroups(MaxGroups)
	s := newScope(Scope{Read: groups, Write: groups[:50]})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = s.CanRead("group-099")
		_ = s.CanWrite("group-049")
		_ = s.CanRead("not-there")
	}
}
