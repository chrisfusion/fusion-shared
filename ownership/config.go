// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ErrInvalidConfig is returned (wrapped, listing every problem found) when an
// ownership config file or value is not valid.
var ErrInvalidConfig = errors.New("invalid ownership config")

// ErrAuthRequired is returned by Config.Validate when enforcement is switched on
// while authentication is disabled: every caller would be anonymous.
var ErrAuthRequired = errors.New("ownership enforcement requires authentication to be enabled")

// PrincipalEntry configures the scope of one non-user caller (a service account,
// an API key or an OIDC subject). Name is the opaque principal string the
// service passes to the Resolver, for example weave's
// "sa/system:serviceaccount:<ns>:<name>". Exactly one mode must be used:
//
//   - TrustedProxy: the principal forwards the end user's scope in the trusted
//     headers (BFF, weave, wizard). It has no groups of its own.
//   - AllGroups: the principal may read and write every owner group.
//   - Groups (read) and optionally WritableGroups (a subset of Groups).
//   - AssertableGroups: the principal (for example a job pod) may assert a read
//     scope in the headers, honoured only for groups in this list, never write.
type PrincipalEntry struct {
	Name             string   `yaml:"name"`
	TrustedProxy     bool     `yaml:"trustedProxy"`
	AllGroups        bool     `yaml:"allGroups"`
	Groups           []string `yaml:"groups"`
	WritableGroups   []string `yaml:"writableGroups"`
	AssertableGroups []string `yaml:"assertableGroups"`
}

// Config is the ownership configuration of one service, loaded once at start-up
// from a mounted file (a changed ConfigMap needs a pod restart). Build it with
// Load, Parse or by filling the fields and calling Validate. It must not be
// modified after first use and must be shared by pointer.
type Config struct {
	// Enforce switches visibility and ownership checks on. When false the
	// service keeps its previous behaviour but still records an owner on create.
	Enforce bool `yaml:"enforce"`
	// LegacyGroup is the owner recorded when enforcement is off and neither the
	// request nor the caller names one; it is also the migration target.
	LegacyGroup string `yaml:"legacyGroup"`
	// Principals lists the non-user callers and their scope.
	Principals []PrincipalEntry `yaml:"principals"`

	once   sync.Once
	byName map[string]PrincipalEntry
}

// Load reads and validates the config file at path. Unknown keys are an error,
// so typos cannot silently drop a rule.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ownership config: %w", err)
	}
	return Parse(data)
}

// Parse decodes and validates YAML config data. The data must contain exactly
// one non-empty document.
func Parse(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var c Config
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: file is empty", ErrInvalidConfig)
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: more than one YAML document", ErrInvalidConfig)
	}
	if err := c.checkStructure(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks the config and the runtime setting. authEnabled states whether
// the service has authentication switched on. With Enforce on and authentication
// off it returns ErrAuthRequired so the service can refuse to start (W38).
func (c *Config) Validate(authEnabled bool) error {
	if err := c.checkStructure(); err != nil {
		return err
	}
	if c.Enforce && !authEnabled {
		return ErrAuthRequired
	}
	return nil
}

// Lookup returns the entry configured for the principal name.
func (c *Config) Lookup(name string) (PrincipalEntry, bool) {
	c.once.Do(func() {
		c.byName = make(map[string]PrincipalEntry, len(c.Principals))
		for _, e := range c.Principals {
			c.byName[e.Name] = e
		}
	})
	e, ok := c.byName[name]
	return e, ok
}

// checkStructure reports all structural problems at once.
func (c *Config) checkStructure() error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if c.LegacyGroup != "" && !ValidGroupName(c.LegacyGroup) {
		add("legacyGroup is not a valid group name")
	}

	seen := make(map[string]struct{}, len(c.Principals))
	for i, e := range c.Principals {
		label := fmt.Sprintf("principals[%d]", i)
		if e.Name != "" {
			label += " (" + e.Name + ")"
		}
		switch {
		case e.Name == "":
			add("%s: name is required", label)
		case e.Name != strings.TrimSpace(e.Name):
			add("%s: name has leading or trailing whitespace", label)
		default:
			if _, dup := seen[e.Name]; dup {
				add("%s: duplicate name", label)
			}
			seen[e.Name] = struct{}{}
		}

		// A slice (not a map) keeps the order of reported problems stable.
		for _, l := range []struct {
			field string
			names []string
		}{
			{"groups", e.Groups}, {"writableGroups", e.WritableGroups}, {"assertableGroups", e.AssertableGroups},
		} {
			for _, g := range l.names {
				if !ValidGroupName(g) {
					add("%s: %s contains an invalid group name", label, l.field)
					break
				}
			}
		}

		hasLists := len(e.Groups) > 0 || len(e.WritableGroups) > 0
		hasAssertable := len(e.AssertableGroups) > 0
		switch {
		case e.TrustedProxy && (e.AllGroups || hasLists || hasAssertable):
			add("%s: trustedProxy has no groups of its own and cannot be combined with other modes", label)
		case e.AllGroups && (hasLists || hasAssertable):
			add("%s: allGroups cannot be combined with group lists", label)
		case hasAssertable && hasLists:
			add("%s: assertableGroups cannot be combined with groups or writableGroups", label)
		case !e.TrustedProxy && !e.AllGroups && !hasLists && !hasAssertable:
			add("%s: entry grants nothing", label)
		}

		if !subset(e.WritableGroups, e.Groups) {
			add("%s: writableGroups must be a subset of groups", label)
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidConfig, strings.Join(problems, "; "))
	}
	return nil
}

// subset reports whether every element of sub is in super.
func subset(sub, super []string) bool {
	if len(sub) == 0 {
		return true
	}
	in := make(map[string]struct{}, len(super))
	for _, g := range super {
		in[g] = struct{}{}
	}
	for _, g := range sub {
		if _, ok := in[g]; !ok {
			return false
		}
	}
	return true
}
