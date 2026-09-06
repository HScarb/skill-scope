package codex

import (
	"cmp"
	"errors"
	"strconv"
	"strings"
)

type version struct {
	core       [3]uint64
	pre, build []string
}

func parseVersion(text string) (version, bool) {
	var v version
	core, build, hasBuild := strings.Cut(text, "+")
	core, pre, hasPre := strings.Cut(core, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, part := range parts {
		if !numeric(part) || (len(part) > 1 && part[0] == '0') {
			return v, false
		}
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return v, false
		}
		v.core[i] = n
	}
	if hasPre {
		v.pre = strings.Split(pre, ".")
		if !validIdentifiers(v.pre, true) {
			return v, false
		}
	}
	if hasBuild {
		v.build = strings.Split(build, ".")
		if !validIdentifiers(v.build, false) {
			return v, false
		}
	}
	return v, true
}
func numeric(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
func validIdentifiers(parts []string, pre bool) bool {
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i := range len(part) {
			b := part[i]
			allowed := b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b == '-'
			if !allowed {
				return false
			}
		}
		if pre && numeric(part) && len(part) > 1 && part[0] == '0' {
			return false
		}
	}
	return true
}
func compareIdentifiers(a, b []string, build bool) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		x, y := a[i], b[i]
		nx, ny := numeric(x), numeric(y)
		if nx != ny {
			if nx {
				return -1
			}
			return 1
		}
		var order int
		if nx {
			xx, yy := strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
			order = cmp.Compare(len(xx), len(yy))
			if order == 0 {
				order = strings.Compare(xx, yy)
			}
			if order == 0 && build {
				order = cmp.Compare(len(x), len(y))
			}
		} else {
			order = strings.Compare(x, y)
		}
		if order != 0 {
			return order
		}
	}
	return cmp.Compare(len(a), len(b))
}

// compareVersions mirrors Codex's pinned Rust semver total ordering, including build metadata.
// Mixed semantic and nonsemantic names may make this relation nontransitive.
func compareVersions(a, b string) int {
	if a == b {
		return 0
	}
	if a == "local" {
		return 1
	}
	if b == "local" {
		return -1
	}
	x, okX := parseVersion(a)
	y, okY := parseVersion(b)
	if !okX || !okY {
		return strings.Compare(a, b)
	}
	for i := range x.core {
		if order := cmp.Compare(x.core[i], y.core[i]); order != 0 {
			return order
		}
	}
	if len(x.pre) == 0 && len(y.pre) > 0 {
		return 1
	}
	if len(y.pre) == 0 && len(x.pre) > 0 {
		return -1
	}
	if order := compareIdentifiers(x.pre, y.pre, false); order != 0 {
		return order
	}
	return compareIdentifiers(x.build, y.build, true)
}
func activeVersion(names []string) (string, error) {
	if len(names) == 0 {
		return "", nil
	}
	// A tournament or sorting pass can silently pick a winner in a comparison cycle.
	for _, candidate := range names {
		dominates := true
		for _, other := range names {
			if other != candidate && compareVersions(candidate, other) <= 0 {
				dominates = false
				break
			}
		}
		if dominates {
			return candidate, nil
		}
	}
	return "", errors.New("ambiguous active plugin version")
}

func validVersionDirectory(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for i := range len(name) {
		b := name[i]
		allowed := b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b == '-' || b == '_' || b == '.' || b == '+'
		if !allowed {
			return false
		}
	}
	return true
}
