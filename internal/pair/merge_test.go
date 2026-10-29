package pair

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// M1/M8: a used solo home joins without replacement, retaining readable chats
// and saved keys. M4: adoption writes nothing to the offering home.
func TestM1M4M8AdoptUsedSoloHome(t *testing.T) {
	r := newChatRig(t)
	if _, err := identity.EnsureSolo(r.homeB); err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(r.homeB, "v3", "projects", "local", "saved", "transcript.jsonl")
	want := []byte("first question\nfirst answer\nsecond question\nsecond answer\n")
	if err := os.MkdirAll(filepath.Dir(chat), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chat, want, 0600); err != nil {
		t.Fatal(err)
	}
	v, err := keys.Open(r.homeB)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put("p/KEY", keys.Entry{Name: "KEY", Value: "kept", Scope: "p"}); err != nil {
		t.Fatal(err)
	}
	beforeA := filesIn(t, r.homeA)
	joined, _, err := pairOnce(t, r, joiningAt(r.homeB))
	if err != nil || joined.Already || joined.Successor {
		t.Fatalf("M1 join: %+v, %v", joined, err)
	}
	got, err := os.ReadFile(chat)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("M1 history changed: %q, %v", got, err)
	}
	v, err = keys.Open(r.homeB)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := v.Get("p/KEY"); err != nil || got.Value != "kept" {
		t.Fatalf("M1 saved key: %+v, %v", got, err)
	}
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

func TestM1M6LinkFreeAcceptsUsedSoloAndDefersGrantChecks(t *testing.T) {
	home := t.TempDir()
	if _, err := identity.EnsureSolo(home); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "own-chat"), []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (LinkJoining{Joining: Joining{Home: home}}).free(); err != nil {
		t.Fatalf("M1 link preflight: %v", err)
	}
	// Until the grant arrives this could also be the same set or a successor.
	if err := identity.EndSolo(home); err != nil {
		t.Fatal(err)
	}
	if _, err := (LinkJoining{Joining: Joining{Home: home}}).free(); err != nil {
		t.Fatalf("M6 link preflight: %v", err)
	}
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
			v, err := keys.Open(home)
			if err != nil {
				t.Fatal(err)
			}
			if err := v.Put("p/KEY", keys.Entry{Name: "KEY", Value: "kept", Scope: "p"}); err != nil {
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
			v, err = keys.Open(home)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := v.Get("p/KEY"); err != nil || got.Value != "kept" {
				t.Fatalf("M5 unreadable while interrupted: %+v, %v", got, err)
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
	for _, step := range []string{"recorded", "staged", "installed"} {
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
			if err := Resume(home, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(chat); !os.IsNotExist(err) {
				t.Fatal("M8 old chat visible after recovery")
			}
			id, err := identity.Load(home)
			if err != nil || id.ID() != other.ID() {
				t.Fatal("M8 replacement did not finish")
			}
		})
	}
}

// M1/M4/M6 exercise the real link approval service for used solo homes and
// a computer already in the same set, preserving its device certificate.
func TestM1M4M6JoinByLinkPreservesLocalState(t *testing.T) {
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
			chat := filepath.Join(r.new, "own-chat")
			want := []byte("question 1\nanswer 1\nquestion 2\nanswer 2\n")
			if err := os.WriteFile(chat, want, 0600); err != nil {
				t.Fatal(err)
			}
			v, err := keys.Open(r.new)
			if err != nil {
				t.Fatal(err)
			}
			if err := v.Put("p/KEY", keys.Entry{Name: "KEY", Value: "kept", Scope: "p"}); err != nil {
				t.Fatal(err)
			}
			before := filesIn(t, r.new)
			beforeA := filesIn(t, r.old)
			ui, done := r.start(t, t.Context())
			asking, err := r.approver.Look(t.Context(), waitInvite(t, ui).Ref)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.approver.Approve(t.Context(), asking); err != nil {
				t.Fatal(err)
			}
			res := waitResult(t, done)
			if res.err != nil || res.joined.Already != already {
				t.Fatalf("M1/M6 join: %+v, %v", res.joined, res.err)
			}
			if got, _ := os.ReadFile(chat); !bytes.Equal(got, want) {
				t.Fatal("M1 history changed")
			}
			v, err = keys.Open(r.new)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := v.Get("p/KEY"); err != nil || got.Value != "kept" {
				t.Fatalf("M1 saved key: %+v, %v", got, err)
			}
			if already {
				same(t, before, filesIn(t, r.new))
			}
			same(t, beforeA, filesIn(t, r.old))
		})
	}
}
