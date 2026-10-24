//go:build e2e

package e2e

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

type c6Stamp struct {
	Mode fs.FileMode
	At   int64
	Hash [32]byte
}

func c6Tree(t *testing.T, root string) map[string]c6Stamp {
	t.Helper()
	result := map[string]c6Stamp{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		var raw []byte
		if !d.IsDir() {
			raw, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		result[rel] = c6Stamp{info.Mode(), info.ModTime().UnixNano(), sha256.Sum256(raw)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// TestC6WholeState proves C29.1/C29.3/C30.2/C30.3/C31.1/C31.2 through the real
// binary, local relay and two independent homes, on both process routes. The
// existing pairing journey separately proves the actual pairing gestures.
func TestC6WholeState(t *testing.T) {
	requireTmuxAndKey(t)
	for _, route := range []string{"in-process", "engine"} {
		t.Run(route, func(t *testing.T) {
			out := filepath.Join(os.Getenv("UX_EVIDENCE"), "c6-"+route)
			if err := os.MkdirAll(out, 0700); err != nil {
				t.Fatal(err)
			}
			frames := func(label string, r *rig) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(out, label+".txt"), []byte(captureStyled(r)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			aHome, bHome := newHome(t, map[string]any{config.KeyChatModel: "stub/older-a"}), newHome(t, map[string]any{config.KeyChatModel: "stub/before-pair"})
			id, err := identity.Ensure(aHome)
			if err != nil {
				t.Fatal(err)
			}
			if err := identity.Save(bHome, id); err != nil {
				t.Fatal(err)
			}
			if _, err := identity.Device(bHome); err != nil {
				t.Fatal(err)
			}
			relay := startOwnRelay(t)
			env := []string{config.APIKeyEnv + "=" + liveKey(t), "CODEAF_SYNC_URL=" + relay, "CODEAF_SYNC_INTERVAL_MS=500"}
			work, other := newWorkspace(t, "original", true), newWorkspace(t, "other", true)
			root := filepath.Dir(filepath.Dir(aHome))
			t.Cleanup(func() {
				for _, pid := range engineDaemonsUnder(root) {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			})
			original := filepath.Join(aHome, "v3", "projects", "legacy-fixture", "old-chat")
			if err := os.MkdirAll(filepath.Join(original, "tasks"), 0700); err != nil {
				t.Fatal(err)
			}
			journal := `{"type":"session","version":1,"id":"old-chat"}` + "\n" + `{"type":"message","role":"user","content":"C29 earlier whole conversation"}` + "\n" + `{"type":"message","role":"assistant","content":"C29 earlier answer is still here"}` + "\n"
			if err := os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(journal), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(original, "tasks", "saved.jsonl"), []byte("retained task journal\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := session.SaveMeta(original, session.Meta{ID: "old-chat", Title: "C29 older conversation", Workspace: work, LastUserAt: time.Now(), Effort: "high", Approval: "allow"}); err != nil {
				t.Fatal(err)
			}
			before := c6Tree(t, original)
			brain, err := store.Open(filepath.Join(aHome, "graph.db"))
			if err != nil {
				t.Fatal(err)
			}
			memory, err := brain.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "C30 hand edited line follows me.", Origin: store.OriginPerson})
			if err != nil {
				t.Fatal(err)
			}
			_ = brain.Close()
			if err := config.WriteChatModel(aHome, e2eModel); err != nil {
				t.Fatal(err)
			}
			args := []string{"chat", "--session", filepath.Join(original, "transcript.jsonl"), "--one-model"}
			if route == "in-process" {
				args = append(args, "--no-host")
			}
			a := startWithEnv(t, env, "c6-a-"+route, aHome, work, 160, 45, args...)
			a.skipSetup(t)
			screen, ok := waitPlain(a, 40*time.Second, "C29 earlier answer is still here")
			if !ok {
				frames("a-open-failure", a)
				t.Fatalf("older conversation did not open: %s", screen)
			}
			frames("a-older-open", a)
			a.lit("!printf 'C31 %s receipt\\n' quit")
			a.keys("Enter")
			if _, ok := waitPlain(a, 40*time.Second, "C31 quit receipt"); !ok {
				t.Fatal("shell turn did not finish")
			}
			a.lit("/quit")
			a.keys("Enter")
			// The directory, rather than a fixed sleep, is the durable quit receipt.
			copied := cell.Copied(original)
			if copied == original {
				t.Fatal("older chat has no copy")
			}
			t.Setenv(syncsetup.URLVar, relay)
			sync, ok, err := syncsetup.Open(aHome)
			if err != nil || !ok {
				t.Fatalf("open local sync: %v", err)
			}
			released := false
			for until := time.Now().Add(40 * time.Second); time.Now().Before(until); {
				view, err := sync.Dir.Cell(t.Context(), filepath.Base(copied))
				if err == nil && view.Cell.Lease.Expires == 0 {
					released = true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !released {
				frames("a-quit-not-released", a)
				view, err := sync.Dir.Cell(t.Context(), filepath.Base(copied))
				t.Logf("C31.1 last directory view: %+v; %v", view, err)
				_ = filepath.WalkDir(aHome, func(path string, d fs.DirEntry, err error) error {
					if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".log") {
						raw, _ := os.ReadFile(path)
						rel, _ := filepath.Rel(aHome, path)
						t.Logf("%s: %s", rel, raw)
					}
					return nil
				})
				t.Fatal("C31.1 quit did not release its durable chat")
			}

			bArgs := []string{}
			if route == "in-process" {
				bArgs = []string{"chat", "--no-host"}
			}
			b := startWithEnv(t, env, "c6-b-"+route, bHome, other, 180, 50, bArgs...)
			b.skipSetup(t)
			screen, ok = waitPlain(b, 50*time.Second, "Moved from", "C29 earlier answer is still here")
			frames("b-automatic-arrival", b)
			if !ok {
				t.Fatalf("C31.1 bare launch did not continue automatically: %s", screen)
			}
			// The copied original and its task journal remain independent after use.
			if !reflect.DeepEqual(before, c6Tree(t, original)) {
				t.Fatal("C29 original changed")
			}
			bb, err := store.Open(filepath.Join(bHome, "graph.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer bb.Close()
			deadline := time.Now().Add(15 * time.Second)
			found := false
			for time.Now().Before(deadline) {
				m, yes, _ := bb.MemoryRecord(memory.ID)
				if yes && m.Text == memory.Text {
					found = true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !found {
				t.Fatal("C30 hand edited memory did not arrive")
			}
			if got := config.ChatModelAt(bHome); got != e2eModel {
				t.Fatalf("C30 model pick did not arrive: %q", got)
			}
			b.keys("Escape")
			b.lit("!printf 'C31 %s receipt\\n' asleep")
			b.keys("Enter")
			if _, ok := waitPlain(b, 30*time.Second, "C31 asleep receipt"); !ok {
				t.Fatal("second shell turn failed")
			}
			frames("b-before-lid", b)
			b.loseLid()
			// Automatic arrival created a workspace under B's home, beyond
			// the original workspace known to loseLid. A sleeping computer
			// stops that engine too; leaving it alive correctly forbids rescue.
			for _, pid := range engineDaemonsUnder(bHome) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
			a.kill()
			a = startWithEnv(t, env, "c6-a-return-"+route, aHome, work, 180, 50, bArgs...)
			a.skipSetup(t)
			a.lit("/home")
			a.keys("Enter")
			if _, ok := waitPlain(a, 30*time.Second, "latest chat here?"); !ok {
				frames("a-no-sleep-offer", a)
				t.Fatal("C31.2 sleeping computer was not offered")
			}
			frames("a-asleep-offer", a)
			a.keys("M-c")
			screen, ok = waitPlain(a, 40*time.Second, "Moved from")
			frames("a-one-key-arrival", a)
			if !ok || strings.Contains(screen, "Continue this chat here?") {
				t.Fatalf("C31.2 clean sleeping continuation required another key: %s", screen)
			}
			if !reflect.DeepEqual(before, c6Tree(t, original)) {
				t.Fatal("C29 take-back changed original")
			}
			fmt.Fprintln(os.Stdout, "C6 whole-state journey passed", route)
		})
	}
}
