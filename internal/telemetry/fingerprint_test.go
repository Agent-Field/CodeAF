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
		"some.dependency/pkg.(*Thing).Do(0x1)",
		"\t/go/pkg/mod/some.dependency/pkg@v1.2.3/thing.go:9 +0x10",
		"runtime.gopark(0x0, 0x0)",
		"\t/usr/local/go/src/runtime/proc.go:417 +0x5",
	}, "\n")
	if got := Fingerprint([]byte(foreign)); got != Fingerprint(nil) {
		t.Errorf("a stack with no codeaf frames must group as the empty fingerprint, got %q", got)
	}
	if !reFingerprint.MatchString(Fingerprint(nil)) {
		t.Error("the empty fingerprint must still be 16 lowercase hex characters")
	}
	// main frames ARE codeaf's own — cmd/codeaf prints under main. — so a
	// crash there groups by its function names, not as an empty fingerprint.
	mainOnly := strings.Join([]string{
		"main.execute(0x14000010350)",
		"\t/Users/santosh/secret-project/cmd/codeaf/main.go:31 +0x4",
	}, "\n")
	if got := Fingerprint([]byte(mainOnly)); got == Fingerprint(nil) {
		t.Error("a crash in cmd/codeaf must carry its main frames, not group as empty")
	}
}

// stackAbsPaths and stackTrimPaths are one literal, realistic two-goroutine
// debug.Stack() sample of one crash, as a local build prints it (absolute
// source paths) and as every release build prints it (-trimpath, where the
// file halves ALSO start with the module path). The frames are identical.
const stackAbsPaths = `goroutine 18 [running]:
github.com/Agent-Field/codeaf/internal/session.(*Runner).Step(0x14000106008, {0x1400011a090, 0x2})
	/Users/santosh/secret-project/internal/session/runner.go:212 +0x104
github.com/Agent-Field/codeaf/internal/session.Run(0x14000106008, 0x1)
	/Users/santosh/secret-project/internal/session/session.go:88 +0x9c
created by github.com/Agent-Field/codeaf/internal/session.(*Runner).Start in goroutine 1
	/Users/santosh/secret-project/internal/session/start.go:41 +0x1d4

goroutine 1 [chan receive]:
github.com/Agent-Field/codeaf/cmd/codeaf.runTask(0x14000010350, {0x1400012a008, 0x3, 0x4})
	/Users/santosh/secret-project/cmd/codeaf/main.go:140 +0xd8
main.main()
	/Users/santosh/secret-project/cmd/codeaf/main.go:64 +0x228
runtime.goexit()
	/usr/local/go/src/runtime/asm_amd64.s:1650 +0xbe`

const stackTrimPaths = `goroutine 18 [running]:
github.com/Agent-Field/codeaf/internal/session.(*Runner).Step(0x14000106008, {0x1400011a090, 0x2})
	github.com/Agent-Field/codeaf/internal/session/runner.go:212 +0x104
github.com/Agent-Field/codeaf/internal/session.Run(0x14000106008, 0x1)
	github.com/Agent-Field/codeaf/internal/session/session.go:88 +0x9c
created by github.com/Agent-Field/codeaf/internal/session.(*Runner).Start in goroutine 1
	github.com/Agent-Field/codeaf/internal/session/start.go:41 +0x1d4

goroutine 1 [chan receive]:
github.com/Agent-Field/codeaf/cmd/codeaf.runTask(0x14000010350, {0x1400012a008, 0x3, 0x4})
	github.com/Agent-Field/codeaf/cmd/codeaf/main.go:140 +0xd8
main.main()
	github.com/Agent-Field/codeaf/cmd/codeaf/main.go:64 +0x228
runtime.goexit()
	/usr/local/go/src/runtime/asm_amd64.s:1650 +0xbe`

func TestFingerprintStableAcrossStackForms(t *testing.T) {
	abs := Fingerprint([]byte(stackAbsPaths))
	if trim := Fingerprint([]byte(stackTrimPaths)); trim != abs {
		t.Errorf("the same crash in a -trimpath release build fingerprinted differently: %q against %q", trim, abs)
	}
	// Copies differing only in pointer values and line numbers: the same
	// crash on a next run, on another machine, after a rebuild.
	drift := stackAbsPaths
	for old, next := range map[string]string{
		"0x14000106008": "0x1400204a6a08",
		"0x1400011a090": "0x1400204a7b90",
		"0x14000010350": "0x1400204a8350",
		"0x1400012a008": "0x1400204a9008",
		"runner.go:212": "runner.go:981",
		"session.go:88": "session.go:9",
		"main.go:140":   "main.go:7",
		"main.go:64":    "main.go:3",
		"+0x104":        "+0x9c",
		"+0x1d4":        "+0x208",
	} {
		drift = strings.ReplaceAll(drift, old, next)
	}
	if got := Fingerprint([]byte(drift)); got != abs {
		t.Errorf("pointer values and line numbers moved the fingerprint: %q against %q", got, abs)
	}
	// One changed function name is a different fault and must be seen as one.
	renamed := strings.ReplaceAll(stackAbsPaths, "(*Runner).Step", "(*Runner).Stop")
	if got := Fingerprint([]byte(renamed)); got == abs {
		t.Error("a changed function name must move the fingerprint")
	}
	// The four function names alone, bare: everything else in the sample is
	// noise: goroutine headers, created-by footers, both path forms.
	bare := strings.Join([]string{
		"github.com/Agent-Field/codeaf/internal/session.(*Runner).Step",
		"github.com/Agent-Field/codeaf/internal/session.Run",
		"github.com/Agent-Field/codeaf/cmd/codeaf.runTask",
		"main.main",
	}, "\n")
	if got := Fingerprint([]byte(bare)); got != abs {
		t.Errorf("the printed stack and its bare function names fingerprinted differently: %q against %q", got, abs)
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
