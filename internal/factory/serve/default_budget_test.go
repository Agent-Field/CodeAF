package serve_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// THE DEFAULT APPLIES ONLY TO UNBUDGETED JOBS. A new built-in default must not
// silently raise an existing job's cap or replace a repository's explicit choice.
func TestFactoryStartDefaultsToFiveWithoutChangingSavedBudgets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cap    float64
		config string
		want   float64
	}{
		{name: "new job", want: 5},
		{name: "saved three-dollar job", cap: 3, want: 3},
		{name: "saved larger job", cap: 10, want: 10},
		{name: "explicit repository default", config: "default_budget: 7\n", want: 7},
		{name: "saved job beats repository default", cap: 3, config: "default_budget: 7\n", want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newV2Jobs(t)
			f.h.Close()
			f.cfg.Pipeline = func(context.Context, string, string) (pipeline.File, error) {
				return pipeline.Parse(tc.config)
			}
			f.h = serve.New(f.seam, f.cfg)
			it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Cap: tc.cap})
			f.post(t, it, "run", "", 200)
			j := f.read(t, it)
			if j.State != "queued" || j.Budget != tc.want {
				t.Fatalf("start state=%s budget=%v, want queued budget=%v", j.State, j.Budget, tc.want)
			}
			saved, err := f.st.Get(it.ID)
			if err != nil || saved.Cap != tc.want {
				t.Fatalf("saved budget=%v err=%v, want %v", saved.Cap, err, tc.want)
			}
		})
	}
}
