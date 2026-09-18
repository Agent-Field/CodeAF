package verify

// Regexps, compiled on first use rather than at package init.
//
// Every pattern this package matches a runner's output with used to be a
// package-level regexp.MustCompile, so all of them were compiled before main
// on every invocation — including --version, which reads no runner output and
// needs none of them. GODEBUG=inittrace=1 put that at ~1.5 MB and ~11k
// allocations of package init. lazyRegexp compiles a pattern the first time it
// is called instead, and the callers below are unchanged in what they match.

import (
	"regexp"
	"sync"
)

// lazyRegexp defers a pattern's compilation to its first call. MustCompile's
// panic on a malformed pattern is kept — it still fails loudly, at first use
// rather than before main.
func lazyRegexp(pattern string) func() *regexp.Regexp {
	return sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(pattern) })
}
