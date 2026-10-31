package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// C1: the manual quotes the closing limit from the same bound as both chat doors.
func TestOncePublicationManualMatchesCloseBudget(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "manual", "chat", "running-from-the-terminal.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(raw)), " ")
	if want := fmt.Sprintf("up to %.0f seconds for the final send and release", driveCloseWithin.Seconds()); !strings.Contains(text, want) {
		t.Fatalf("manual must say %q", want)
	}
}

// C5: doctor has no sync output, writes or requests before pairing, even with --fix.
func TestDoctorNeverPairedMakesNoRelayRequests(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	if _, err := identity.EnsureSolo(root); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	srv := publicationRelay(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(503) }))
	t.Setenv(syncsetup.URLVar, srv.URL)
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, fix := range []bool{false, true} {
		issues, fixes := inspectDoctorSync(fix)
		if len(issues) != 0 || len(fixes) != 0 {
			t.Fatalf("doctor: %v, %v", issues, fixes)
		}
	}
	after, err := os.ReadDir(root)
	if err != nil || len(after) != len(before) {
		t.Fatalf("doctor changed home: %v, %v", after, err)
	}
	for i := range before {
		if after[i].Name() != before[i].Name() {
			t.Fatal("doctor changed home entries")
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("relay requests = %d", requests.Load())
	}
}
