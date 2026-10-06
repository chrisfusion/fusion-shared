// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// nameOracle is an independent statement of the naming rule (W11).
var nameOracle = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,61}[a-z0-9])?$`)

func TestValidGroupName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"single letter", "a", true},
		{"single digit", "7", true},
		{"typical", "team-data", true},
		{"with dots and underscore", "alice.smith_01", true},
		{"max length", strings.Repeat("a", 63), true},
		{"too long", strings.Repeat("a", 64), false},
		{"empty", "", false},
		{"leading dot", ".a", false},
		{"trailing dot", "a.", false},
		{"leading dash", "-a", false},
		{"trailing dash", "a-", false},
		{"leading underscore", "_a", false},
		{"only separator", "-", false},
		{"upper case", "Alice", false},
		{"space", "a b", false},
		{"at sign", "alice@corp", false},
		{"slash", "a/b", false},
		{"comma", "a,b", false},
		{"wildcard", "*", false},
		{"non-ascii", "ä", false},
		{"newline", "a\nb", false},
		{"max length with inner separators", "a" + strings.Repeat(".", 61) + "a", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidGroupName(tc.in); got != tc.want {
				t.Fatalf("ValidGroupName(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizePersonalName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"already lower", "alice", "alice", false},
		{"upper case is lowered", "Alice", "alice", false},
		{"mixed with separators", "ALICE.Smith_01", "alice.smith_01", false},
		{"empty", "", "", true},
		{"email is rejected, not trimmed", "alice@corp.com", "", true},
		{"leading space is rejected, not trimmed", " alice", "", true},
		{"trailing separator", "Alice-", "", true},
		{"too long is rejected, not truncated", strings.Repeat("A", 64), "", true},
		{"non-ascii", "Jürgen", "", true},
		{"kelvin sign must not become k", "Klice", "", true},
		{"dotted capital I must not become i", "İvan", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizePersonalName(tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidGroupName) {
					t.Fatalf("NormalizePersonalName(%q) error = %v, want ErrInvalidGroupName", tc.in, err)
				}
				if got != "" {
					t.Fatalf("NormalizePersonalName(%q) returned %q with an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizePersonalName(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizePersonalName(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if !ValidGroupName(got) {
				t.Fatalf("NormalizePersonalName(%q) = %q is not a valid group name", tc.in, got)
			}
		})
	}
}

func FuzzValidGroupName(f *testing.F) {
	for _, seed := range []string{"", "a", "team-data", ".a", "a.", "Alice", "ä", strings.Repeat("a", 64), "a\x00b"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := ValidGroupName(s), nameOracle.MatchString(s); got != want {
			t.Fatalf("ValidGroupName(%q) = %v, oracle says %v", s, got, want)
		}
	})
}

func FuzzNormalizePersonalName(f *testing.F) {
	for _, seed := range []string{"", "Alice", "ALICE.Smith", "Klice", "a b"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, err := NormalizePersonalName(s)
		if err != nil {
			return
		}
		if !ValidGroupName(got) {
			t.Fatalf("NormalizePersonalName(%q) = %q is not valid", s, got)
		}
		if strings.ToLower(s) != got && !strings.EqualFold(s, got) {
			t.Fatalf("NormalizePersonalName(%q) = %q changed more than letter case", s, got)
		}
	})
}
