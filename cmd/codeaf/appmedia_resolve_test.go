package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAppMediaResolveActualWireUsesOwnedRecordedEvidence(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	defer func() { appMediaIndex = old }()
	owner, _, err := backend.mediaScope(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agent.Workspace, "chart.png")
	data := writePNG(t, path, 40, 30)
	fact := session.GeneratedMedia(path, data, "")
	fact.Origin = session.MediaEvidence
	fact.Verb = "looked_at"
	fact.Tool = "actual-look"
	fact.Run = "actual-run"
	id := session.RecordMedia(index, session.Artifact{Path: path, Session: owner, Title: "chart", Kind: "image", Created: time.Now()}, fact)
	path = fact.Canonical
	foreign := filepath.Join(agent.Workspace, "foreign.png")
	other := session.GeneratedMedia(foreign, writePNG(t, foreign, 10, 10), "")
	session.RecordMedia(index, session.Artifact{Path: foreign, Session: "other", Title: "foreign", Kind: "image", Created: time.Now()}, other)
	unrecorded := filepath.Join(agent.Workspace, "unrecorded.png")
	writePNG(t, unrecorded, 8, 8)

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	server := appwire.NewServer(outW)
	go func() {
		defer close(done)
		defer outW.Close()
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
	}()
	t.Cleanup(func() { _ = inW.Close(); _ = outR.Close(); <-done })
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 64<<10), appwire.MediaReplyBudget)
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	seq := 0
	call := func(path, version string) (appwire.MediaSource, *appwire.Error) {
		t.Helper()
		seq++
		encoded, _ := json.Marshal(map[string]any{"id": seq, "method": "media.resolve", "params": map[string]string{"agent": agent.ID, "path": path, "expect_version": version}})
		if _, err := inW.Write(append(encoded, '\n')); err != nil {
			t.Fatal(err)
		}
		for scanner.Scan() {
			var reply struct {
				ID     int                 `json:"id"`
				Result appwire.MediaResult `json:"result"`
				Error  *appwire.Error      `json:"error"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
			if reply.ID == seq {
				return reply.Result.Source, reply.Error
			}
		}
		t.Fatalf("no resolve reply: %v", scanner.Err())
		return appwire.MediaSource{}, nil
	}
	got, wire := call(path, "")
	if wire != nil || got.ID != id || got.Origin != "evidence" || got.Verb != "looked_at" || got.Tool != "actual-look" || got.Run != "actual-run" || got.State != appwire.MediaAvailable || got.Width == nil || *got.Width != 40 {
		t.Fatalf("reference %+v %v", got, wire)
	}
	for _, version := range []string{got.Version, got.LiveVersion} {
		if same, err := call(path, version); err != nil || same.ID != id {
			t.Fatalf("version %q %+v %v", version, same, err)
		}
	}
	if _, err := call(path, "sha256:wrong"); err == nil || err.Code != appwire.CodeConflict {
		t.Fatalf("version conflict %v", err)
	}
	for _, query := range []string{other.Canonical, unrecorded} {
		if _, err := call(query, ""); err == nil || err.Code != appwire.CodeNotFound {
			t.Fatalf("discovery %s %v", query, err)
		}
	}
	if _, err := call("chart.png", ""); err == nil || err.Code != appwire.CodeBadParams {
		t.Fatalf("relative path %v", err)
	}
	writePNG(t, path, 41, 30)
	if changed, err := call(path, got.Version); err != nil || changed.State != appwire.MediaChanged || changed.ID != id {
		t.Fatalf("changed %+v %v", changed, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if gone, err := call(path, ""); err != nil || gone.State != appwire.MediaMissing {
		t.Fatalf("gone %+v %v", gone, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(index)); len(entries) != 1 {
		t.Fatalf("resolution created new records/files: %v", entries)
	}
	rows, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	// Exactly the two recorded creation lines; resolving never appends a source.
	count := 0
	for _, b := range rows {
		if b == '\n' {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("resolver appended %d records", count)
	}
}
