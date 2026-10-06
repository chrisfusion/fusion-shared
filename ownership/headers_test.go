// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func manyGroups(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("group-%03d", i)
	}
	return out
}

func TestParseHeaders(t *testing.T) {
	t.Run("full set", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderUserID, "u-1")
		h.Set(HeaderUserEmail, "a@corp.com")
		h.Set(HeaderGroups, "alice, team-data ,shared")
		h.Set(HeaderWritableGroups, "alice,team-data")
		h.Set(HeaderAllGroups, "true")
		h.Set(HeaderDefaultGroup, "alice")

		got := ParseHeaders(h)
		want := Headers{
			UserID: "u-1", Email: "a@corp.com",
			Groups:   []string{"alice", "team-data", "shared"},
			Writable: []string{"alice", "team-data"},
			All:      true, DefaultGroup: "alice",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("empty request", func(t *testing.T) {
		got := ParseHeaders(http.Header{})
		if got.All || got.UserID != "" || len(got.Groups) != 0 || len(got.Writable) != 0 || got.DefaultGroup != "" {
			t.Fatalf("expected zero value, got %+v", got)
		}
	})

	t.Run("repeated header values are combined", func(t *testing.T) {
		h := http.Header{}
		h.Add(HeaderGroups, "a")
		h.Add(HeaderGroups, "b,c")
		if got := ParseHeaders(h).Groups; !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("duplicates and empty entries are dropped silently", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderGroups, "a,,a, ,b,a")
		got := ParseHeaders(h)
		if !reflect.DeepEqual(got.Groups, []string{"a", "b"}) || got.Invalid != 0 || got.Truncated {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("invalid names are dropped and counted", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderGroups, "ok,Bad,-x,ä,fine")
		h.Set(HeaderWritableGroups, "ok,UPPER")
		got := ParseHeaders(h)
		if !reflect.DeepEqual(got.Groups, []string{"ok", "fine"}) || !reflect.DeepEqual(got.Writable, []string{"ok"}) {
			t.Fatalf("got %+v", got)
		}
		if got.Invalid != 4 {
			t.Fatalf("Invalid = %d, want 4", got.Invalid)
		}
	})

	t.Run("all-groups flag only for exact true", func(t *testing.T) {
		for value, want := range map[string]bool{
			"true": true, " true ": true,
			"TRUE": false, "True": false, "1": false, "yes": false, "": false, "true,false": false,
		} {
			h := http.Header{}
			h.Set(HeaderAllGroups, value)
			if got := ParseHeaders(h).All; got != want {
				t.Errorf("All for %q = %v, want %v", value, got, want)
			}
		}
	})

	t.Run("invalid default group is ignored", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderDefaultGroup, "Not Valid")
		if got := ParseHeaders(h).DefaultGroup; got != "" {
			t.Fatalf("DefaultGroup = %q, want empty", got)
		}
	})

	t.Run("exactly MaxGroups is not truncated", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderGroups, strings.Join(manyGroups(MaxGroups), ","))
		got := ParseHeaders(h)
		if len(got.Groups) != MaxGroups || got.Truncated {
			t.Fatalf("len=%d truncated=%v", len(got.Groups), got.Truncated)
		}
	})

	t.Run("more than MaxGroups is truncated", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderGroups, strings.Join(manyGroups(MaxGroups+5), ","))
		got := ParseHeaders(h)
		if len(got.Groups) != MaxGroups || !got.Truncated {
			t.Fatalf("len=%d truncated=%v", len(got.Groups), got.Truncated)
		}
		if got.Groups[0] != "group-000" {
			t.Fatalf("order not kept, first = %q", got.Groups[0])
		}
	})

	t.Run("duplicates beyond the cap do not set truncated", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderGroups, strings.Join(manyGroups(MaxGroups), ",")+",group-000")
		if ParseHeaders(h).Truncated {
			t.Fatal("a repeated entry must not count as truncation")
		}
	})
}

func TestStripHeaders(t *testing.T) {
	h := http.Header{}
	for _, k := range []string{HeaderUserID, HeaderUserEmail, HeaderGroups, HeaderWritableGroups, HeaderAllGroups, HeaderDefaultGroup} {
		h.Set(k, "x")
	}
	h.Set("X-Other", "keep")
	StripHeaders(h)
	if len(h) != 1 || h.Get("X-Other") != "keep" {
		t.Fatalf("unexpected headers after strip: %v", h)
	}
}

func TestFormatHeaders(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		in := Headers{
			UserID: "u-1", Email: "a@corp.com",
			Groups: []string{"alice", "shared"}, Writable: []string{"alice"},
			All: true, DefaultGroup: "alice",
		}
		h := http.Header{}
		if err := FormatHeaders(h, in); err != nil {
			t.Fatal(err)
		}
		if got := ParseHeaders(h); !reflect.DeepEqual(got, in) {
			t.Fatalf("round trip: got %+v, want %+v", got, in)
		}
	})

	t.Run("replaces client-supplied values", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderGroups, "victim-team")
		h.Set(HeaderAllGroups, "true")
		h.Set(HeaderWritableGroups, "victim-team")
		if err := FormatHeaders(h, Headers{UserID: "u-1", Groups: []string{"alice"}}); err != nil {
			t.Fatal(err)
		}
		got := ParseHeaders(h)
		if got.All || len(got.Writable) != 0 || !reflect.DeepEqual(got.Groups, []string{"alice"}) {
			t.Fatalf("client values leaked: %+v", got)
		}
	})

	t.Run("omits empty values", func(t *testing.T) {
		h := http.Header{}
		if err := FormatHeaders(h, Headers{UserID: "u-1"}); err != nil {
			t.Fatal(err)
		}
		if len(h) != 1 {
			t.Fatalf("unexpected headers: %v", h)
		}
	})

	invalid := map[string]Headers{
		"invalid group":          {Groups: []string{"ok", "Bad"}},
		"invalid writable group": {Writable: []string{"a,b"}},
		"invalid default group":  {DefaultGroup: "-x"},
		"too many groups":        {Groups: manyGroups(MaxGroups + 1)},
		"newline in user id":     {UserID: "u\r\nX-User-All-Groups: true"},
		"newline in email":       {Email: "a@b\n"},
		"nul in user id":         {UserID: "u\x00"},
	}
	for name, v := range invalid {
		t.Run("rejects "+name, func(t *testing.T) {
			h := http.Header{}
			h.Set(HeaderGroups, "pre-existing")
			err := FormatHeaders(h, v)
			if !errors.Is(err, ErrInvalidHeaders) {
				t.Fatalf("error = %v, want ErrInvalidHeaders", err)
			}
			if h.Get(HeaderGroups) != "pre-existing" || len(h) != 1 {
				t.Fatalf("headers were modified on error: %v", h)
			}
		})
	}
}

func TestCapGroups(t *testing.T) {
	t.Run("under the cap keeps priority first then alphabetical", func(t *testing.T) {
		got, cut := CapGroups([]string{"zeta", "alpha", "alice", "mid"}, "alice", "mid")
		if cut || !reflect.DeepEqual(got, []string{"alice", "mid", "alpha", "zeta"}) {
			t.Fatalf("got %v cut=%v", got, cut)
		}
	})

	t.Run("priority entries that are not present are ignored", func(t *testing.T) {
		got, _ := CapGroups([]string{"b", "a"}, "missing", "b", "b")
		if !reflect.DeepEqual(got, []string{"b", "a"}) {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("duplicates are removed", func(t *testing.T) {
		got, _ := CapGroups([]string{"a", "b", "a"})
		if !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("over the cap cuts the alphabetical tail and keeps priority groups", func(t *testing.T) {
		groups := append(manyGroups(MaxGroups+20), "zzz-personal")
		got, cut := CapGroups(groups, "zzz-personal")
		if !cut || len(got) != MaxGroups {
			t.Fatalf("len=%d cut=%v", len(got), cut)
		}
		if got[0] != "zzz-personal" {
			t.Fatalf("priority group lost, first = %q", got[0])
		}
		if got[MaxGroups-1] != fmt.Sprintf("group-%03d", MaxGroups-2) {
			t.Fatalf("unexpected last entry %q", got[MaxGroups-1])
		}
	})

	t.Run("is deterministic regardless of input order", func(t *testing.T) {
		a := manyGroups(MaxGroups + 10)
		b := make([]string, len(a))
		for i, g := range a {
			b[len(a)-1-i] = g
		}
		ga, _ := CapGroups(a)
		gb, _ := CapGroups(b)
		if !reflect.DeepEqual(ga, gb) {
			t.Fatal("result depends on input order")
		}
	})

	t.Run("output always passes FormatHeaders", func(t *testing.T) {
		got, _ := CapGroups(manyGroups(MaxGroups + 50))
		if err := FormatHeaders(http.Header{}, Headers{Groups: got}); err != nil {
			t.Fatal(err)
		}
	})
}

func FuzzParseHeaders(f *testing.F) {
	for _, seed := range []string{"", "a,b", "a, ,b,a", "Bad,ok", ",,,", "a\x00b", strings.Repeat("a,", 200)} {
		f.Add(seed, seed, "true")
	}
	f.Fuzz(func(t *testing.T, groups, writable, all string) {
		h := http.Header{}
		h["X-User-Groups"] = []string{groups}
		h["X-User-Writable-Groups"] = []string{writable}
		h["X-User-All-Groups"] = []string{all}

		got := ParseHeaders(h)
		for _, list := range [][]string{got.Groups, got.Writable} {
			if len(list) > MaxGroups {
				t.Fatalf("list longer than MaxGroups: %d", len(list))
			}
			seen := map[string]bool{}
			for _, g := range list {
				if !ValidGroupName(g) {
					t.Fatalf("invalid group %q returned", g)
				}
				if seen[g] {
					t.Fatalf("duplicate group %q returned", g)
				}
				seen[g] = true
			}
		}
		if got.All && strings.TrimSpace(all) != "true" {
			t.Fatalf("All set for %q", all)
		}
	})
}

func BenchmarkParseHeaders(b *testing.B) {
	for _, n := range []int{20, MaxGroups} {
		h := http.Header{}
		h.Set(HeaderUserID, "u-1")
		h.Set(HeaderGroups, strings.Join(manyGroups(n), ","))
		h.Set(HeaderWritableGroups, strings.Join(manyGroups(n/2), ","))
		h.Set(HeaderAllGroups, "false")
		b.Run(fmt.Sprintf("groups=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = ParseHeaders(h)
			}
		})
	}
}
