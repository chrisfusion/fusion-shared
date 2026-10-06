// SPDX-License-Identifier: GPL-3.0-or-later

package ownership

import (
	"errors"
	"fmt"
)

// MaxGroupNameLen is the maximum length of an owner group name. It matches the
// Kubernetes label value limit because services store the owner as a label.
const MaxGroupNameLen = 63

// ErrInvalidGroupName is returned (wrapped, with the reason) for names that
// violate the owner group naming rules.
var ErrInvalidGroupName = errors.New("invalid owner group name")

// ValidGroupName reports whether name is a valid owner group name: 1-63
// characters from a-z 0-9 . _ - that start and end with a-z or 0-9.
// Upper-case letters and non-ASCII characters are not valid.
func ValidGroupName(name string) bool {
	return groupNameProblem(name) == ""
}

// NormalizePersonalName turns the value of the configured OIDC claim into a
// personal group name: ASCII letters are lower-cased, nothing else is changed.
// A value that is still invalid afterwards is rejected, never altered or
// truncated. Non-ASCII input is rejected before lower-casing so that look-alike
// characters (for example the Kelvin sign) can never collapse into an ASCII name.
func NormalizePersonalName(raw string) (string, error) {
	lowered := make([]byte, len(raw))
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c >= 0x80 {
			return "", fmt.Errorf("%w: contains a non-ASCII character", ErrInvalidGroupName)
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		lowered[i] = c
	}
	name := string(lowered)
	if problem := groupNameProblem(name); problem != "" {
		return "", fmt.Errorf("%w: %s", ErrInvalidGroupName, problem)
	}
	return name, nil
}

// groupNameProblem returns a short reason why name is invalid, or "" if valid.
func groupNameProblem(name string) string {
	n := len(name)
	switch {
	case n == 0:
		return "empty"
	case n > MaxGroupNameLen:
		return "longer than 63 characters"
	}
	for i := 0; i < n; i++ {
		c := name[i]
		switch {
		case isLowerAlnum(c):
		case c == '.' || c == '_' || c == '-':
			if i == 0 || i == n-1 {
				return "must start and end with a-z or 0-9"
			}
		default:
			return "contains a character outside a-z 0-9 . _ -"
		}
	}
	return ""
}

func isLowerAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}
