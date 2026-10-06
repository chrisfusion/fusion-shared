// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Trusted headers set by the BFF (and by trusted proxies such as weave and
// wizard) on calls to upstream services. Services must only honour them from
// principals marked as trusted proxy and must strip them from all other callers.
const (
	HeaderUserID         = "X-User-ID"
	HeaderUserEmail      = "X-User-Email"
	HeaderGroups         = "X-User-Groups"          // owner groups the caller may see
	HeaderWritableGroups = "X-User-Writable-Groups" // subset the caller may write to
	HeaderAllGroups      = "X-User-All-Groups"      // "true" if the caller holds the all-groups permission
	HeaderDefaultGroup   = "X-User-Default-Group"   // owner group used when a create names none
)

// MaxGroups is the maximum number of groups carried in one group header.
const MaxGroups = 100

// ErrInvalidHeaders is returned (wrapped) by FormatHeaders when a value cannot
// be sent safely.
var ErrInvalidHeaders = errors.New("invalid ownership headers")

// Headers is the parsed form of the trusted ownership headers.
type Headers struct {
	UserID       string
	Email        string
	Groups       []string // owner groups the caller may see
	Writable     []string // as sent; callers must intersect with Groups
	All          bool
	DefaultGroup string

	// Invalid counts group entries dropped because they are not valid group
	// names. Truncated is set when a group list exceeded MaxGroups. Both only
	// ever reduce access.
	Invalid   int
	Truncated bool
}

// ParseHeaders reads the trusted headers from h. It never fails: invalid group
// names, duplicates and entries beyond MaxGroups are dropped, an all-groups flag
// is true only for the exact value "true", and an invalid default group is
// ignored. Parsing narrows access, it never widens it.
func ParseHeaders(h http.Header) Headers {
	groups, invalidG, truncG := parseGroupList(h.Values(HeaderGroups))
	writable, invalidW, truncW := parseGroupList(h.Values(HeaderWritableGroups))

	out := Headers{
		UserID:    strings.TrimSpace(h.Get(HeaderUserID)),
		Email:     strings.TrimSpace(h.Get(HeaderUserEmail)),
		Groups:    groups,
		Writable:  writable,
		All:       strings.TrimSpace(h.Get(HeaderAllGroups)) == "true",
		Invalid:   invalidG + invalidW,
		Truncated: truncG || truncW,
	}
	if d := strings.TrimSpace(h.Get(HeaderDefaultGroup)); ValidGroupName(d) {
		out.DefaultGroup = d
	}
	return out
}

// StripHeaders removes all trusted ownership headers from h. Call it on every
// incoming request before the values of a trusted principal are applied, and on
// requests from principals that are not trusted proxies.
func StripHeaders(h http.Header) {
	for _, k := range []string{
		HeaderUserID, HeaderUserEmail, HeaderGroups,
		HeaderWritableGroups, HeaderAllGroups, HeaderDefaultGroup,
	} {
		h.Del(k)
	}
}

// FormatHeaders replaces the trusted ownership headers in h with v. Existing
// values (for example client-supplied ones) are removed first. Nothing is
// written if any value is unsafe: invalid group names, more than MaxGroups
// groups, or control characters in the identity values.
func FormatHeaders(h http.Header, v Headers) error {
	if err := validateFormat(v); err != nil {
		return err
	}
	StripHeaders(h)
	if v.UserID != "" {
		h.Set(HeaderUserID, v.UserID)
	}
	if v.Email != "" {
		h.Set(HeaderUserEmail, v.Email)
	}
	if len(v.Groups) > 0 {
		h.Set(HeaderGroups, strings.Join(v.Groups, ","))
	}
	if len(v.Writable) > 0 {
		h.Set(HeaderWritableGroups, strings.Join(v.Writable, ","))
	}
	if v.All {
		h.Set(HeaderAllGroups, "true")
	}
	if v.DefaultGroup != "" {
		h.Set(HeaderDefaultGroup, v.DefaultGroup)
	}
	return nil
}

// CapGroups limits groups to MaxGroups deterministically: the groups named in
// first come first (in that order, if present in groups), the rest follow in
// alphabetical order. Duplicates are removed. The second result reports whether
// groups were cut. Use it before FormatHeaders for users in many groups; cutting
// only ever reduces access.
func CapGroups(groups []string, first ...string) ([]string, bool) {
	present := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		present[g] = struct{}{}
	}
	out := make([]string, 0, min(len(present), MaxGroups))
	used := make(map[string]struct{}, len(first))
	for _, f := range first {
		if _, ok := present[f]; !ok {
			continue
		}
		if _, dup := used[f]; dup {
			continue
		}
		used[f] = struct{}{}
		out = append(out, f)
	}
	rest := make([]string, 0, len(present))
	for g := range present {
		if _, ok := used[g]; !ok {
			rest = append(rest, g)
		}
	}
	sort.Strings(rest)
	out = append(out, rest...)
	if len(out) > MaxGroups {
		return out[:MaxGroups], true
	}
	return out, false
}

// parseGroupList splits header values on commas, trims spaces and keeps valid,
// unique group names in order, up to MaxGroups.
func parseGroupList(values []string) (groups []string, invalid int, truncated bool) {
	var seen map[string]struct{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			if !ValidGroupName(name) {
				invalid++
				continue
			}
			if seen == nil {
				seen = make(map[string]struct{})
			}
			if _, dup := seen[name]; dup {
				continue
			}
			if len(groups) == MaxGroups {
				truncated = true
				continue
			}
			seen[name] = struct{}{}
			groups = append(groups, name)
		}
	}
	return groups, invalid, truncated
}

func validateFormat(v Headers) error {
	for _, s := range []string{v.UserID, v.Email} {
		if strings.ContainsAny(s, "\r\n\x00") {
			return fmt.Errorf("%w: control character in identity value", ErrInvalidHeaders)
		}
	}
	if v.DefaultGroup != "" && !ValidGroupName(v.DefaultGroup) {
		return fmt.Errorf("%w: invalid default group", ErrInvalidHeaders)
	}
	for _, list := range [][]string{v.Groups, v.Writable} {
		if len(list) > MaxGroups {
			return fmt.Errorf("%w: more than %d groups", ErrInvalidHeaders, MaxGroups)
		}
		for _, g := range list {
			if !ValidGroupName(g) {
				return fmt.Errorf("%w: invalid group name", ErrInvalidHeaders)
			}
		}
	}
	return nil
}
