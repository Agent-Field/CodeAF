package builder

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type shapeModel func(context.Context, svc.ModelCall) (svc.ModelReply, error)

func (f shapeModel) Complete(ctx context.Context, in svc.ModelCall) (svc.ModelReply, error) {
	return f(ctx, in)
}

func TestShapeModelDoorAndParser(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		question    bool
	}{
		{"changed files", `{"files":{"app.json":{"records":{"deal":{"fields":{"amount":{"type":"money","alias":"value"}}}}}},"summary":"Amount replaces Value"}`, false},
		{"direct files", `{"screens/pipeline.json":{"screen":"pipeline"}}`, false},
		{"question", `{"question":{"text":"Source was removed. Group by?","options":[{"id":"stage","label":"By stage"},{"id":"drop","label":"Drop this request"}]}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(ModelEnv, "deepseek/deepseek-v4.1-flash")
			calls := 0
			model := shapeModel(func(_ context.Context, call svc.ModelCall) (svc.ModelReply, error) {
				calls++
				if call.Model != DefaultModel || call.App != "crm" {
					t.Fatalf("route = %+v", call)
				}
				text := call.Messages[0].Text
				for _, want := range []string{"rename Value to Amount", "synthetic-company", "last check issue", `"alias"`, "Drop this request", "app.json capabilities", "records.query", "records.get", "records.create", "records.update", "records.delete", "repair missing record tools"} {
					if !strings.Contains(text, want) {
						t.Errorf("prompt missing %q", want)
					}
				}
				return svc.ModelReply{Text: tc.reply, Cost: .001}, nil
			})
			out, err := NewShaper(svc.Config{}, &svc.Services{Model: model}).Shape(context.Background(), svc.ShapeInput{
				App: "crm", Files: spec.Files{"app.json": []byte(`{"id":"crm"}`)}, Request: "rename Value to Amount",
				Report: &bridge.CheckReport{Issues: []spec.Issue{{Message: "last check issue"}}},
				Sample: map[string][]bridge.Record{"company": {{ID: "synthetic-company"}}},
			})
			if err != nil || calls != 1 || out.Cost != .001 || (out.Question != nil) != tc.question {
				t.Fatalf("shape = %+v, %v; calls=%d", out, err, calls)
			}
			if !tc.question && len(out.Files) != 1 {
				t.Fatalf("files = %+v", out.Files)
			}
		})
	}
}

func TestShapeRejectsContentBeforeModel(t *testing.T) {
	for _, path := range []string{"records.db", "content/secret.md", "../app.json", "sample.json"} {
		t.Run(path, func(t *testing.T) {
			model := shapeModel(func(context.Context, svc.ModelCall) (svc.ModelReply, error) {
				t.Fatal("content reached model")
				return svc.ModelReply{}, nil
			})
			_, err := NewShaper(svc.Config{}, &svc.Services{Model: model}).Shape(context.Background(), svc.ShapeInput{Files: spec.Files{path: []byte("secret")}, Request: "edit"})
			if err == nil {
				t.Fatal("content accepted")
			}
		})
	}
}

func TestShapeRepairsMalformedReplyOnce(t *testing.T) {
	calls := 0
	model := shapeModel(func(_ context.Context, call svc.ModelCall) (svc.ModelReply, error) {
		calls++
		if calls == 1 {
			return svc.ModelReply{Text: `{"question":{"text":"Why?","options":[{"id":"drop","label":"Drop this request"}]}}`, Cost: .002}, nil
		}
		if len(call.Messages) != 3 || !strings.Contains(call.Messages[2].Text, "concrete alternatives") {
			t.Fatalf("repair = %+v", call.Messages)
		}
		return svc.ModelReply{Text: `{"agent.md":"---\nname: CRM agent\ntools: [records.query, records.get, records.create, records.update, records.delete]\n---\nNew instructions"}`, Cost: .003}, nil
	})
	out, err := NewShaper(svc.Config{}, &svc.Services{Model: model}).Shape(context.Background(), svc.ShapeInput{Files: spec.Files{"agent.md": []byte("old")}, Request: "edit"})
	if err != nil || calls != 2 || out.Cost != .005 {
		t.Fatalf("shape = %+v %v calls=%d", out, err, calls)
	}
}

func TestShapeReplyRefusals(t *testing.T) {
	for _, reply := range []string{
		`{"files":{"records.db":"bad"}}`,
		`{"question":{"text":"Q","options":[{"id":"a","label":"A"},{"id":"a","label":"B"},{"id":"drop","label":"Drop this request"}]}}`,
		`{"files":{"agent.md":"new"},"question":{"text":"Q","options":[{"id":"a","label":"A"},{"id":"drop","label":"Drop this request"}]}}`,
	} {
		if _, err := parseShape(reply); err == nil {
			t.Errorf("accepted %s", reply)
		}
	}
}

func TestShapeUsesOnlySuppliedFiles(t *testing.T) {
	model := shapeModel(func(_ context.Context, call svc.ModelCall) (svc.ModelReply, error) {
		return svc.ModelReply{}, errors.New("offline")
	})
	_, err := NewShaper(svc.Config{}, &svc.Services{Model: model}).Shape(context.Background(), svc.ShapeInput{Files: spec.Files{"app.json": json.RawMessage(`{}`)}, Request: "edit"})
	if err == nil || err.Error() != "offline" {
		t.Fatalf("err=%v", err)
	}
}
