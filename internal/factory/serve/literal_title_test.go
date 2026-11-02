package serve_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func TestSeparateJobTitleIsLiteral(t *testing.T) {
	for _, source := range []string{"app", "hand-off", "", "tui"} {
		t.Run(source, func(t *testing.T) {
			f := newV2Jobs(t)
			f.h.Close()
			f.seam = factory.LocalSeam(f.st, time.Now())
			f.h = serve.New(f.seam, f.cfg)
			body := api.NewJob{Repo: "acme/api", Title: "  Fix auto-save $5  ", Text: "  security, two review rounds, cheaper; ship it; plan first\n", Source: source}
			if source == "hand-off" {
				body.FromRef = &factory.SourceRef{Kind: "agent", ID: "a1", Title: "Source"}
			}
			raw, _ := json.Marshal(body)
			w := verbRequest(f.h, "POST", "/v1/jobs", string(raw))
			checkJSON(t, w, 201)
			var made struct {
				ID int `json:"id"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &made); err != nil {
				t.Fatal(err)
			}
			j := f.read(t, factory.Item{ID: made.ID})
			it := api.ToItem(j)
			if j.Title != "Fix auto-save $5" {
				t.Errorf("title = %q; want Fix auto-save $5", j.Title)
			}
			if j.Ask == nil || j.Ask.Text != body.Text {
				t.Errorf("ask = %+v; want %q", j.Ask, body.Text)
			}
			saved, err := f.st.Get(made.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Body != body.Text {
				t.Errorf("body = %q; want %q", saved.Body, body.Text)
			}
			if it.Cap != 0 {
				t.Errorf("cap = %v; want 0", it.Cap)
			}
			want := factory.DefaultRecipe().For(factory.KindIssue)
			if len(saved.Stages) != len(want) {
				t.Errorf("stages = %+v; want %+v", saved.Stages, want)
			}
			for i, step := range saved.Stages {
				if i < len(want) && (step.Name != want[i].Name || step.On != want[i].On || step.Max != want[i].Max || step.Effort != want[i].Effort) {
					t.Errorf("stage %d = %+v; want %+v", i, step, want[i])
				}
			}
		})
	}
}

func TestSeparateJobTitleIsLiteralWithoutDetails(t *testing.T) {
	for _, source := range []string{"app", "hand-off", "", "tui"} {
		t.Run(source, func(t *testing.T) {
			f := newV2Jobs(t)
			f.h.Close()
			f.seam = factory.LocalSeam(f.st, time.Now())
			f.h = serve.New(f.seam, f.cfg)
			body := api.NewJob{Repo: "acme/api", Title: "Fix auto-save $5", Source: source}
			if source == "hand-off" {
				body.FromRef = &factory.SourceRef{Kind: "agent", ID: "a1", Title: "Source"}
			}
			raw, _ := json.Marshal(body)
			w := verbRequest(f.h, "POST", "/v1/jobs", string(raw))
			checkJSON(t, w, 201)
			var made struct {
				ID int `json:"id"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &made); err != nil {
				t.Fatal(err)
			}
			got := f.read(t, factory.Item{ID: made.ID})
			if source == "app" || source == "hand-off" {
				if got.Ask == nil || got.Ask.Text != "" {
					t.Errorf("empty details became ask %+v", got.Ask)
				}
				saved, err := f.st.Get(made.ID)
				if err != nil {
					t.Fatal(err)
				}
				if saved.Body != "" {
					t.Errorf("empty details became body %q", saved.Body)
				}
			}
			title, budget := "Fix auto-save $5", float64(0)
			if source == "" || source == "tui" {
				title, budget = "Fix -save", 5
			}
			if got.Title != title || got.Budget != budget {
				t.Fatalf("title = %q budget %v; want %q budget %v", got.Title, got.Budget, title, budget)
			}
		})
	}
}
