package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// Contract G3/J19 refuses an unavailable listing without claiming nothing is paired.
func TestDevicesOutageDoesNotClaimNothingIsPaired(t *testing.T) {
	_ = newHostedMoveRig(t)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	relay.Close()
	t.Setenv(syncsetup.URLVar, relay.URL)
	printed, err := captureStdout(t, func() error { return runDevices(nil) })
	if err == nil || !strings.Contains(err.Error(), "cannot be reached from here") {
		t.Errorf("listing must fail with the network sentence: output=%q error=%v", printed, err)
	}
	if strings.TrimSpace(printed) != "" || strings.Contains(printed, pair.NoDevices) {
		t.Errorf("unavailable listing printed an empty-device claim: %q", printed)
	}
}
