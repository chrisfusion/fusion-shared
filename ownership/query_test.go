// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected a panic", name)
		}
	}()
	f()
}

func TestSQLClause(t *testing.T) {
	user := newScope(Scope{Read: []string{"alice", "shared"}, Write: []string{"alice"}})

	t.Run("group filter", func(t *testing.T) {
		clause, args := SQLClause(user, ColumnOwnerGroup, 1)
		if clause != "owner_group = ANY($1)" {
			t.Fatalf("clause = %q", clause)
		}
		if len(args) != 1 || !reflect.DeepEqual(args[0], []string{"alice", "shared"}) {
			t.Fatalf("args = %#v", args)
		}
	})

	t.Run("placeholder position follows argPos", func(t *testing.T) {
		clause, _ := SQLClause(user, "r.owner_group", 4)
		if clause != "r.owner_group = ANY($4)" {
			t.Fatalf("clause = %q", clause)
		}
	})

	t.Run("no filter for all-groups and unenforced", func(t *testing.T) {
		for name, s := range map[string]Scope{"all": newScope(Scope{All: true}), "unenforced": newScope(Scope{Unenforced: true})} {
			clause, args := SQLClause(s, ColumnOwnerGroup, 1)
			if clause != "TRUE" || args != nil {
				t.Errorf("%s: clause=%q args=%v", name, clause, args)
			}
		}
	})

	t.Run("empty scope matches nothing", func(t *testing.T) {
		clause, args := SQLClause(Scope{}, ColumnOwnerGroup, 1)
		if clause != "FALSE" || args != nil {
			t.Fatalf("clause=%q args=%v", clause, args)
		}
	})

	t.Run("rejects identifiers that are not plain columns", func(t *testing.T) {
		for _, col := range []string{"", "owner_group; DROP TABLE x", "a b", "1col", `"x"`, "a.b.c", "a..b", ".a", "a.", "x)--", "é"} {
			mustPanic(t, fmt.Sprintf("col %q", col), func() { SQLClause(user, col, 1) })
		}
		for _, col := range []string{"owner_group", "t.owner_group", "_x1", "Owner"} {
			SQLClause(user, col, 1) // must not panic
		}
	})

	t.Run("rejects a position below one", func(t *testing.T) {
		mustPanic(t, "argPos 0", func() { SQLClause(user, ColumnOwnerGroup, 0) })
	})
}

// parseSelector extracts the group list from "key in (a,b,c)".
func parseSelector(t *testing.T, sel, key string) []string {
	t.Helper()
	prefix, suffix := key+" in (", ")"
	if !strings.HasPrefix(sel, prefix) || !strings.HasSuffix(sel, suffix) {
		t.Fatalf("selector %q has an unexpected shape", sel)
	}
	return strings.Split(strings.TrimSuffix(strings.TrimPrefix(sel, prefix), suffix), ",")
}

func TestLabelSelector(t *testing.T) {
	t.Run("set-based selector", func(t *testing.T) {
		s := newScope(Scope{Read: []string{"alice", "team-data", "shared"}})
		sel, kind := LabelSelector(s, LabelOwnerGroup)
		if kind != SelectLabels || sel != "fusion-platform.io/owner-group in (alice,team-data,shared)" {
			t.Fatalf("sel=%q kind=%v", sel, kind)
		}
	})

	t.Run("single group", func(t *testing.T) {
		sel, kind := LabelSelector(newScope(Scope{Read: []string{"a"}}), "owner")
		if kind != SelectLabels || sel != "owner in (a)" {
			t.Fatalf("sel=%q kind=%v", sel, kind)
		}
	})

	t.Run("all-groups, unenforced and empty", func(t *testing.T) {
		if sel, kind := LabelSelector(newScope(Scope{All: true}), LabelOwnerGroup); sel != "" || kind != SelectAll {
			t.Errorf("all: %q %v", sel, kind)
		}
		if sel, kind := LabelSelector(newScope(Scope{Unenforced: true}), LabelOwnerGroup); sel != "" || kind != SelectAll {
			t.Errorf("unenforced: %q %v", sel, kind)
		}
		if sel, kind := LabelSelector(Scope{}, LabelOwnerGroup); sel != "" || kind != SelectNone {
			t.Errorf("empty: %q %v", sel, kind)
		}
	})

	t.Run("limit is exact", func(t *testing.T) {
		key := "k"
		base := len(key) + len(" in (") + len(")")
		// two groups: one name carries the length so the selector is exactly the limit
		fill := MaxSelectorLen - base - 1 /* comma */ - 1 /* "a" */
		long := strings.Repeat("b", fill)
		sel, kind := LabelSelector(newScope(Scope{Read: []string{"a", long}}), key)
		if kind != SelectLabels || len(sel) != MaxSelectorLen {
			t.Fatalf("at the limit: kind=%v len=%d", kind, len(sel))
		}
		sel, kind = LabelSelector(newScope(Scope{Read: []string{"a", long + "b"}}), key)
		if kind != SelectInMemory || sel != "" {
			t.Fatalf("over the limit: kind=%v sel=%q", kind, sel)
		}
	})

	t.Run("100 long group names fall back to in-memory", func(t *testing.T) {
		groups := make([]string, MaxGroups)
		for i := range groups {
			groups[i] = fmt.Sprintf("%s-%03d", strings.Repeat("g", 50), i)
		}
		if _, kind := LabelSelector(newScope(Scope{Read: groups}), LabelOwnerGroup); kind != SelectInMemory {
			t.Fatalf("kind = %v, want in-memory", kind)
		}
	})

	t.Run("selector lists exactly the read groups", func(t *testing.T) {
		read := manyGroups(20)
		sel, kind := LabelSelector(newScope(Scope{Read: read}), LabelOwnerGroup)
		if kind != SelectLabels {
			t.Fatal(kind)
		}
		if got := parseSelector(t, sel, LabelOwnerGroup); !reflect.DeepEqual(got, read) {
			t.Fatalf("selector groups differ: %v", got)
		}
	})

	t.Run("rejects malformed keys", func(t *testing.T) {
		for _, key := range []string{"", "a b", "a/b/c", "/a", "a/", "x in (y)", "a,b", "é"} {
			mustPanic(t, fmt.Sprintf("key %q", key), func() { LabelSelector(newScope(Scope{Read: []string{"a"}}), key) })
		}
	})
}

func TestSelectorKindString(t *testing.T) {
	for kind, want := range map[SelectorKind]string{
		SelectAll: "all", SelectNone: "none", SelectLabels: "labels", SelectInMemory: "in-memory", SelectorKind(9): "unknown(9)",
	} {
		if kind.String() != want {
			t.Errorf("%d.String() = %q, want %q", kind, kind.String(), want)
		}
	}
}

type item struct{ name, owner string }

func TestFilter(t *testing.T) {
	items := []item{{"1", "alice"}, {"2", "other"}, {"3", "shared"}, {"4", ""}, {"5", "alice"}}
	owner := func(i item) string { return i.owner }
	user := newScope(Scope{Read: []string{"alice", "shared"}})

	got := Filter(user, items, owner)
	want := []item{{"1", "alice"}, {"3", "shared"}, {"5", "alice"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(items) != 5 || items[1].owner != "other" {
		t.Fatal("input was modified")
	}

	if all := Filter(newScope(Scope{All: true}), items, owner); !reflect.DeepEqual(all, items) {
		t.Fatalf("all-groups must keep every item, got %v", all)
	}
	if none := Filter(Scope{}, items, owner); len(none) != 0 || none == nil {
		t.Fatalf("empty scope must give an empty, non-nil list, got %#v", none)
	}

	// Filter agrees with CanRead for every item.
	for _, it := range items {
		inResult := false
		for _, g := range got {
			inResult = inResult || g == it
		}
		if inResult != user.CanRead(it.owner) {
			t.Errorf("Filter and CanRead disagree for %v", it)
		}
	}
}

func TestOwnerLabelHelpers(t *testing.T) {
	if OwnerFromLabels(nil) != "" || OwnerFromLabels(map[string]string{"x": "y"}) != "" {
		t.Fatal("missing label must give an empty owner")
	}
	labels := WithOwnerLabel(nil, "alice")
	if labels[LabelOwnerGroup] != "alice" || OwnerFromLabels(labels) != "alice" {
		t.Fatalf("labels = %v", labels)
	}
	existing := map[string]string{"app": "x"}
	got := WithOwnerLabel(existing, "team-data")
	if got["app"] != "x" || got[LabelOwnerGroup] != "team-data" {
		t.Fatalf("labels = %v", got)
	}
	if LabelOwnerGroup != "fusion-platform.io/owner-group" || ColumnOwnerGroup != "owner_group" {
		t.Fatal("stored names changed; services depend on them")
	}
}

func BenchmarkSQLClause(b *testing.B) {
	s := newScope(Scope{Read: manyGroups(MaxGroups)})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SQLClause(s, ColumnOwnerGroup, 1)
	}
}

func BenchmarkLabelSelector(b *testing.B) {
	for _, n := range []int{20, MaxGroups} {
		s := newScope(Scope{Read: manyGroups(n)})
		b.Run(fmt.Sprintf("groups=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				LabelSelector(s, LabelOwnerGroup)
			}
		})
	}
}

// W35: 50,000 resources per service, 20 groups for a typical user.
func BenchmarkFilter50k(b *testing.B) {
	groups := manyGroups(200)
	items := make([]item, 50000)
	for i := range items {
		items[i] = item{owner: groups[i%len(groups)]}
	}
	s := newScope(Scope{Read: groups[:20]})
	owner := func(i item) string { return i.owner }
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Filter(s, items, owner)
	}
}
