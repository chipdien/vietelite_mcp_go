package updater

import (
	"fmt"
	"strconv"
	"strings"
)

// semver is a parsed semantic version. Missing parts default to zero, so
// "1.2" and "1.2.0" compare equal.
type semver struct {
	major, minor, patch int
	pre                 []string
}

// parseSemver accepts an optional leading "v", numeric core parts, and an
// optional prerelease suffix ("-rc.1"). Build metadata ("+abc") is ignored.
func parseSemver(s string) (semver, error) {
	v := strings.TrimSpace(s)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}

	var pre []string
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = strings.Split(v[i+1:], ".")
		v = v[:i]
	}

	parts := strings.Split(v, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return semver{}, fmt.Errorf("invalid version %q", s)
	}
	nums := []int{0, 0, 0}
	for i, p := range parts {
		if p == "" {
			return semver{}, fmt.Errorf("invalid version %q", s)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, fmt.Errorf("invalid version %q", s)
		}
		nums[i] = n
	}
	return semver{major: nums[0], minor: nums[1], patch: nums[2], pre: pre}, nil
}

// isPrerelease reports whether a version string carries a prerelease suffix.
func isPrerelease(v string) bool {
	sv, err := parseSemver(v)
	return err == nil && len(sv.pre) > 0
}

// isNewer reports whether candidate is a strictly newer version than current.
//
// A development build ("dev", "", or unparseable) is always considered older
// than any published release, so `--update` works on locally built binaries.
func isNewer(current, candidate string) (bool, error) {
	if candidate == "" {
		return false, nil
	}
	cand, err := parseSemver(candidate)
	if err != nil {
		return false, err
	}

	cur, err := parseSemver(current)
	if err != nil {
		// Unversioned/dev build: any parseable release wins.
		return true, nil
	}

	if cur.major != cand.major {
		return cand.major > cur.major, nil
	}
	if cur.minor != cand.minor {
		return cand.minor > cur.minor, nil
	}
	if cur.patch != cand.patch {
		return cand.patch > cur.patch, nil
	}
	return comparePrerelease(cur.pre, cand.pre) < 0, nil
}

// comparePrerelease orders two prerelease identifier lists per semver spec:
// a version without a prerelease outranks one with it, numeric identifiers
// compare numerically, and alphanumerics compare lexically.
func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1 // release > prerelease
	case len(b) == 0:
		return -1
	}

	for i := 0; i < len(a) && i < len(b); i++ {
		if cmp := compareIdentifier(a[i], b[i]); cmp != 0 {
			return cmp
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func compareIdentifier(a, b string) int {
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	switch {
	case aerr == nil && berr == nil:
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		}
		return 0
	case aerr == nil:
		return -1 // numeric identifiers rank below alphanumeric
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
}
