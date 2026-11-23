package main

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A shell-published picture reaches the app as ONE source of origin published,
// with the change the engine observed and the run and call that declared it.
// SHELL_OUTPUTS_WIRE_OUT, when set, receives the exact JSON the wire carries.
func TestAppMediaPublishedShellOutputWireShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chart.png")
	file, _ := os.Create(path)
	png.Encode(file, image.NewRGBA(image.Rect(0, 0, 3, 2)))
	file.Close()
	index := filepath.Join(dir, "artifacts.jsonl")
	data, _ := os.ReadFile(path)
	fact := session.GeneratedMedia(path, data, "")
	fact.Origin, fact.Change, fact.Declared = session.MediaPublished, session.ShellCreated, "chart.png"
	fact.Run, fact.Tool = "run_actual", "call_actual"
	id := session.RecordMedia(index, session.Artifact{Path: path, Session: "sess", Title: "chart.png", Kind: "image", Created: time.Now()}, fact)
	scan, err := session.ListMedia(index, "sess", "", 10, time.Time{})
	if err != nil || len(scan.Rows) != 1 || scan.Rows[0].Fact.ID != id {
		t.Fatalf("%v %+v", err, scan)
	}
	row := scan.Rows[0]
	live := session.ResolveMedia(row, []string{dir})
	wire := appMediaSource(row, live, false)
	raw, _ := json.MarshalIndent(wire, "", "  ")
	for _, want := range []string{`"origin": "published"`, `"change": "created"`, `"declared": "chart.png"`, `"run": "run_actual"`, `"tool": "call_actual"`, `"state": "available"`, `"charge": "unknown"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %s in\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), `"message"`) || strings.Contains(string(raw), "cost_usd") {
		t.Errorf("invented message or cost:\n%s", raw)
	}
	if out := os.Getenv("SHELL_OUTPUTS_WIRE_OUT"); out != "" {
		os.WriteFile(out, append(raw, '\n'), 0o600)
	}
}
