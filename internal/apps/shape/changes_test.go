package shape

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestChangeStoreCancelDiffAndRestart(t *testing.T) {
	for _, tt := range []struct {
		name   string
		cancel string
		want   spec.Files
	}{
		{"remove first independent edit", "c1", spec.Files{"app.json": []byte("original"), "agent.md": []byte("new agent")}},
		{"remove second edit", "c2", spec.Files{"app.json": []byte("changed"), "agent.md": []byte("old agent")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, cfg, all, scope := setupDraft(t)
			d := openDraft(t, s, scope)
			if err := d.Rebase(1, spec.Files{"app.json": []byte("original"), "agent.md": []byte("old agent")}); err != nil {
				t.Fatal(err)
			}
			events, cancel := all.Hub.Subscribe(bridge.TopicShape("crm", svc.Owner))
			defer cancel()
			for _, change := range []bridge.Change{{ID: "c1", Request: "rename value", From: "user", State: "checking"}, {ID: "c2", Request: "change agent", From: "plan", State: "checking"}} {
				if err := d.AddChange(change, nil); err != nil {
					t.Fatal(err)
				}
				needEvent(t, events, svc.Owner, "checking")
			}
			if err := d.ApplyFiles("c1", spec.Files{"app.json": []byte("changed"), "agent.md": []byte("old agent")}); err != nil {
				t.Fatal(err)
			}
			needEvent(t, events, svc.Owner, "checking")
			if err := d.ApplyFiles("c2", spec.Files{"app.json": []byte("changed"), "agent.md": []byte("new agent")}); err != nil {
				t.Fatal(err)
			}
			needEvent(t, events, svc.Owner, "ready")
			change := d.Changes[1]
			change.Request = "revised agent request"
			if err := d.SetChange(change, d.Diffs[change.ID]); err != nil {
				t.Fatal(err)
			}
			needEvent(t, events, svc.Owner, "ready")
			one, err := s.Diff(context.Background(), scope, bridge.ShapeDiffParams{Change: "c2"})
			if err != nil || len(one.Files) != 1 || one.Files[0].Before != "old agent" || one.Files[0].After != "new agent" {
				t.Fatalf("change diff: %+v, %v", one, err)
			}
			allDiff, err := s.Diff(context.Background(), scope, bridge.ShapeDiffParams{})
			if err != nil || len(allDiff.Files) != 2 {
				t.Fatalf("whole diff: %+v, %v", allDiff, err)
			}
			restarted := New(cfg, all)
			restored, err := restarted.draftFor(scope)
			if err != nil || !reflect.DeepEqual(restored.Diffs, d.Diffs) || restored.Changes[1].Request != change.Request {
				t.Fatalf("restart did not preserve request and diffs: %v", err)
			}
			result, err := restarted.CancelChange(context.Background(), scope, bridge.ShapeChangeCancelParams{Change: tt.cancel})
			if err != nil || len(result.Changes) != 1 || result.State != "ready" {
				t.Fatalf("cancel: %+v, %v", result, err)
			}
			needEvent(t, events, svc.Owner, "ready")
			files, err := restored.Files()
			if err != nil || !reflect.DeepEqual(files, tt.want) {
				t.Fatalf("cancel files: %v, %v; want %v", files, err, tt.want)
			}
			disk, err := spec.ReadDir(filepath.Join(restored.dir, "files"))
			if err != nil || !reflect.DeepEqual(disk, tt.want) {
				t.Fatalf("disk not recomputed: %v", err)
			}
			_, err = restarted.CancelChange(context.Background(), scope, bridge.ShapeChangeCancelParams{Change: "missing"})
			needCode(t, err, bridge.CodeNotFound)
			result.Changes[0].Request = "caller mutation"
			if restored.Changes[0].Request == "caller mutation" {
				t.Fatal("response aliases stored draft")
			}
		})
	}
}

func TestCancelEarlierEditInSameFile(t *testing.T) {
	s, _, _, scope := setupDraft(t)
	d := openDraft(t, s, scope)
	base := "value: old\n" + "unchanged middle\n" + "agent: old\n"
	first := "value: new\n" + "unchanged middle\n" + "agent: old\n"
	second := "value: new\n" + "unchanged middle\n" + "agent: new\n"
	if err := d.Rebase(1, spec.Files{"agent.md": []byte(base)}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ id, text string }{{"c1", first}, {"c2", second}} {
		if err := d.AddChange(bridge.Change{ID: tt.id, Request: tt.id, State: "checking"}, nil); err != nil {
			t.Fatal(err)
		}
		if err := d.ApplyFiles(tt.id, spec.Files{"agent.md": []byte(tt.text)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.removeChange("c1"); err != nil {
		t.Fatal(err)
	}
	files, err := d.Files()
	if err != nil || string(files["agent.md"]) != "value: old\nunchanged middle\nagent: new\n" {
		t.Fatalf("cancel resurrected earlier edit: %q, %v", files["agent.md"], err)
	}
}

func TestRebaseAndMetadataIsCommitPoint(t *testing.T) {
	s, cfg, all, scope := setupDraft(t)
	d := openDraft(t, s, scope)
	if err := d.AddChange(bridge.Change{ID: "c1", Request: "add screen", State: "checking"}, nil); err != nil {
		t.Fatal(err)
	}
	files, _ := d.Files()
	files["screens/new.json"] = []byte("{}")
	delete(files, "agent.md")
	if err := d.ApplyFiles("c1", files); err != nil {
		t.Fatal(err)
	}
	diff := d.Diffs["c1"]
	if len(diff) != 2 || diff[0].Op != "delete" || diff[1].Op != "add" {
		t.Fatalf("diff operations: %+v", diff)
	}
	if err := os.WriteFile(filepath.Join(d.dir, "files", "app.json"), []byte("interrupted write"), 0600); err != nil {
		t.Fatal(err)
	}
	restarted := New(cfg, all)
	restored, err := restarted.draftFor(scope)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := spec.ReadDir(filepath.Join(d.dir, "files"))
	if err != nil || !reflect.DeepEqual(disk, files) {
		t.Fatalf("restart did not repair files: %v", err)
	}
	if err := restored.Rebase(2, spec.Files{"app.json": []byte("new base")}); err != nil {
		t.Fatal(err)
	}
	rebuilt, _ := restored.Files()
	if restored.Base != 2 || restored.State != "checking" || restored.Changes[0].Request != "add screen" || len(restored.Diffs["c1"]) != 0 || string(rebuilt["app.json"]) != "new base" {
		t.Fatalf("rebase: %+v, %v", restored, rebuilt)
	}
	var saved map[string]json.RawMessage
	data, err := os.ReadFile(filepath.Join(d.dir, "draft.json"))
	if err != nil || json.Unmarshal(data, &saved) != nil || saved["diffs"] == nil {
		t.Fatalf("missing durable diffs: %v", err)
	}
}

func TestChangeErrorsLeaveDraftIntact(t *testing.T) {
	s, _, _, scope := setupDraft(t)
	d := openDraft(t, s, scope)
	original := d.snapshot()
	for _, tt := range []struct {
		name string
		run  func() error
		code bridge.ErrorCode
	}{
		{"missing id", func() error { return d.AddChange(bridge.Change{}, nil) }, bridge.CodeInvalid},
		{"missing replacement", func() error { return d.SetChange(bridge.Change{ID: "absent"}, nil) }, bridge.CodeNotFound},
		{"missing files change", func() error { return d.ApplyFiles("absent", spec.Files{}) }, bridge.CodeNotFound},
		{"unsafe diff", func() error {
			return d.AddChange(bridge.Change{ID: "bad", State: "passed"}, []bridge.FileDiff{{Path: "../records.db", Op: "add"}})
		}, bridge.CodeInvalid},
		{"unknown operation", func() error {
			return d.AddChange(bridge.Change{ID: "bad", State: "passed"}, []bridge.FileDiff{{Path: "agent.md", Op: "unknown"}})
		}, bridge.CodeInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			needCode(t, tt.run(), tt.code)
			if !reflect.DeepEqual(original, d.snapshot()) {
				t.Fatal("failed mutation changed draft")
			}
		})
	}
}

func TestChangeStateSelectsOnlyPassedFiles(t *testing.T) {
	for _, tt := range []struct {
		state, draftState string
		applied           bool
	}{
		{"checking", "checking", false}, {"failed", "open", false},
		{"question", "question", false}, {"passed", "ready", true},
	} {
		t.Run(tt.state, func(t *testing.T) {
			s, _, _, scope := setupDraft(t)
			d := openDraft(t, s, scope)
			before, err := d.Files()
			if err != nil {
				t.Fatal(err)
			}
			diffs := []bridge.FileDiff{{Path: "agent.md", Op: "modify", Before: string(before["agent.md"]), After: "changed"}}
			change := bridge.Change{ID: "c1", Request: "edit agent", State: tt.state}
			if err := d.AddChange(change, diffs); err != nil {
				t.Fatal(err)
			}
			files, err := d.Files()
			if err != nil || (string(files["agent.md"]) == "changed") != tt.applied || d.State != tt.draftState {
				t.Fatalf("state=%s files=%s err=%v", d.State, files["agent.md"], err)
			}
			needCode(t, d.AddChange(change, diffs), bridge.CodeConflict)
		})
	}
}
