package pair

import (
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/identity"
	"os"
	"path/filepath"
	"testing"
)

func TestSameIdentityReplacementClearsOldState(t *testing.T) {
	home := t.TempDir()
	id, err := identity.EnsureSolo(home)
	if err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(home, "v3", "projects", "local", "old-chat", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(chat), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chat, []byte("old chat"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"model.talk":"stub/old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := adopt(Joining{Home: home, Replace: true}, Grant{Identity: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(chat); !os.IsNotExist(err) {
		t.Errorf("old chat still in live tree: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var preferences map[string]json.RawMessage
	if err := json.Unmarshal(raw, &preferences); err != nil {
		t.Fatal(err)
	}
	if _, ok := preferences["model.talk"]; ok {
		t.Errorf("old model.talk survived replacement: %s", raw)
	}
}

func TestSameIdentityReplacementRecoveryPreservesInstalledWrites(t *testing.T) {
	for _, stop := range []string{"staged", "installed", "installed-before-progress"} {
		t.Run(stop, func(t *testing.T) {
			home := t.TempDir()
			id, err := identity.EnsureSolo(home)
			if err != nil {
				t.Fatal(err)
			}
			stopped := errors.New("process stopped")
			_, err = adopt(Joining{Home: home, Replace: true, After: func(step string) error {
				if step == stop || stop == "installed-before-progress" && step == "installed" {
					return stopped
				}
				return nil
			}}, Grant{Identity: id})
			if !errors.Is(err, stopped) {
				t.Fatalf("interruption: %v", err)
			}
			if stop == "installed-before-progress" {
				// Simulate installation succeeding before the phase write reaches disk.
				raw, err := os.ReadFile(filepath.Join(home, mergeFile))
				if err != nil {
					t.Fatal(err)
				}
				var rec mergeRecord
				if err := json.Unmarshal(raw, &rec); err != nil {
					t.Fatal(err)
				}
				rec.Phase = "prepared"
				if err := rec.save(home); err != nil {
					t.Fatal(err)
				}
			}
			chat := filepath.Join(home, "v3", "projects", "local", "during", "transcript.jsonl")
			if err := os.MkdirAll(filepath.Dir(chat), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(chat, []byte("during"), 0600); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(home, "config.json")
			if err := os.WriteFile(config, []byte(`{"model.talk":"during/model"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := Resume(home, nil); err != nil {
				t.Fatal(err)
			}
			_, chatErr := os.Stat(chat)
			raw, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			var preferences map[string]json.RawMessage
			if err := json.Unmarshal(raw, &preferences); err != nil {
				t.Fatal(err)
			}
			_, hasModel := preferences["model.talk"]
			if stop == "staged" {
				if !os.IsNotExist(chatErr) || hasModel {
					t.Fatalf("pre-install writes survived: chat=%v config=%s", chatErr, raw)
				}
			} else if chatErr != nil || string(preferences["model.talk"]) != `"during/model"` {
				t.Fatalf("post-install writes lost: chat=%v config=%s", chatErr, raw)
			}
		})
	}
}
