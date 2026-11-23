package pair

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// A used solo home refuses adoption without changing chats or saved keys.
func TestAdoptUsedSoloHomeRequiresReplacement(t *testing.T) {
	r := newChatRig(t)
	if _, err := identity.EnsureSolo(r.homeB); err != nil {
		t.Fatal(err)
	}
	preflightChat(t, r.homeB)
	v, err := keys.Open(r.homeB)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put("p/KEY", keys.Entry{Name: "KEY", Value: "kept", Scope: "p"}); err != nil {
		t.Fatal(err)
	}
	before := filesIn(t, r.homeB)
	beforeA := filesIn(t, r.homeA)
	if _, err := adopt(joiningAt(r.homeB), Grant{Identity: r.a}); !errors.Is(err, ErrDifferentChats) {
		t.Fatalf("adoption = %v", err)
	}
	same(t, before, filesIn(t, r.homeB))
	same(t, beforeA, filesIn(t, r.homeA))
}

// M7b: a used home already sharing another set is unchanged on refusal.
func TestM7AdoptAnotherSetChangesNothing(t *testing.T) {
	r := newChatRig(t)
	if _, err := identity.Ensure(r.homeB); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.homeB, "own-chat"), []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	before := filesIn(t, r.homeB)
	_, err := adopt(joiningAt(r.homeB), Grant{Identity: r.a})
	if !errors.Is(err, ErrDifferentChats) {
		t.Fatalf("M7 refusal = %v", err)
	}
	same(t, before, filesIn(t, r.homeB))
}

func TestLinkFreeRefusesUsedIdentity(t *testing.T) {
	home := t.TempDir()
	if _, err := identity.EnsureSolo(home); err != nil {
		t.Fatal(err)
	}
	preflightChat(t, home)
	before := filesIn(t, home)
	if _, err := (LinkJoining{Joining: Joining{Home: home}}).free(); !errors.Is(err, ErrDifferentChats) {
		t.Fatalf("solo preflight = %v", err)
	}
	same(t, before, filesIn(t, home))
	if err := identity.EndSolo(home); err != nil {
		t.Fatal(err)
	}
	before = filesIn(t, home)
	if _, err := (LinkJoining{Joining: Joining{Home: home}}).free(); !errors.Is(err, ErrDifferentChats) {
		t.Fatalf("paired preflight = %v", err)
	}
	same(t, before, filesIn(t, home))
}

// M5: an interrupted local switch is completed on startup, and retries retain
// the same device rather than creating a second computer in the shared set.
func TestM5InterruptedMergeResumes(t *testing.T) {
	for _, step := range []string{"staged", "recorded", "installed", "keys", "shared", "relay"} {
		t.Run(step, func(t *testing.T) {
			home := t.TempDir()
			old, err := identity.EnsureSolo(home)
			if err != nil {
				t.Fatal(err)
			}
			other, _ := identity.Mint()
			stopped := errors.New("process stopped")
			var saved string
			j := Joining{Home: home, SaveSyncURL: func(url string) error { saved = url; return nil },
				After: func(at string) error {
					if at == step {
						return stopped
					}
					return nil
				}}
			grant := Grant{Identity: other, SyncURL: "http://127.0.0.1:8787"}
			if _, err := adopt(j, grant); !errors.Is(err, stopped) {
				t.Fatalf("M5 stop at %s: %v", step, err)
			}
			if err := Resume(home, j.SaveSyncURL); err != nil {
				t.Fatal(err)
			}
			got, _ := identity.Load(home)
			if got.ID() != other.ID() || got.ID() == old.ID() || identity.Solo(home) || saved != grant.SyncURL {
				t.Fatalf("M5 startup did not finish merge: %s, solo=%v, relay=%q", got.ID(), identity.Solo(home), saved)
			}
			j.After = nil
			if _, err := adopt(j, grant); err != nil {
				t.Fatal(err)
			}

			dev, err := identity.Device(home)
			if err != nil {
				t.Fatal(err)
			}
			if err := Resume(home, j.SaveSyncURL); err != nil {
				t.Fatal(err)
			}
			again, _ := identity.Device(home)
			if dev.ID() != again.ID() {
				t.Fatal("M5 retry duplicated this computer")
			}
		})
	}
}

func TestM8InterruptedExplicitReplaceStaysClean(t *testing.T) {
	for _, step := range []string{"recorded", "staged", "installed", "keys", "shared", "relay"} {
		t.Run(step, func(t *testing.T) {
			home := t.TempDir()
			if _, err := identity.EnsureSolo(home); err != nil {
				t.Fatal(err)
			}
			chat := filepath.Join(home, "v3", "projects", "local", "own-chat")
			if err := os.MkdirAll(chat, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(chat, "transcript.jsonl"), []byte("kept aside"), 0600); err != nil {
				t.Fatal(err)
			}
			other, err := identity.Mint()
			if err != nil {
				t.Fatal(err)
			}
			v, err := keys.Open(home)
			if err != nil {
				t.Fatal(err)
			}
			if err := v.Put("old", keys.Entry{Name: "OLD", Value: "old"}); err != nil {
				t.Fatal(err)
			}
			stopped := errors.New("process stopped")
			_, err = adopt(Joining{Home: home, Replace: true, After: func(at string) error {
				if at == step {
					return stopped
				}
				return nil
			}}, Grant{Identity: other})
			if !errors.Is(err, stopped) {
				t.Fatalf("M8 interruption: %v", err)
			}
			v, err = keys.Open(home)
			if err != nil {
				t.Fatal(err)
			}
			if step != "recorded" {
				if _, err := v.Get("old"); err == nil {
					t.Fatal("replacement retained old key")
				}
			}
			if step == "installed" || step == "keys" || step == "shared" || step == "relay" {
				if err := v.Put("new", keys.Entry{Name: "NEW", Value: "kept"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := Resume(home, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(chat); !os.IsNotExist(err) {
				t.Fatal("M8 old chat visible after recovery")
			}
			v, err = keys.Open(home)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := v.Get("old"); err == nil {
				t.Fatal("recovery retained old key")
			}
			if step == "installed" || step == "keys" || step == "shared" || step == "relay" {
				if got, err := v.Get("new"); err != nil || got.Value != "kept" {
					t.Fatalf("recovery lost new key: %+v, %v", got, err)
				}
			}
			id, err := identity.Load(home)
			if err != nil || id.ID() != other.ID() {
				t.Fatal("M8 replacement did not finish")
			}
		})
	}
}

// A submitted chat refuses before a link is offered, even in the same set.
func TestJoinByLinkWithSubmittedChatsRequiresReplacement(t *testing.T) {
	for _, already := range []bool{false, true} {
		t.Run(map[bool]string{false: "used-solo", true: "already"}[already], func(t *testing.T) {
			r := newLinkRig(t)
			if already {
				if _, err := identity.Adopt(r.new, r.approver.Identity, false); err != nil {
					t.Fatal(err)
				}
			} else if _, err := identity.EnsureSolo(r.new); err != nil {
				t.Fatal(err)
			}
			preflightChat(t, r.new)
			before := filesIn(t, r.new)
			beforeA := filesIn(t, r.old)
			_, done := r.start(t, t.Context())
			if res := waitResult(t, done); !errors.Is(res.err, ErrDifferentChats) {
				t.Fatalf("join=%v", res.err)
			}
			same(t, before, filesIn(t, r.new))
			same(t, beforeA, filesIn(t, r.old))
		})
	}
}
