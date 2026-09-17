package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"strings"
)

// ourModule is the module path whose frames may enter a fingerprint. Anything
// outside it — the standard library, a dependency, the runtime — is skipped,
// because those names are about machinery, not about this program.
const ourModule = "github.com/Agent-Field/codeaf/"

// fingerprintFrames is how deep the fingerprint reads: the contract's top five
// codeaf frames.
const fingerprintFrames = 5

// Fingerprint reduces a panic stack to 16 lowercase hex characters: the first
// eight bytes of sha256 over the function names of the top five stack frames
// that start with github.com/Agent-Field/codeaf/.
//
// Function names ONLY. No file paths, no line numbers, no panic value, ever —
// a path is a name of the person's machine and a panic message is often their
// words. A stack with no codeaf frames at all hashes the empty name list, so a
// fault inside a dependency still groups identically for everyone rather than
// quietly becoming a fingerprint of their directory layout.
func Fingerprint(stack []byte) string {
	names := codeafFrames(stack)
	sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
	return hex.EncodeToString(sum[:8])
}

// FingerprintHere fingerprints the calling goroutine's own stack, the shape a
// deferred recover has on hand.
func FingerprintHere() string {
	stack := make([]uintptr, 64)
	n := runtime.Callers(2, stack)
	return Fingerprint(stackBytes(stack[:n]))
}

// codeafFrames parses a printed stack and keeps the package-qualified function
// names under ourModule, dropping everything else — including the file and
// line halves of each stack line before they can reach the hash.
func codeafFrames(stack []byte) []string {
	var names []string
	for _, line := range strings.Split(string(stack), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, ourModule) {
			continue
		}
		// "pkg.Func" or "pkg.Func.Shape.Method" or "pkg.Func-fm" — never the
		// tab-indented file:line half, which never starts with the module.
		name := line
		if idx := strings.IndexAny(name, " \t"); idx >= 0 {
			name = name[:idx]
		}
		names = append(names, name)
		if len(names) == fingerprintFrames {
			break
		}
	}
	return names
}

// stackBytes renders PCs through runtime.CallersFrames, the reading that
// survives inlining.
func stackBytes(pcs []uintptr) []byte {
	var out []byte
	frames := runtime.CallersFrames(pcs)
	for {
		frame, more := frames.Next()
		out = append(out, []byte(frame.Function)...)
		out = append(out, '\n')
		if !more {
			break
		}
	}
	return out
}
