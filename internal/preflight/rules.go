package preflight

import (
	"regexp"
	"strings"
)

// requirement is a capability a tool cannot run without.
type requirement struct {
	Reason string
	Met    func(Capabilities) bool
}

func onDarwin(c Capabilities) bool { return c.Platform.OS == "darwin" }
func hasCUDA(c Capabilities) bool  { return c.GPU && c.Platform.OS != "darwin" }

var (
	needsXcode = requirement{"needs Xcode (macOS only)", onDarwin}
	needsCUDA  = requirement{"needs CUDA", hasCUDA}
)

// impossibleTools lists tools that no setup turn can provide on a machine
// that lacks the hardware or operating system they belong to.
var impossibleTools = map[string]requirement{
	"xcodebuild": needsXcode,
	"xcrun":      needsXcode,
	"simctl":     needsXcode,
	"nvcc":       needsCUDA,
	"nvidia-smi": needsCUDA,
	"cuda":       needsCUDA,
}

// lockedFamilies are the services whose data directory only opens under the
// same major version. The name is the binary the inventory records.
var lockedFamilies = map[string]string{
	"postgres":     "Postgres",
	"psql":         "Postgres",
	"mysqld":       "MySQL",
	"mysql":        "MySQL",
	"redis-server": "Redis",
	"redis":        "Redis",
}

func lockedFamily(name string) bool { _, ok := lockedFamilies[name]; return ok }

var versionNumber = regexp.MustCompile(`\d+(\.\d+)*`)

// numbers returns the dotted numeric parts of the first version in s.
func numbers(s string) []string {
	return strings.Split(versionNumber.FindString(s), ".")
}

func prefix(s string, n int) string {
	p := numbers(s)
	if len(p) < n {
		n = len(p)
	}
	return strings.Join(p[:n], ".")
}

// short is the major.minor form used in messages; "" when s has no number.
func short(s string) string { return strings.Trim(prefix(s, 2), ".") }

func major(s string) string { return strings.Trim(prefix(s, 1), ".") }

// differs compares major.minor; an unknown version on either side never differs.
func differs(want, have string) bool {
	w, h := short(want), short(have)
	return w != "" && h != "" && w != h
}

func differsMajor(want, have string) bool {
	w, h := major(want), major(have)
	return w != "" && h != "" && w != h
}

func platformString(p Platform) string { return p.OS + "/" + p.Arch }
