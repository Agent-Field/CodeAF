package telemetry

import (
	"strings"
	"testing"
)

// codeafStack renders bare function names the way a stack the program built
// itself would carry them.
func codeafStack(names ...string) []byte {
	lines := make([]string, len(names))
	for i, name := range names {
		lines[i] = "github.com/Agent-Field/codeaf/" + name
	}
	return []byte(strings.Join(lines, "\n"))
}

func TestFingerprintIsStableHex(t *testing.T) {
	stack := codeafStack("internal/session.(*Runner).step", "internal/model.(*Client).call")
	first := Fingerprint(stack)
	if !reFingerprint.MatchString(first) {
		t.Fatalf("Fingerprint = %q, want 16 lowercase hex characters", first)
	}
	if again := Fingerprint(stack); again != first {
		t.Errorf("the same stack fingerprinted as %q and %q", first, again)
	}
}

func TestFingerprintReadsFunctionNamesOnly(t *testing.T) {
	// The same three frames, first bare, then in the shape debug.Stack
	// prints them: arguments on the frame line, file and line beneath.
	printed := strings.Join([]string{
		"github.com/Agent-Field/codeaf/internal/session.(*Runner).step(0xc0001, 0x2)",
		"\t/Users/santosh/secret-project/internal/session/runner.go:412 +0x88",
		"github.com/Agent-Field/codeaf/internal/model.(*Client).call(...)",
		"\t/Users/santosh/secret-project/internal/model/client.go:97 +0x1c",
		"github.com/Agent-Field/codeaf/cmd/codeaf.runChat()",
		"\t/Users/santosh/secret-project/cmd/codeaf/main.go:201 +0x64",
	}, "\n")
	bare := Fingerprint(codeafStack(
		"internal/session.(*Runner).step",
		"internal/model.(*Client).call",
		"cmd/codeaf.runChat",
	))
	if got := Fingerprint([]byte(printed)); got != bare {
		t.Errorf("paths and line numbers moved the fingerprint: %q against %q", got, bare)
	}
}

func TestFingerprintIgnoresForeignFramesAndPanicText(t *testing.T) {
	core := codeafStack("internal/session.(*Runner).step", "internal/model.(*Client).call")
	noisy := strings.Join([]string{
		"panic: the deploy froze mid-write for person@example.com",
		"/Users/santosh/secret-project/leaky.go:7 +0x10",
		"some.dependency/pkg.(*Thing).Do(0x1)",
		"github.com/Agent-Field/codeaf/internal/session.(*Runner).step(0x1, 0x2)",
		"\t/tmp/whatever/runner.go:1",
		"github.com/Agent-Field/codeaf/internal/model.(*Client).call(...)",
	}, "\n")
	if got, want := Fingerprint([]byte(noisy)), Fingerprint(core); got != want {
		t.Errorf("foreign frames and panic text moved the fingerprint: %q against %q", got, want)
	}
}

func TestFingerprintHashesOnlyTheTopFiveCodeafFrames(t *testing.T) {
	five := Fingerprint(codeafStack("a", "b", "c", "d", "e"))
	six := Fingerprint(codeafStack("a", "b", "c", "d", "e", "f"))
	if five != six {
		t.Error("a sixth codeaf frame must not move the fingerprint")
	}
	if changed := Fingerprint(codeafStack("a", "b", "c", "d", "z")); changed == five {
		t.Error("a changed fifth frame must move the fingerprint")
	}
}

func TestFingerprintOfAForeignStackGroupsAsEmpty(t *testing.T) {
	foreign := strings.Join([]string{
		"main.main()",
		"\t/tmp/other-project/main.go:9",
		"runtime.main()",
	}, "\n")
	if got := Fingerprint([]byte(foreign)); got != Fingerprint(nil) {
		t.Errorf("a stack with no codeaf frames must group as the empty fingerprint, got %q", got)
	}
	if !reFingerprint.MatchString(Fingerprint(nil)) {
		t.Error("the empty fingerprint must still be 16 lowercase hex characters")
	}
}

func TestFingerprintHereMatchesTheRunningStack(t *testing.T) {
	first := FingerprintHere()
	if !reFingerprint.MatchString(first) {
		t.Fatalf("FingerprintHere = %q, want 16 lowercase hex characters", first)
	}
	if again := FingerprintHere(); again != first {
		t.Errorf("two calls from the same frame answered %q and %q", first, again)
	}
	if nested := fingerprintFromHelper(); nested == first {
		t.Error("two different call sites must not share a fingerprint")
	}
}

func fingerprintFromHelper() string { return FingerprintHere() }
