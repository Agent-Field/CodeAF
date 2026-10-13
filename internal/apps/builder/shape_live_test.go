//go:build live

package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/sample"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/config"
)

// TestLiveShapeRename uses the existing live tag and profile key ladder. The
// golden CRM already says Amount, so Value is restored as the starting shape.
func TestLiveShapeRename(t *testing.T) {
	if only := os.Getenv("LIVE_ONLY"); only != "" && only != "shape" {
		t.Skip("LIVE_ONLY selects another smoke")
	}
	dir := config.ProfilePath(config.ProfileDir(), "")
	if config.APIKeyAt(dir) == "" {
		t.Skip("no configured provider key")
	}
	t.Setenv(ModelEnv, DefaultModel)
	files, err := spec.ReadDir("../spec/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		files[name] = bytes.ReplaceAll(data, []byte("amount"), []byte("value"))
	}
	var app map[string]any
	if err := json.Unmarshal(files[spec.AppFile], &app); err != nil {
		t.Fatal(err)
	}
	fields := app["records"].(map[string]any)["deal"].(map[string]any)["fields"].(map[string]any)
	fields["value"].(map[string]any)["label"] = "Value"
	files[spec.AppFile], err = json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	all := &svc.Services{}
	all.Model = NewModel(svc.Config{}, all)
	all.Sample = sample.New(svc.Config{}, all)
	all.Check = check.New(svc.Config{}, all)
	shaper := NewShaper(svc.Config{}, all)
	baseline, err := all.Check.Run(ctx, "crm", files, 1)
	if err != nil || !baseline.Pass {
		t.Fatalf("starting golden CRM fails Check: %+v %v", baseline, err)
	}
	for round := 1; round <= prompt.MaxCheckRounds; round++ {
		def, err := spec.Decode(files)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := all.Sample.Generate(ctx, def)
		if err != nil {
			t.Fatal(err)
		}
		in := svc.ShapeInput{App: "crm", Files: files, Request: "rename Value to Amount", Round: round, Sample: rows}
		if round > 1 {
			in.Report = &baseline
		}
		out, err := shaper.Shape(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if out.Question != nil {
			t.Fatalf("rename asked a question: %+v", out.Question)
		}
		for name, data := range out.Files {
			files[name] = data
		}
		validation, err := store.ShapeValidationFiles(files)
		if err != nil {
			t.Fatal(err)
		}
		baseline, err = all.Check.Run(ctx, "crm", validation, round)
		t.Logf("model=%s routed_model=%s round=%d pass=%v rendered=%d cost=$%.5f summary=%s", shaper.builder.model, clientConfig(shaper.builder.model).Model, round, baseline.Pass, len(baseline.Rendered), out.Cost, out.Summary)
		if err != nil {
			t.Fatal(err)
		}
		if !baseline.Pass {
			t.Logf("issues=%+v", baseline.Issues)
			continue
		}
		if err := json.Unmarshal(files[spec.AppFile], &app); err != nil {
			t.Fatal(err)
		}
		fields = app["records"].(map[string]any)["deal"].(map[string]any)["fields"].(map[string]any)
		amount, ok := fields["amount"].(map[string]any)
		if !ok || amount["alias"] != "value" || fields["value"] != nil {
			t.Fatalf("rename did not preserve alias: %+v", fields)
		}
		return
	}
	t.Fatalf("rename did not pass Check: %+v", baseline)
}
