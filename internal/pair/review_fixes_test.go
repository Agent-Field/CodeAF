package pair

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// M1/M5: a writer opened before pairing must keep both saved keys when it
// overlaps the staged switch, and must remain usable after the switch.
func TestR1_1SavedKeyWriterOverlapsSwitch(t *testing.T) {
	home := t.TempDir()
	v, err := keys.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put("before", keys.Entry{Name: "BEFORE", Value: "kept"}); err != nil {
		t.Fatal(err)
	}
	other, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	written := make(chan error, 1)
	j := Joining{Home: home, After: func(step string) error {
		if step == "staged" {
			go func() { written <- v.Put("during", keys.Entry{Name: "DURING", Value: "kept"}) }()
			select {
			case err := <-written:
				written <- err
			case <-time.After(50 * time.Millisecond):
			}
		}
		return nil
	}}
	if _, err := adopt(j, Grant{Identity: other}); err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if err := v.Put("after", keys.Entry{Name: "AFTER", Value: "kept"}); err != nil {
		t.Fatalf("old handle after switch: %v", err)
	}
	again, err := keys.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"before", "during", "after"} {
		if got, err := again.Get(id); err != nil || got.Value != "kept" {
			t.Errorf("saved key %s after switch = %+v, %v", id, got, err)
		}
	}
}

// M5/M8: recovery after the destination was installed must keep new state,
// including when it resumes an older journal without a phase field.
func TestR1_2ReplaceRecoveryKeepsStateMadeAfterInstall(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "phased", true: "older-record"}[legacy], func(t *testing.T) {
			home := t.TempDir()
			if _, err := identity.EnsureSolo(home); err != nil {
				t.Fatal(err)
			}
			other, err := identity.Mint()
			if err != nil {
				t.Fatal(err)
			}
			stopped := errors.New("process stopped")
			_, err = adopt(Joining{Home: home, Replace: true, After: func(step string) error {
				if step == "installed" {
					return stopped
				}
				return nil
			}}, Grant{Identity: other})
			if !errors.Is(err, stopped) {
				t.Fatalf("stop after install: %v", err)
			}
			if legacy {
				raw, err := os.ReadFile(filepath.Join(home, mergeFile))
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				delete(fields, "phase")
				raw, err = json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(home, mergeFile), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			chat := filepath.Join(home, "v3", "projects", "local", "new-chat", "transcript.jsonl")
			if err := os.MkdirAll(filepath.Dir(chat), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(chat, []byte("new history"), 0600); err != nil {
				t.Fatal(err)
			}
			v, err := keys.Open(home)
			if err != nil {
				t.Fatal(err)
			}
			if err := v.Put("new", keys.Entry{Name: "NEW", Value: "kept"}); err != nil {
				t.Fatal(err)
			}
			if err := config.ApplyPreferences(home, map[string]string{config.KeyEffort: `"high"`}); err != nil {
				t.Fatal(err)
			}
			if err := Resume(home, nil); err != nil {
				t.Fatal(err)
			}
			if raw, err := os.ReadFile(chat); err != nil || string(raw) != "new history" {
				t.Errorf("new chat after recovery: %q, %v", raw, err)
			}
			if got, err := v.Get("new"); err != nil || got.Value != "kept" {
				t.Errorf("new key after recovery: %+v, %v", got, err)
			}
			if prefs, err := config.ReadPreferences(home); err != nil || prefs[config.KeyEffort] != `"high"` {
				t.Errorf("new preferences after recovery: %+v, %v", prefs, err)
			}
		})
	}
}
