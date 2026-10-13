package parts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestGridVersionsIncludeFormulas(t *testing.T) {
	s, scope, _ := setup(t)
	ctx := context.Background()
	files := spec.Files{
		"budget/doc.json":                []byte(`{"surface":"grid","title":"Budget","parts":["sheets/q4.csv"]}`),
		"budget/sheets/q4.csv":           []byte("Name,Total\nHiring,12\n"),
		"budget/sheets/q4.formulas.json": []byte(`{"B2":"=SUM(B3:B5)"}`),
	}
	if err := s.Seed(scope.App, scope.Data, files); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		extra *bridge.PartExtra
		want  string
	}{
		{"replace", &bridge.PartExtra{Formulas: map[string]string{"B2": "=SUM(B3:B6)"}}, "=SUM(B3:B6)"},
		{"preserve", nil, "=SUM(B3:B6)"},
		{"clear", &bridge.PartExtra{}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			current, err := s.Get(ctx, scope, bridge.GetPartParams{Doc: "budget", Part: "sheets/q4.csv"})
			if err != nil {
				t.Fatal(err)
			}
			part, err := s.Write(ctx, scope, bridge.WritePartParams{Doc: "budget", Part: "sheets/q4.csv", Text: "Name,Total\nHiring,14\n", BaseVersion: current.Version, Kind: bridge.PartVersionKindNudge, Extra: tt.extra})
			if err != nil || part.Extra.Formulas["B2"] != tt.want {
				t.Fatalf("part=%+v err=%v", part, err)
			}
			part, err = s.Get(ctx, scope, bridge.GetPartParams{Doc: "budget", Part: "sheets/q4.csv"})
			if err != nil || part.Extra.Formulas["B2"] != tt.want {
				t.Fatalf("current=%+v err=%v", part, err)
			}
		})
	}
	part, err := s.Restore(ctx, scope, bridge.RestorePartParams{Doc: "budget", Part: "sheets/q4.csv", Version: 1})
	if err != nil || part.Version != 5 || part.Text != string(files["budget/sheets/q4.csv"]) || part.Extra.Formulas["B2"] != "=SUM(B3:B5)" {
		t.Fatalf("restore=%+v err=%v", part, err)
	}
	body, err := os.ReadFile(filepath.Join(s.cfg.Root, scope.App, "content/budget/sheets/q4.formulas.json"))
	if err != nil || string(body) != `{"B2":"=SUM(B3:B5)"}` {
		t.Fatalf("formulas=%q err=%v", body, err)
	}
}

func TestInvalidGridFormulasSeed(t *testing.T) {
	for _, formulas := range []string{`null`, `[]`, `{"B2":12}`, `{"B0":"=1"}`, `{"B2":"1"}`} {
		t.Run(formulas, func(t *testing.T) {
			s, scope, hub := setup(t)
			files := spec.Files{
				"budget/doc.json":                []byte(`{"surface":"grid","title":"Budget","parts":["sheets/q4.csv"]}`),
				"budget/sheets/q4.csv":           []byte("a,b"),
				"budget/sheets/q4.formulas.json": []byte(formulas),
			}
			requireCode(t, s.Seed(scope.App, scope.Data, files), bridge.CodeInvalid)
			if len(hub.events) != 0 {
				t.Fatal("invalid seed published events")
			}
		})
	}
}
