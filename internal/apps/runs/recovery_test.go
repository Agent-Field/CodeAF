//go:build unix

package runs_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

const followUpNote = `
run: follow-up-note
steps:
  - id: save
    gate: {approval: plan}
    records.create: {type: note, set: {body: Follow up with Acme}}
  - id: mark-saved
    records.update:
      type: note
      id: "{{steps.save.rows[0].id}}"
      set: {saved: true}
`

func TestApprovedLocalWriteRecoversExactlyOnce(t *testing.T) {
	const faultEnv = "CODEAF_TEST_LOCAL_WRITE_KILL_POINT"
	if window := os.Getenv(faultEnv); window != "" {
		crashLocalWrite(t, os.Getenv("CODEAF_TEST_LOCAL_WRITE_ROOT"), window)
		return
	}
	for _, window := range []string{"before-prepare", "after-prepare", "after-write", "after-update"} {
		t.Run(window, func(t *testing.T) {
			root := t.TempDir()
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestApprovedLocalWriteRecoversExactlyOnce$")
			cmd.Env = append(os.Environ(), faultEnv+"="+window, "CODEAF_TEST_LOCAL_WRITE_ROOT="+root)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || t.Context().Err() != nil {
				t.Fatalf("expected SIGKILL at %s, got %v\n%s", window, err, output)
			}
			status, ok := exit.Sys().(syscall.WaitStatus)
			if !ok || status.Signal() != syscall.SIGKILL {
				t.Fatalf("expected SIGKILL at %s, got %v\n%s", window, err, output)
			}

			db := openRecoveryData(t, root)
			before := 0
			if window == "after-write" || window == "after-update" {
				before = 1
			}
			notes := assertNoteCount(t, db, before)
			wantSaved := true
			if window == "after-update" {
				if notes[0]["saved"] != true {
					t.Fatalf("the update did not land before the crash: %+v", notes[0])
				}
				// A replayed update would overwrite this later edit.
				if _, err := db.Update(data.Scope{}, "note", []string{notes[0].ID()}, map[string]any{"saved": false}, data.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
				wantSaved = false
			}
			adapter := runs.Data{Store: func() (*data.Store, error) { return db, nil }}
			cfg := runs.Config{Root: filepath.Join(root, "runs"), Records: adapter.Write}
			for range 2 {
				engine := newEngine(t, followUpNote, cfg)
				tick(t, engine)
				notes := assertNoteCount(t, db, 1)
				if notes[0]["body"] != "Follow up with Acme" || notes[0]["saved"] != wantSaved {
					t.Fatalf("run did not finish with the saved note output: %+v", notes[0])
				}
				list, err := engine.List("crm", "follow-up-note")
				if err != nil || len(list) != 1 || list[0].State != runs.Done {
					t.Fatalf("recovered runs = %+v, %v", list, err)
				}
				if err := engine.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func crashLocalWrite(t *testing.T, root, window string) {
	db := openRecoveryData(t, root)
	// The shared CRM fixture predates the integration run that added notes.
	raw, err := os.ReadFile("../store/testdata/crm/app.json")
	if err != nil {
		t.Fatal(err)
	}
	var app map[string]any
	if err := json.Unmarshal(raw, &app); err != nil {
		t.Fatal(err)
	}
	app["records"].(map[string]any)["note"] = map[string]any{
		"title": "body", "fields": map[string]any{
			"body": map[string]any{"type": "text"}, "saved": map[string]any{"type": "bool"},
		},
	}
	app["capabilities"].(map[string]any)["records"].(map[string]any)["note"] = "rw"
	raw, err = json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RegisterApp(raw); err != nil {
		t.Fatal(err)
	}
	kill := func() {
		t.Helper()
		process, err := os.FindProcess(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		if err := process.Kill(); err != nil {
			t.Fatal(err)
		}
	}
	adapter := runs.Data{Store: func() (*data.Store, error) { return db, nil }}
	engine := newEngine(t, followUpNote, runs.Config{
		Root: filepath.Join(root, "runs"),
		Records: func(ctx context.Context, call runs.RecordCall) (runs.Reply, error) {
			if window == "after-prepare" {
				kill()
			}
			reply, err := adapter.Write(ctx, call)
			if err != nil {
				t.Fatal(err)
			}
			if window == "after-write" && call.Step == "save" || window == "after-update" && call.Step == "mark-saved" {
				kill()
			}
			return reply, nil
		},
	})
	run, err := engine.Start("crm", "follow-up-note", nil)
	if err != nil {
		t.Fatal(err)
	}
	tick(t, engine)
	waiting := state(t, engine, run.ID)
	if waiting.Approval == nil {
		t.Fatalf("run did not request approval: %+v", waiting)
	}
	if err := engine.Answer(run.ID, waiting.Approval.ID, true); err != nil {
		t.Fatal(err)
	}
	if window == "before-prepare" {
		kill()
	}
	tick(t, engine)
	t.Fatal("run missed the requested kill point")
}

func openRecoveryData(t *testing.T, root string) *data.Store {
	t.Helper()
	db, err := data.Open(filepath.Join(root, "data.db"), data.Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertNoteCount(t *testing.T, db *data.Store, want int) []data.Row {
	t.Helper()
	page, err := db.Query(data.Scope{}, "note", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != want {
		t.Fatalf("note count = %d, want %d", len(page.Rows), want)
	}
	return page.Rows
}
