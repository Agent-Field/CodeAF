package main

import (
	"github.com/Agent-Field/codeaf/internal/devname"
	"runtime"
	"strings"
	"testing"
)

// L4-R8: The public devices command lists this never-paired computer from local facts.
func TestL4FixTerminalUnpairedDevice(t *testing.T) {
	h := t.TempDir()
	t.Setenv("CODEAF_HOME", h)
	t.Setenv("CODEAF_SYNC_URL", "off")
	if _, err := devname.Set(h, "fresh-laptop"); err != nil {
		t.Fatal(err)
	}
	text, err := captureStdout(t, func() error { return runDevices(nil) })
	if err != nil {
		t.Fatal(err)
	}
	// The local facts row holds whether or not an engine is available here; the
	// pairing next step is pinned on the capable road by TestL4Fix2PublicUnpairedDevices,
	// because an unavailable engine keeps the base remote-access route.
	for _, want := range []string{"fresh-laptop", "this device"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "nothing paired") {
		t.Fatal(text)
	}
	if runtime.GOOS == "linux" && !strings.Contains(text, "Linux") {
		t.Fatal(text)
	}
}
