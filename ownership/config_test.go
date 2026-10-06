// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const validConfig = `
enforce: true
legacyGroup: shared
principals:
  - name: sa/system:serviceaccount:fusion:fusion-bff
    trustedProxy: true
  - name: sa/system:serviceaccount:fusion:ci
    groups: [team-data, shared]
    writableGroups: [team-data]
  - name: apikey/reporting
    groups: [shared]
  - name: oidc/ops-admin
    allGroups: true
  - name: sa/system:serviceaccount:fusion:jobs
    assertableGroups: [team-data, team-ml]
`

func TestParse_Valid(t *testing.T) {
	c, err := Parse([]byte(validConfig))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !c.Enforce || c.LegacyGroup != "shared" || len(c.Principals) != 5 {
		t.Fatalf("unexpected config: %+v", c)
	}

	e, ok := c.Lookup("sa/system:serviceaccount:fusion:ci")
	if !ok {
		t.Fatal("ci entry not found")
	}
	if !reflect.DeepEqual(e.Groups, []string{"team-data", "shared"}) || !reflect.DeepEqual(e.WritableGroups, []string{"team-data"}) {
		t.Fatalf("ci entry: %+v", e)
	}
	if e, _ := c.Lookup("sa/system:serviceaccount:fusion:fusion-bff"); !e.TrustedProxy {
		t.Fatalf("bff entry not trusted: %+v", e)
	}
	if e, _ := c.Lookup("oidc/ops-admin"); !e.AllGroups {
		t.Fatalf("admin entry: %+v", e)
	}
	if e, _ := c.Lookup("sa/system:serviceaccount:fusion:jobs"); !reflect.DeepEqual(e.AssertableGroups, []string{"team-data", "team-ml"}) {
		t.Fatalf("jobs entry: %+v", e)
	}
	if _, ok := c.Lookup("unknown"); ok {
		t.Fatal("unknown principal must not be found")
	}
}

func TestParse_MinimalAndDisabled(t *testing.T) {
	c, err := Parse([]byte("enforce: false\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Enforce || len(c.Principals) != 0 {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestParse_Rejects(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string // substring of the error
	}{
		{"empty file", "", "file is empty"},
		{"only comments", "# nothing\n", "file is empty"},
		{"malformed yaml", "enforce: [", "invalid ownership config"},
		{"unknown top-level key", "enforcee: true\n", "enforcee"},
		{"unknown entry key", "principals:\n  - name: a\n    trustedProxie: true\n", "trustedProxie"},
		{"multiple documents", "enforce: true\n---\nenforce: false\n", "more than one YAML document"},
		{"invalid legacy group", "legacyGroup: Shared\n", "legacyGroup"},
		{"missing name", "principals:\n  - trustedProxy: true\n", "name is required"},
		{"whitespace in name", "principals:\n  - name: ' a'\n    trustedProxy: true\n", "whitespace"},
		{"duplicate name", "principals:\n  - {name: a, trustedProxy: true}\n  - {name: a, allGroups: true}\n", "duplicate name"},
		{"invalid group name", "principals:\n  - {name: a, groups: [Bad]}\n", "groups contains an invalid group name"},
		{"invalid writable name", "principals:\n  - {name: a, groups: [x], writableGroups: ['x,y']}\n", "writableGroups contains an invalid group name"},
		{"invalid assertable name", "principals:\n  - {name: a, assertableGroups: ['-x']}\n", "assertableGroups contains an invalid group name"},
		{"writable not subset", "principals:\n  - {name: a, groups: [x], writableGroups: [y]}\n", "writableGroups must be a subset"},
		{"writable without groups", "principals:\n  - {name: a, writableGroups: [x]}\n", "writableGroups must be a subset"},
		{"trusted proxy with groups", "principals:\n  - {name: a, trustedProxy: true, groups: [x]}\n", "trustedProxy has no groups"},
		{"trusted proxy with allGroups", "principals:\n  - {name: a, trustedProxy: true, allGroups: true}\n", "trustedProxy has no groups"},
		{"allGroups with groups", "principals:\n  - {name: a, allGroups: true, groups: [x]}\n", "allGroups cannot be combined"},
		{"assertable with groups", "principals:\n  - {name: a, assertableGroups: [x], groups: [y]}\n", "assertableGroups cannot be combined"},
		{"entry grants nothing", "principals:\n  - name: a\n", "grants nothing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse([]byte(tc.yaml))
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error = %v, want ErrInvalidConfig", err)
			}
			if c != nil {
				t.Fatalf("config returned with an error: %+v", c)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestParse_ReportsAllProblemsTogether(t *testing.T) {
	_, err := Parse([]byte("legacyGroup: Bad\nprincipals:\n  - name: a\n  - name: a\n    trustedProxy: true\n"))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"legacyGroup", "grants nothing", "duplicate name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestParse_ToleratesDuplicateGroups(t *testing.T) {
	if _, err := Parse([]byte("principals:\n  - {name: a, groups: [x, x], writableGroups: [x, x]}\n")); err != nil {
		t.Fatalf("duplicate group entries should be harmless: %v", err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ownership.yaml")
	if err := os.WriteFile(path, []byte(validConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Principals) != 5 {
		t.Fatalf("got %d principals", len(c.Principals))
	}

	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil || errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing file should be a read error, got %v", err)
	}

	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("enforce: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestValidate_FailFastWithoutAuth(t *testing.T) {
	tests := []struct {
		name        string
		enforce     bool
		authEnabled bool
		want        error
	}{
		{"enforcement on, auth on", true, true, nil},
		{"enforcement off, auth off", false, false, nil},
		{"enforcement off, auth on", false, true, nil},
		{"enforcement on, auth off", true, false, ErrAuthRequired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Enforce: tc.enforce}
			if err := c.Validate(tc.authEnabled); !errors.Is(err, tc.want) && err != tc.want {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidate_HandBuiltConfigIsChecked(t *testing.T) {
	c := &Config{Enforce: true, Principals: []PrincipalEntry{{Name: "a", Groups: []string{"Bad"}}}}
	if err := c.Validate(true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Validate = %v, want ErrInvalidConfig", err)
	}

	ok := &Config{Principals: []PrincipalEntry{{Name: "a", TrustedProxy: true}}}
	if err := ok.Validate(false); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	if e, found := ok.Lookup("a"); !found || !e.TrustedProxy {
		t.Fatalf("Lookup on a hand-built config failed: %+v %v", e, found)
	}
}

func FuzzParseConfig(f *testing.F) {
	f.Add(validConfig)
	f.Add("")
	f.Add("enforce: true\nprincipals:\n  - name: a\n")
	f.Add("principals: [{name: a, groups: [x], writableGroups: [x]}]")
	f.Fuzz(func(t *testing.T, data string) {
		c, err := Parse([]byte(data))
		if err != nil {
			return
		}
		// Anything Parse accepts must also pass a fresh structural check and
		// keep the write-subset-of-read invariant.
		if err := c.Validate(true); err != nil {
			t.Fatalf("accepted config fails Validate: %v", err)
		}
		for _, e := range c.Principals {
			if !subset(e.WritableGroups, e.Groups) {
				t.Fatalf("writable not a subset in accepted entry %+v", e)
			}
		}
	})
}
