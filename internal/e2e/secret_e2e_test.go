//go:build e2e

// secret_e2e_test.go is REQUEST_SECRET, DRIVEN THROUGH THE REAL BINARY IN A REAL
// TERMINAL AGAINST A REAL MODEL.
//
// The unit tests in internal/session prove where the value goes once an answer
// reaches the engine. They cannot prove what a person's terminal does with the
// bytes on the way there, and the first live run of this feature found three
// leaks no unit test had seen: the draft autosave wrote the typed token to
// `v3/draft-*`, the answer's receipt drew it back as `with: <token>`, and the
// model printed its first eight and last four characters as a "fingerprint".
// This scenario is the one that would have caught all three, so it asserts the
// whole home and the whole transcript, not the one record the code meant to
// keep clean.
//
// It costs a fraction of a cent and about a minute:
//
//	go test -tags e2e -run TestRequestSecretE2E -count=1 -timeout 15m -v ./internal/e2e/
package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestRequestSecretE2E(t *testing.T) {
	requireTmuxAndKey(t)
	binary(t)

	// A value shaped like no provider's key, so the registration — not a
	// recognised shape — is what has to keep it out of command output.
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	token := "Zq7-tui-cred-" + hex.EncodeToString(raw)
	fragments := []string{token, token[:10], token[len(token)-8:]}

	home := newHome(t, nil)
	ws := newWorkspace(t, "secret", false)
	r := questionRigAt(t, "secret", home, ws)

	steer(t, r, "I need to hand you a staging deploy token. Ask me for it with the request_secret tool "+
		"(label \"Staging deploy token\", why \"the deploy check reads it\"). When the tool comes back, run "+
		"exactly one shell command, `wc -c < PATH` with the path the tool gave you, and then reply with "+
		"exactly COUNT=<the number it printed>. Do nothing else and never print any part of the token.")

	// THE CARD IS UP, offering its masked box and Decline.
	awaitQuestion(t, r, "Staging deploy token", say(t, "secretDeclineWord"))

	// TYPED, IT IS BULLETS AND A COUNT, never the value.
	type_(t, r, token)
	screen := r.capture()
	for _, part := range fragments {
		if strings.Contains(screen, part) {
			r.dump()
			t.Fatalf("the masked box drew %q of the token:\n%s", part, screen)
		}
	}
	masked := regexp.MustCompile(`[•*]{10,}\s+` + strconv.Itoa(len(token)) + `\b`)
	if !masked.MatchString(screen) {
		r.dump()
		t.Fatalf("the box does not show bullets and the count %d:\n%s", len(token), screen)
	}

	press(t, r, "Enter")

	// THE RECEIPT SAYS IT WAS GIVEN, AND THE MODEL'S COMMAND COUNTED IT FROM THE FILE.
	screen = r.waitFor(questionPatience, say(t, "secretGivenReceipt"), "COUNT="+strconv.Itoa(len(token)))
	for _, part := range fragments {
		if strings.Contains(screen, part) {
			r.dump()
			t.Fatalf("the screen holds %q of the token after the answer:\n%s", part, screen)
		}
	}
	r.quit()

	// THE TRANSCRIPT HOLDS NO PART OF IT: not the whole, not a fingerprint at either end.
	transcripts := sessionTranscripts(t, home)
	if len(transcripts) == 0 {
		t.Fatal("the run wrote no transcript, so nothing below would prove anything")
	}
	for path, body := range transcripts {
		for _, part := range fragments {
			if strings.Contains(body, part) {
				t.Fatalf("%s holds %q of the token", path, part)
			}
		}
		if !strings.Contains(body, "/secrets/staging-deploy-token-") {
			t.Fatalf("%s never shows the model using the kept file", path)
		}
	}

	// AND THE HOME HOLDS IT IN EXACTLY ONE FILE, its own, readable by this user alone.
	// Every other file — the draft, the decision record, the presence file, the
	// journal, the log — holds no fragment of it.
	var kept []string
	_ = filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if string(body) == token && filepath.Base(filepath.Dir(path)) == "secrets" {
			kept = append(kept, path)
			return nil
		}
		for _, part := range fragments {
			if strings.Contains(string(body), part) {
				t.Errorf("%s holds %q of the token", path, part)
			}
		}
		return nil
	})
	if len(kept) != 1 {
		t.Fatalf("want the token kept in one secrets file, got %v", kept)
	}
	if info, err := os.Stat(kept[0]); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the kept file is %v (%v), want 0600", info.Mode().Perm(), err)
	}
	_ = filepath.WalkDir(ws, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if body, err := os.ReadFile(path); err == nil && strings.Contains(string(body), token) {
			t.Errorf("the token was written into the workspace at %s", path)
		}
		return nil
	})
}
