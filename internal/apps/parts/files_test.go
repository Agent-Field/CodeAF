package parts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestPathEscapes(t *testing.T) {
	s, scope, hub := setup(t)
	ctx := context.Background()
	textDoc(t, s, scope, "q4")
	initial := len(hub.events)
	for _, part := range []string{"../escape.md", "a/../escape.md", "/escape.md", "a/./b.md", "a//b.md", "a\\b.md", "doc.json", "nested/doc.json", "a\x00.md"} {
		t.Run(part, func(t *testing.T) {
			_, err := s.Add(ctx, scope, bridge.AddPartParams{Doc: "q4", Part: part, Text: "bad"})
			requireCode(t, err, bridge.CodeInvalid)
			_, err = s.Get(ctx, scope, bridge.GetPartParams{Doc: "q4", Part: part, Version: 1})
			requireCode(t, err, bridge.CodeInvalid)
			_, err = s.Versions(ctx, scope, bridge.ListPartVersionsParams{Doc: "q4", Part: part})
			requireCode(t, err, bridge.CodeInvalid)
		})
	}
	for _, doc := range []string{"../outside", "a/../outside", "/outside", "a\\outside", ""} {
		t.Run("doc/"+doc, func(t *testing.T) {
			_, err := s.GetDoc(ctx, scope, bridge.GetDocParams{Doc: doc})
			requireCode(t, err, bridge.CodeInvalid)
		})
	}
	scope.App = "../outside"
	_, err := s.ListDocs(ctx, scope, bridge.ListDocsParams{})
	requireCode(t, err, bridge.CodeInvalid)
	if len(hub.events) != initial {
		t.Fatalf("failed calls published events: %v", hub.events)
	}
}

func TestSymlinkEscapes(t *testing.T) {
	for _, target := range []string{"part", "part-directory", "doc", "content", "app", "database"} {
		t.Run(target, func(t *testing.T) {
			s, scope, _ := setup(t)
			ctx := context.Background()
			textDoc(t, s, scope, "q4")
			app := filepath.Join(s.cfg.Root, scope.App)
			outside := t.TempDir()
			var link string
			switch target {
			case "part":
				link = filepath.Join(app, "content/q4/section-1.md")
			case "part-directory":
				link = filepath.Join(app, "content/q4/nested")
			case "doc":
				link = filepath.Join(app, "content/q4")
			case "content":
				link = filepath.Join(app, "content")
			case "app":
				link = app
			case "database":
				link = filepath.Join(app, "parts.db")
			}
			if err := os.RemoveAll(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			var err error
			if target == "part-directory" {
				_, err = s.Add(ctx, scope, bridge.AddPartParams{Doc: "q4", Part: "nested/escape.md", Text: "escape"})
			} else {
				_, err = s.Get(ctx, scope, bridge.GetPartParams{Doc: "q4", Part: "section-1.md"})
			}
			requireCode(t, err, bridge.CodeInvalid)
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("outside modified: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestTransactionRollsBackFilesAndVersions(t *testing.T) {
	s, scope, hub := setup(t)
	ctx := context.Background()
	// The second part's invalid body arrives after the first file was renamed.
	_, err := s.CreateDoc(ctx, scope, bridge.CreateDocParams{Doc: "bad", Surface: bridge.SurfaceText, Parts: []bridge.CreateDocPart{{Part: "first.md", Text: "ok"}, {Part: "second.md", Text: string([]byte{0xff})}}})
	requireCode(t, err, bridge.CodeInvalid)
	file := filepath.Join(s.cfg.Root, scope.App, "content/bad/first.md")
	if _, err = os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("rolled-back file remains: %v", err)
	}
	if len(hub.events) != 0 {
		t.Fatalf("rollback published events: %v", hub.events)
	}
	textDoc(t, s, scope, "q4")
	initial := len(hub.events)
	// A filesystem error follows the version insert and must roll it back too.
	blocked := filepath.Join(s.cfg.Root, scope.App, "content/q4/blocked.md")
	if err = os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = s.Add(ctx, scope, bridge.AddPartParams{Doc: "q4", Part: "blocked.md", Text: "ok"})
	if err == nil {
		t.Fatal("expected filesystem failure")
	}
	_, err = s.Versions(ctx, scope, bridge.ListPartVersionsParams{Doc: "q4", Part: "blocked.md"})
	requireCode(t, err, bridge.CodeNotFound)
	if len(hub.events) != initial {
		t.Fatal("failed file write published an event")
	}
	// A fixture failure must undo earlier documents in that fixture import.
	files := spec.Files{
		"a/doc.json": []byte(`{"surface":"text","title":"A","parts":["first.md"]}`),
		"a/first.md": []byte("ok"),
		"z/doc.json": []byte(`{"surface":"text","title":"Z","parts":["missing.md"]}`),
	}
	err = s.Seed(scope.App, scope.Data, files)
	requireCode(t, err, bridge.CodeInvalid)
	_, err = s.GetDoc(ctx, scope, bridge.GetDocParams{Doc: "a"})
	requireCode(t, err, bridge.CodeNotFound)
	if len(hub.events) != initial {
		t.Fatal("failed seed published an event")
	}
}
