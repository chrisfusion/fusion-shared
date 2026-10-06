// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"errors"
	"fmt"
	"net/http"
)

// Errors returned by scope decisions. Map them to HTTP with HTTPStatus.
var (
	// ErrNotVisible: the resource is outside the caller's read scope. Answer 404
	// so the existence of other groups' resources is not revealed.
	ErrNotVisible = errors.New("resource not visible")
	// ErrForbidden: the caller can see the resource or group but may not write it.
	ErrForbidden = errors.New("write access to the owner group denied")
	// ErrOwnerRequired: a create named no owner group and none could be defaulted.
	ErrOwnerRequired = errors.New("an owner group must be named")
)

// Scope is what a caller may do: which owner groups it can see and write, or
// everything. The zero Scope sees and writes nothing (fail closed).
//
// Build scopes with Resolver.Resolve. A Scope may also be filled by hand (for
// example in tests); do not modify its fields after first use.
type Scope struct {
	// Principal is the authenticated caller (service account, API key, ...),
	// for audit logs. UserID is the end user for user requests.
	Principal string
	UserID    string

	// Read lists the owner groups the caller may see; Write the subset it may
	// change. Write must be a subset of Read.
	Read  []string
	Write []string

	// All grants read and write on every owner group (all-groups permission).
	All bool

	// DefaultGroup is the owner used when a user's create names none.
	DefaultGroup string

	// Service marks a non-user caller. It must name the owner on create.
	Service bool

	// Unenforced means enforcement is switched off: every check passes and
	// creates still record an owner (requested, default, else LegacyGroup).
	Unenforced  bool
	LegacyGroup string

	readSet, writeSet map[string]struct{}
}

// newScope returns s with duplicate-free group lists and lookup sets built once.
func newScope(s Scope) Scope {
	s.Read, s.readSet = dedupe(s.Read)
	s.Write, s.writeSet = dedupe(s.Write)
	return s
}

// IsEmpty reports whether the scope can see nothing at all.
func (s Scope) IsEmpty() bool {
	return !s.All && !s.Unenforced && len(s.Read) == 0
}

// Sees reports whether the scope may read resources of every group, so queries
// need no owner filter.
func (s Scope) Sees() (everything bool) {
	return s.All || s.Unenforced
}

// CanRead reports whether a resource owned by owner is visible. A resource
// without an owner (not yet migrated) is visible only to an all-groups scope.
func (s Scope) CanRead(owner string) bool {
	if s.Sees() {
		return true
	}
	return owner != "" && contains(s.readSet, s.Read, owner)
}

// CanWrite reports whether a resource owned by owner may be changed or deleted.
func (s Scope) CanWrite(owner string) bool {
	if s.Sees() {
		return true
	}
	return owner != "" && contains(s.writeSet, s.Write, owner)
}

// CanCreate reports whether a new resource may be given to owner. The owner
// must be a valid group name; an all-groups scope may name any group.
func (s Scope) CanCreate(owner string) bool {
	if !ValidGroupName(owner) {
		return false
	}
	if s.Sees() {
		return true
	}
	return contains(s.writeSet, s.Write, owner)
}

// CanMove reports whether a resource may be moved from one group to another:
// write on the source and write on the target.
func (s Scope) CanMove(from, to string) bool {
	return s.CanWrite(from) && s.CanCreate(to)
}

// CheckRead returns ErrNotVisible if the resource is outside the read scope.
func (s Scope) CheckRead(owner string) error {
	if s.CanRead(owner) {
		return nil
	}
	return ErrNotVisible
}

// CheckWrite returns ErrNotVisible for resources outside the read scope and
// ErrForbidden for visible but read-only ones.
func (s Scope) CheckWrite(owner string) error {
	if !s.CanRead(owner) {
		return ErrNotVisible
	}
	if !s.CanWrite(owner) {
		return ErrForbidden
	}
	return nil
}

// CheckMove checks a move from one owner group to another: the source must be
// writable (see CheckWrite) and the target must accept creates.
func (s Scope) CheckMove(from, to string) error {
	if err := s.CheckWrite(from); err != nil {
		return err
	}
	if !ValidGroupName(to) {
		return fmt.Errorf("%w: %s", ErrInvalidGroupName, groupNameProblem(to))
	}
	if !s.CanCreate(to) {
		return ErrForbidden
	}
	return nil
}

// ResolveCreateOwner decides the owner of a new resource.
//
// With a requested group it must be valid and creatable (ErrInvalidGroupName,
// ErrForbidden). Without one, a user falls back to DefaultGroup; a service
// caller must always name the group (ErrOwnerRequired). With enforcement off the
// fallback is DefaultGroup (users) and then LegacyGroup, so an owner is recorded
// even before enforcement is switched on.
func (s Scope) ResolveCreateOwner(requested string) (string, error) {
	owner := requested
	if owner == "" {
		owner = s.defaultOwner()
		if owner == "" {
			return "", ErrOwnerRequired
		}
	}
	if !ValidGroupName(owner) {
		return "", fmt.Errorf("%w: %s", ErrInvalidGroupName, groupNameProblem(owner))
	}
	if !s.CanCreate(owner) {
		return "", ErrForbidden
	}
	return owner, nil
}

func (s Scope) defaultOwner() string {
	if !s.Service && s.DefaultGroup != "" {
		return s.DefaultGroup
	}
	if s.Unenforced {
		return s.LegacyGroup
	}
	return ""
}

// HTTPStatus maps an ownership error to an HTTP status code: 404 for
// ErrNotVisible, 403 for ErrForbidden, 400 for ErrOwnerRequired and
// ErrInvalidGroupName, 401 for ErrNoPrincipal, 500 for anything else.
func HTTPStatus(err error) int {
	switch {
	case errors.Is(err, ErrNotVisible):
		return http.StatusNotFound
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrOwnerRequired), errors.Is(err, ErrInvalidGroupName):
		return http.StatusBadRequest
	case errors.Is(err, ErrNoPrincipal):
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// contains uses the prebuilt set when there is one and falls back to a scan so
// hand-built scopes work too.
func contains(set map[string]struct{}, list []string, name string) bool {
	if set != nil {
		_, ok := set[name]
		return ok
	}
	for _, g := range list {
		if g == name {
			return true
		}
	}
	return false
}

// dedupe returns list without duplicates (order kept) and a lookup set.
func dedupe(list []string) ([]string, map[string]struct{}) {
	set := make(map[string]struct{}, len(list))
	out := make([]string, 0, len(list))
	for _, g := range list {
		if _, dup := set[g]; dup {
			continue
		}
		set[g] = struct{}{}
		out = append(out, g)
	}
	return out, set
}

// intersect returns the elements of a that are also in b, in a's order.
func intersect(a, b []string) []string {
	in := make(map[string]struct{}, len(b))
	for _, g := range b {
		in[g] = struct{}{}
	}
	out := make([]string, 0, len(a))
	for _, g := range a {
		if _, ok := in[g]; ok {
			out = append(out, g)
		}
	}
	return out
}
