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

// cutArguments drops the trailing argument group from a printed frame line:
// scanning back from the final ')', the group is the argument list only when
// what precedes it ends in a name character — a receiver group like
// (*Runner) sits inside the name and is kept.
func cutArguments(line string) string {
	if !strings.HasSuffix(line, ")") || len(line) < 2 {
		return line
	}
	depth := 0
	for i := len(line) - 1; i >= 0; i-- {
		switch line[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				prefix := line[:i]
				if prefix == "" {
					return line
				}
				last := prefix[len(prefix)-1]
				if last == '_' || (last >= '0' && last <= '9') ||
					(last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z') {
					return prefix
				}
				return line
			}
		}
	}
	return line
}

// FingerprintHere fingerprints the calling goroutine's own stack, the shape a
// deferred recover has on hand.
func FingerprintHere() string {
	stack := make([]uintptr, 64)
	n := runtime.Callers(2, stack)
	return Fingerprint(stackBytes(stack[:n]))
}

// mainPrefix is how the program's own entrypoint prints: cmd/codeaf's frames
// carry no module path, so without this a crash at the top of the binary has
// no codeaf frames at all and every such fault groups as one nameless blob.
const mainPrefix = "main."

// codeafFrames parses a printed stack and keeps the package-qualified function
// names under ourModule — plus the bare main frames — dropping everything
// else, including the file and line halves of each stack line before they can
// reach the hash.
func codeafFrames(stack []byte) []string {
	var names []string
	for _, line := range strings.Split(string(stack), "\n") {
		line = strings.TrimSpace(line)
		// The file:line half of a frame. In a -trimpath release build that
		// half ALSO starts with the module path, so it must be dropped before
		// the prefix is read, or the line numbers of the build enter the hash.
		if strings.Contains(line, ".go:") {
			continue
		}
		// "created by X in goroutine N" — provenance, not a frame the crash
		// passed through; its file:line half was already dropped above.
		if strings.HasPrefix(line, "created by") {
			continue
		}
		if !strings.HasPrefix(line, ourModule) && !strings.HasPrefix(line, mainPrefix) {
			continue
		}
		// "pkg.Func" or "pkg.Func.Shape.Method" or "pkg.Func-fm" — the
		// argument list is dropped by balanced matching from the final ')'
		// so a wide argument list cannot leave half an argument behind, and
		// pointer values in it can never move the fingerprint.
		names = append(names, cutArguments(line))
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
