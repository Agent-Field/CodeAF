package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// C27.3 keeps the reachable-machine door when no engine can move chats.
func TestDevicesWithoutMovingChatsKeepsTheRemoteAccessRoute(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Cleanup(cell.UseAvailabilityInTests(false))
	saved := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = saved }()
	err = runDevices(nil)
	w.Close()
	output, readErr := io.ReadAll(r)
	r.Close()
	if err != nil || readErr != nil {
		t.Fatalf("devices: %v %v", err, readErr)
	}
	for _, want := range []string{"this machine is reachable as ", "no devices are paired with this machine.", "run `codeaf serve` here and `codeaf chat --at "} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("missing %q: %s", want, output)
		}
	}
	if strings.Contains(string(output), "codeaf pair") {
		t.Fatalf("suggested unavailable pairing: %s", output)
	}
}
