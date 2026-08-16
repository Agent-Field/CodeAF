package blocks

import (
	"os"
	"path/filepath"
	"strings"
)

// strictDefault turns [Transcript.Strict] on for test binaries and off
// everywhere else, which is what "detected in test builds" means in 8.1.1.
//
// It is detected rather than compiled in, so `go test ./...` gets the check
// with no build tag to remember and the shipped binary carries neither the
// check nor an import of the testing package.
//
// Benchmarks measuring the steady state must turn it OFF explicitly: the check
// re-renders every cached block on every frame, which is precisely the work the
// cache exists to avoid.
var strictDefault = underTest()

func underTest() bool {
	base := filepath.Base(os.Args[0])
	if strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe") {
		return true
	}
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-test.") || strings.HasPrefix(arg, "--test.") {
			return true
		}
	}
	return false
}
