// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"fmt"
	"strconv"
	"strings"
)

// Names every service uses for the stored owner, so they are written once.
const (
	// LabelOwnerGroup is the label that carries the owner group on Kubernetes resources.
	LabelOwnerGroup = "fusion-platform.io/owner-group"
	// ColumnOwnerGroup is the column that carries the owner group on database rows.
	ColumnOwnerGroup = "owner_group"
)

// MaxSelectorLen is the longest label selector LabelSelector returns. Longer
// ones are reported as SelectInMemory so the caller filters with Filter.
const MaxSelectorLen = 4096

// SelectorKind says how a list query must apply a Scope.
type SelectorKind int

const (
	// SelectAll: no owner filter is needed (all-groups scope or enforcement off).
	SelectAll SelectorKind = iota
	// SelectNone: the scope sees nothing; answer with an empty list without querying.
	SelectNone
	// SelectLabels: pass the returned label selector to the Kubernetes list call.
	SelectLabels
	// SelectInMemory: the selector would be too long; list without it and filter
	// the result with Filter.
	SelectInMemory
)

func (k SelectorKind) String() string {
	switch k {
	case SelectAll:
		return "all"
	case SelectNone:
		return "none"
	case SelectLabels:
		return "labels"
	case SelectInMemory:
		return "in-memory"
	}
	return "unknown(" + strconv.Itoa(int(k)) + ")"
}

// SQLClause returns a WHERE condition that limits rows to the scope, and its
// arguments. argPos is the 1-based placeholder number of the first argument.
// Combine it with other conditions using AND; the result is always safe to
// parenthesise:
//
//	clause, args := ownership.SQLClause(scope, "owner_group", len(existingArgs)+1)
//	query := "SELECT ... WHERE (" + clause + ") AND ..."
//
// All-groups and unenforced scopes give "TRUE", an empty scope gives "FALSE"
// (both without arguments), anything else "col = ANY($n)" with one []string
// argument. Rows with a NULL owner never match a group filter (fail closed).
// Apply it before pagination so pages and counts are right.
//
// col must be a constant identifier, never user input: SQLClause panics on
// anything that is not a plain (optionally table-qualified) identifier or on
// argPos < 1, so a mistake surfaces in the first test run.
func SQLClause(s Scope, col string, argPos int) (clause string, args []any) {
	if !validIdent(col) {
		panic(fmt.Sprintf("ownership: SQLClause: %q is not a plain column identifier", col))
	}
	if argPos < 1 {
		panic("ownership: SQLClause: argPos must be >= 1")
	}
	switch {
	case s.Sees():
		return "TRUE", nil
	case len(s.Read) == 0:
		return "FALSE", nil
	}
	return col + " = ANY($" + strconv.Itoa(argPos) + ")", []any{s.Read}
}

// LabelSelector returns the Kubernetes label selector for the scope, e.g.
// "fusion-platform.io/owner-group in (alice,shared)", and how to use it (see
// SelectorKind). The selector string is only set for SelectLabels. key must be a
// constant label key (use LabelOwnerGroup); LabelSelector panics on a malformed
// key.
func LabelSelector(s Scope, key string) (selector string, kind SelectorKind) {
	if !validLabelKey(key) {
		panic(fmt.Sprintf("ownership: LabelSelector: %q is not a valid label key", key))
	}
	switch {
	case s.Sees():
		return "", SelectAll
	case len(s.Read) == 0:
		return "", SelectNone
	}
	// key + " in (" + names joined by "," + ")"
	n := len(key) + len(" in (") + len(")") + len(s.Read) - 1
	for _, g := range s.Read {
		n += len(g)
	}
	if n > MaxSelectorLen {
		return "", SelectInMemory
	}
	var b strings.Builder
	b.Grow(n)
	b.WriteString(key)
	b.WriteString(" in (")
	for i, g := range s.Read {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(g)
	}
	b.WriteByte(')')
	return b.String(), SelectLabels
}

// Filter returns the items the scope may read, in their original order. Use it
// for SelectInMemory, or whenever a list was fetched without an owner filter.
// The input is not modified; with a scope that sees everything it is returned
// as is.
func Filter[T any](s Scope, items []T, owner func(T) string) []T {
	if s.Sees() {
		return items
	}
	out := make([]T, 0, len(items))
	for _, it := range items {
		if s.CanRead(owner(it)) {
			out = append(out, it)
		}
	}
	return out
}

// OwnerFromLabels returns the owner group stored in labels ("" if none).
func OwnerFromLabels(labels map[string]string) string {
	return labels[LabelOwnerGroup]
}

// WithOwnerLabel returns labels with the owner label set, allocating the map if
// it is nil. The given map is modified and returned.
func WithOwnerLabel(labels map[string]string, owner string) map[string]string {
	if labels == nil {
		labels = make(map[string]string, 1)
	}
	labels[LabelOwnerGroup] = owner
	return labels
}

// validIdent accepts "name" or "table.name" with name made of letters, digits
// and underscores and not starting with a digit.
func validIdent(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || (p[0] >= '0' && p[0] <= '9') {
			return false
		}
		for i := 0; i < len(p); i++ {
			c := p[i]
			if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
				return false
			}
		}
	}
	return true
}

// validLabelKey accepts "name" or "prefix/name" built from letters, digits and
// . _ - characters, which is stricter than Kubernetes but covers our keys.
func validLabelKey(s string) bool {
	if s == "" || len(s) > 253+1+63 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c == '.' || c == '_' || c == '-' || c == '/' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return strings.Count(s, "/") <= 1 && !strings.HasPrefix(s, "/") && !strings.HasSuffix(s, "/")
}
