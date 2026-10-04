package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/rotate"
)

// fakeRotation records what the door asked of it.
type fakeRotation struct {
	plan   rotate.Plan
	rotate int
	thawed int
	err    error
}

func (f *fakeRotation) Plan(context.Context) (rotate.Plan, error) { return f.plan, nil }
func (f *fakeRotation) Rotate(context.Context) (rotate.Result, error) {
	f.rotate++
	return rotate.Result{OldID: "id_old", NewID: "id_new", Chats: f.plan.Chats, RetireAfter: 7 * 24 * time.Hour}, f.err
}
func (f *fakeRotation) Abandon(context.Context) error { f.thawed++; return f.err }

func rotationDoor(t *testing.T, f *fakeRotation, stdin string) (identityDoor, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	return identityDoor{
		home: t.TempDir(), out: out, in: strings.NewReader(stdin),
		now: func() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) },
		rotation: func(time.Duration, func(string)) (rotation, func() error, error) {
			return f, func() error { return nil }, nil
		},
	}, out
}

// The door says what the rotation costs before it changes anything, and a no
// changes nothing.
func TestRotateAsksFirst(t *testing.T) {
	f := &fakeRotation{plan: rotate.Plan{Chats: 3, Missing: 1, DownBytes: 5 << 20, UpBytes: 9 << 20, Wait: 3 * time.Minute}}
	d, out := rotationDoor(t, f, "n\n")
	err := identityRotate(d, nil)
	if err == nil || !strings.Contains(err.Error(), "nothing changed") || f.rotate != 0 {
		t.Fatalf("a no = %v, rotations %d", err, f.rotate)
	}
	for _, want := range []string{"3 chats", "1 of them not on this computer (5.0 MB to fetch first)", "9.0 MB goes up", "3 minutes", "cannot be undone"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the plan does not say %q:\n%s", want, out)
		}
	}
}

// A yes, or --yes, runs it, and the card says what a lost computer keeps.
func TestRotateCardListsSecretNames(t *testing.T) {
	f := &fakeRotation{plan: rotate.Plan{Chats: 1}}
	d, out := rotationDoor(t, f, "yes\n")
	vault, err := keys.Open(d.home)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Put("p/K", keys.Entry{Name: "OPENROUTER_API_KEY", Value: "sk-never-printed", Scope: "p"}); err != nil {
		t.Fatal(err)
	}
	if err := identityRotate(d, nil); err != nil || f.rotate != 1 {
		t.Fatalf("rotation = %v, runs %d", err, f.rotate)
	}
	text := out.String()
	for _, want := range []string{"rotated. your chats now belong to id_new (was id_old)", "run /pair here", "deleted on 2026-10-08",
		"still holds everything it had", "change these at their providers: OPENROUTER_API_KEY"} {
		if !strings.Contains(text, want) {
			t.Errorf("the card does not say %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "sk-never-printed") {
		t.Fatal("the card printed a secret's value")
	}
	d2, _ := rotationDoor(t, &fakeRotation{}, "")
	if err := identityRotate(d2, []string{"--yes"}); err != nil {
		t.Fatalf("--yes = %v", err)
	}
}

// A rotation that was started is finished without asking again.
func TestRotateFinishesWhatWasStarted(t *testing.T) {
	f := &fakeRotation{plan: rotate.Plan{Chats: 2}}
	d, out := rotationDoor(t, f, "")
	if err := os.WriteFile(filepath.Join(d.home, rotate.File), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := identityRotate(d, nil); err != nil || f.rotate != 1 {
		t.Fatalf("rotation = %v, runs %d", err, f.rotate)
	}
	if !strings.Contains(out.String(), "finishing the rotation that was started here") || strings.Contains(out.String(), "continue?") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestRotateAbandon(t *testing.T) {
	f := &fakeRotation{}
	d, out := rotationDoor(t, f, "")
	if err := identityRotate(d, []string{"--abandon"}); err != nil || f.thawed != 1 || f.rotate != 0 {
		t.Fatalf("abandon = %v, thaws %d, runs %d", err, f.thawed, f.rotate)
	}
	if !strings.Contains(out.String(), "rotation cancelled; nothing changed") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestRotateRefusesWhatItDoesNotKnow(t *testing.T) {
	d, _ := rotationDoor(t, &fakeRotation{}, "")
	for _, args := range [][]string{{"now"}, {"--bogus"}, {"--grace", "soon"}} {
		if err := identityRotate(d, args); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("%v = %v, want the usage line", args, err)
		}
	}
}

// With sync off there is no relay to rotate on, and nothing is done.
func TestRotateSyncOff(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_SYNC_URL", "off") // an empty value now means the built-in default
	_, _, err := realRotation(0, nil)
	if err == nil || !strings.Contains(err.Error(), "needs sync") {
		t.Fatalf("realRotation with sync off = %v", err)
	}
}

// Stopping a computer that holds your chats names the way to lock it out for good.
func TestDevicesRevokeSuggestsRotate(t *testing.T) {
	chats, _ := chatsWith(t, "laptop")
	said, err := captureStdout(t, func() error { return stopDevice([]deviceKind{chats}, []string{"laptop"}) })
	if err != nil || !strings.Contains(said, "codeaf identity rotate") {
		t.Fatalf("after a stop: %q, %v", said, err)
	}
}

// A computer that rotated hands the identities it replaced to whoever it pairs,
// so a computer still on one of them may follow.
func TestPairGrantNamesWhatItReplaced(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	gone, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.RecordPredecessor(dir, gone.ID()); err != nil {
		t.Fatal(err)
	}
	g, err := pairGrant(pair.Mailbox{URL: "http://relay.test"})
	if err != nil || len(g.Replaces) != 1 || g.Replaces[0] != gone.ID() {
		t.Fatalf("grant replaces %v, %v", g.Replaces, err)
	}
}
