package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// L4-M10a, L4-B15 stability: The capable public command explicitly includes
// local facts, while the existing roster renderer keeps its base empty result.
func TestL4Fix2PublicUnpairedDevices(t *testing.T) {
	celltest.Available(t)
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	t.Setenv("CODEAF_SYNC_URL", "off")
	if _, err := devname.Set(dir, "fresh-laptop"); err != nil {
		t.Fatal(err)
	}
	text, err := captureStdout(t, func() error { return runDevices(nil) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fresh-laptop", "this device", pair.NextPairing} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Count(text, pair.NextPairing) != 1 || strings.Contains(text, "remove one with") || strings.Contains(text, pair.NoDevices) {
		t.Fatal(text)
	}
}
