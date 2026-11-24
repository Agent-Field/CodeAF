//go:build linux

package sessionhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestC6_RestartKeepsWorkAndDropsCredentials(t *testing.T) {
	cfg := testConfig(t)
	s, _, procs := testSupervisor(t, cfg, healthyReport())
	createChat(t, s, "alice", chatA)
	createChat(t, s, "alice", chatB)
	kept, _ := layoutFor(cfg.Data, "alice", chatA)
	missing, _ := layoutFor(cfg.Data, "alice", chatB)
	orphan, _ := layoutFor(cfg.Data, "alice", chatC)
	const contents = "keep this workspace"
	file := filepath.Join(kept.work(), "notes.txt")
	if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(missing.dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(orphan.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	procs.live[cfg.UIDBase] = []int{1234}
	procs.live[cfg.UIDBase+1] = []int{2345}

	launcher := newWireTestLauncher()
	restarted := newTestSupervisor(t, cfg, healthyReport(), launcher, procs)
	if len(procs.live) != 0 {
		t.Fatal("processes survived startup")
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != contents {
		t.Fatalf("startup lost the workspace: %q, %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Join(cfg.Data, "quarantine"))
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), chatC+"-") {
		t.Fatalf("unregistered folder was not quarantined: %v, %v", entries, err)
	}
	if _, err := os.Lstat(orphan.dir); !os.IsNotExist(err) {
		t.Fatalf("unregistered folder remains in place: %v", err)
	}
	for _, id := range []string{chatA, chatB} {
		w := request(restarted, "GET", "/v1/sessions/"+id, cfg.Secret, "alice", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"suspended"`) {
			t.Fatalf("restart did not suspend %s: %d %s", id, w.Code, w.Body)
		}
		w = request(restarted, "GET", "/v1/sessions/"+id+"/wire", cfg.Secret, "alice", "")
		wantError(t, w, 409, "This chat has no model access yet.")
	}
	w := request(restarted, "PUT", "/v1/sessions/"+chatB+"/credentials", cfg.Secret, "alice",
		`{"model_url":"http://model.test/v1","model_token":"new-fake-token"}`)
	if w.Code != 204 {
		t.Fatalf("restore model credentials: %d %s", w.Code, w.Body)
	}
	conn := dialWire(t, restarted, chatB)
	defer conn.CloseNow()
	launcher.child(t)
	for _, path := range []string{missing.home(), missing.work(), missing.tmp()} {
		entries, err := os.ReadDir(path)
		if err != nil || len(entries) != 0 {
			t.Fatalf("attach did not recreate an empty chat folder: %v, %v", entries, err)
		}
	}
}

func TestC7_StopClearsTemporaryFilesAndKeepsWork(t *testing.T) {
	cfg := testConfig(t)
	s, _, procs := testSupervisor(t, cfg, healthyReport())
	createChat(t, s, "alice", chatA)
	l, _ := layoutFor(cfg.Data, "alice", chatA)
	file := filepath.Join(l.work(), "notes.txt")
	if err := os.WriteFile(file, []byte("keep this"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(l.tmp(), "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(l.work(), filepath.Join(nested, "work")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nested, 0); err != nil {
		t.Fatal(err)
	}
	procs.live[cfg.UIDBase] = []int{1234}
	for repeat := 0; repeat < 2; repeat++ {
		w := request(s, "POST", "/v1/sessions/"+chatA+"/stop", cfg.Secret, "alice", "")
		if w.Code != 204 || len(procs.live) != 0 {
			t.Fatalf("stop did not complete: %d %s", w.Code, w.Body)
		}
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "keep this" {
		t.Fatalf("stop changed work: %q, %v", data, err)
	}
	if entries, err := os.ReadDir(l.tmp()); err != nil || len(entries) != 0 {
		t.Fatalf("stop left temporary files: %v, %v", entries, err)
	}
	w := request(s, "GET", "/v1/sessions/"+chatA+"/wire", cfg.Secret, "alice", "")
	wantError(t, w, 409, "This chat has no model access yet.")
	w = request(s, "DELETE", "/v1/sessions/"+chatA, cfg.Secret, "alice", "")
	if w.Code != 204 {
		t.Fatalf("delete did not complete: %d %s", w.Code, w.Body)
	}
	if _, err := os.Lstat(l.dir); !os.IsNotExist(err) {
		t.Fatalf("delete left the chat folder: %v", err)
	}
	w = request(s, "GET", "/v1/sessions/"+chatA, cfg.Secret, "alice", "")
	wantError(t, w, 404, "There is no such chat.")
	createChat(t, s, "alice", chatB)
	if s.registry.sessions[chatB].UID != cfg.UIDBase+1 {
		t.Fatal("delete did not quarantine the process identity")
	}
}
