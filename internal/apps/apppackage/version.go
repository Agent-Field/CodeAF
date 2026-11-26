package apppackage

import (
	"cmp"
	"regexp"
	"strings"
)

var semverShape = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

type packageVersion struct {
	core       []string
	prerelease []string
}

func parseVersion(raw string) (packageVersion, bool) {
	match := semverShape.FindStringSubmatch(raw)
	if match == nil {
		return packageVersion{}, false
	}
	v := packageVersion{core: match[1:4]}
	if match[5] != "" {
		v.prerelease = strings.Split(match[5], ".")
		for _, part := range v.prerelease {
			if len(part) > 1 && part[0] == '0' && numeric(part) {
				return packageVersion{}, false
			}
		}
	}
	return v, true
}

func numeric(part string) bool {
	return strings.Trim(part, "0123456789") == ""
}

// Integers in SemVer have no size bound. The valid spellings sort by
// digit count first, then lexicographically when their digit counts match.
func compareNumber(a, b string) int {
	if n := cmp.Compare(len(a), len(b)); n != 0 {
		return n
	}
	return cmp.Compare(a, b)
}

func (v packageVersion) compare(b packageVersion) int {
	for i, n := range v.core {
		if order := compareNumber(n, b.core[i]); order != 0 {
			return order
		}
	}
	aPre, bPre := v.prerelease, b.prerelease
	if (len(aPre) == 0) != (len(bPre) == 0) {
		if len(aPre) == 0 {
			return 1
		}
		return -1
	}
	for i := 0; i < min(len(aPre), len(bPre)); i++ {
		a, b := aPre[i], bPre[i]
		var order int
		switch {
		case numeric(a) && numeric(b):
			order = compareNumber(a, b)
		case numeric(a):
			order = -1
		case numeric(b):
			order = 1
		default:
			order = cmp.Compare(a, b)
		}
		if order != 0 {
			return order
		}
	}
	return cmp.Compare(len(aPre), len(bPre))
}
