package main

// The install's nonce and the push that rides it: what the file holds, what
// replaces a mangled one, and what one push does to the outbox at a relay
// that answers and at one that does not.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
)

var hexWord = regexp.MustCompile(`^[0-9a-f]{32}$`)

// The nonce is drawn once and kept: a second call in the same pool directory
// reads the same file and answers the same 32 lowercase hex characters, and
// the file sits at mode 0600 in a directory of mode 0700.
func TestInstallNonceDrawsOnceAndKeepsTheWord(t *testing.T) {
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "pool")

	first, err := installNonce(poolDir)
	if err != nil {
		t.Fatalf("installNonce: %v", err)
	}
	if len(first) != 32 || !hexWord.MatchString(first) {
		t.Fatalf("the nonce is %q, want 32 lowercase hex characters", first)
	}
	second, err := installNonce(poolDir)
	if err != nil {
		t.Fatalf("installNonce again: %v", err)
	}
	if second != first {
		t.Fatalf("the second draw answered %q, want the kept %q", second, first)
	}

	info, err := os.Stat(filepath.Join(poolDir, "install"))
	if err != nil {
		t.Fatalf("the nonce file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("the nonce file is %v, want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(poolDir)
	if err != nil {
		t.Fatalf("the pool directory: %v", err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("the pool directory is %v, want 0700", dirInfo.Mode().Perm())
	}

	// A nonce drawn here is not the one another install drew: two pool
	// directories hold two words.
	other, err := installNonce(filepath.Join(t.TempDir(), "pool"))
	if err != nil {
		t.Fatalf("installNonce elsewhere: %v", err)
	}
	if other == first {
		t.Fatal("two installs drew the same nonce")
	}
}

// A file that does not hold the one accepted shape is replaced the way a
// missing one is: a new draw, written over it. Whatever mangled the file — a
// half write, an older shape — the install is sending again afterwards.
func TestInstallNonceReplacesAMangledFile(t *testing.T) {
	for name, mangled := range map[string]string{
		"not hex":            "not a nonce at all",
		"too short":          "abcd1234",
		"upper case":         strings.ToUpper("0123456789abcdef0123456789abcdef"),
		"the empty file":     "",
		"right length, junk": "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
	} {
		dir := filepath.Join(t.TempDir(), "pool")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "install"), []byte(mangled), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := installNonce(dir)
		if err != nil {
			t.Fatalf("%s: installNonce: %v", name, err)
		}
		if !hexWord.MatchString(got) {
			t.Fatalf("%s: the mangled file was not replaced: %q", name, got)
		}
		if back, err := installNonce(dir); err != nil || back != got {
			t.Fatalf("%s: the replacement was not kept: %q, %v", name, back, err)
		}
	}
}

// A pool directory that cannot be created is an error, not a silence.
func TestInstallNonceRefusesAPlaceItCannotWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := installNonce(filepath.Join(file, "pool")); err == nil {
		t.Fatal("a nonce was drawn over a file")
	}
}

// ── THE PUSH ────────────────────────────────────────────────────────────────

// A push is the outbox's pending rows handed to the relay under the install's
// nonce: one POST, the header carrying the word the nonce file holds, and
// every row it reached marked sent in the file.
func TestPoolPushPostsPendingRowsUnderTheInstallNonce(t *testing.T) {
	var installs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		installs = append(installs, r.Header.Get("X-Codeaf-Install"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := seedOutbox(t)
	submit := oneEnv("CODEAF_MODEL_POOL_SUBMIT_URL", srv.URL)
	cfg := poolcfg.Resolve("", "", submit)
	poolPush(context.Background(), dir, cfg, 2*time.Second)

	if len(installs) != 1 {
		t.Fatalf("the relay was asked %d times, want once: %v", len(installs), installs)
	}
	if !hexWord.MatchString(installs[0]) {
		t.Fatalf("the batch rode with X-Codeaf-Install %q, want 32 lowercase hex", installs[0])
	}
	nonce, err := installNonce(configuredPoolDir(dir))
	if err != nil {
		t.Fatalf("the nonce file: %v", err)
	}
	if installs[0] != nonce {
		t.Fatalf("the header carried %q, want the word the nonce file holds %q", installs[0], nonce)
	}

	// Every row the relay took is marked sent: the same outbox, reopened,
	// holds nothing pending.
	box, err := outboxOpenForTest(dir)
	if err != nil {
		t.Fatalf("the outbox: %v", err)
	}
	defer box.Close()
	if got := len(box.Pending()); got != 0 {
		t.Fatalf("%d row(s) still pending after a delivered push", got)
	}
}

// A relay that does not answer is an ordinary state: the push says nothing,
// prints nothing, and every row stays pending where the next push finds it.
func TestPoolPushLeavesTheRowsPendingWhenTheRelayDoesNotAnswer(t *testing.T) {
	dir := seedOutbox(t)
	// A dead loopback port: the address the relay would have answered at, if
	// the relay were there.
	submit := oneEnv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/v1/rows")
	cfg := poolcfg.Resolve("", "", submit)

	said, err := captureStdout(t, func() error {
		poolPush(context.Background(), dir, cfg, time.Second)
		return nil
	})
	if err != nil {
		t.Fatalf("the push: %v", err)
	}
	if said != "" {
		t.Fatalf("a push nothing waits for said %q", said)
	}

	box, err := outboxOpenForTest(dir)
	if err != nil {
		t.Fatalf("the outbox: %v", err)
	}
	defer box.Close()
	if got := len(box.Pending()); got != 2 {
		t.Fatalf("%d row(s) pending after an unanswered push, want the 2 seeded", got)
	}
}

// A mode that does not send pushes nothing at all: no outbox opened, no
// nonce drawn, no file written under the pool directory.
func TestPoolPushDoesNothingWhenTheModeDoesNotSend(t *testing.T) {
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "pool")
	if err := os.MkdirAll(poolDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := poolcfg.Resolve("read", "", oneEnv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/v1/rows"))
	poolPush(context.Background(), dir, cfg, time.Second)
	if _, err := os.Stat(filepath.Join(poolDir, "install")); !os.IsNotExist(err) {
		t.Fatal("a read-only pool drew a nonce")
	}
	if _, err := os.Stat(filepath.Join(poolDir, "outbox.jsonl")); !os.IsNotExist(err) {
		t.Fatal("a read-only pool opened the outbox")
	}
}

// configuredPoolDir is the pool directory a push under profileDir writes
// into, the same reading the push itself makes.
func configuredPoolDir(profileDir string) string {
	return filepath.Join(profileDir, "pool")
}

// outboxOpenForTest opens the outbox file the way a reading test reads it
// back — the same path poolPush writes through config.ProfilePath.
func outboxOpenForTest(dir string) (*outbox.Outbox, error) {
	return outbox.Open(filepath.Join(dir, "pool", "outbox.jsonl"))
}
