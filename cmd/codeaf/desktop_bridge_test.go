package main

import (
	"strings"
	"testing"
)

func TestDesktopBridgeRefusesNonLoopbackAddress(t *testing.T) {
	err := runDesktopBridge([]string{"--listen", "0.0.0.0:1423"})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("public listener accepted: %v", err)
	}
}
func TestDesktopBridgeRefusesWeakTokenBeforeListening(t *testing.T) {
	t.Setenv("CODEAF_DESKTOP_TOKEN", "short")
	err := runDesktopBridge([]string{"--listen", "127.0.0.1:0", "--workspace", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "32 characters") {
		t.Fatalf("weak token accepted: %v", err)
	}
}
