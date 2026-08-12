package rail

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The fakes. Every test in this package drives the component through
// [ScopeSource], which is the same door the integration lane will use — so a
// test that passes here is evidence about the shipped path and not about a
// test-only shortcut.
//
// The scene is 5.15's wireframe, with the IDs deliberately shaped like the node
// IDs the store actually mints: 5.14 forbids ever drawing one, and a fake whose
// IDs looked like names could not catch a leak.

const (
	idWisp  = "01JB7QK4M0ZS3T8N4V2C9H6XE1"
	idClean = "01JB7QK4M0ZS3T8N4V2C9H6XE2"
	idPerf  = "01JB7QK4M0ZS3T8N4V2C9H6XE3"
	idH2    = "01JB7QK4M0ZS3T8N4V2C9H6XF7"
)

type fakeSource struct {
	scopes map[string]Scope
	calls  int
}

func (f *fakeSource) Scope(id string) (Scope, bool) {
	f.calls++
	s, ok := f.scopes[id]
	return s, ok
}

func (f *fakeSource) set(s Scope) { f.scopes[s.ID] = s }

func (f *fakeSource) drop(id string) { delete(f.scopes, id) }

// scene builds the home rail and the wisp-parity task scope of 5.15.
func scene() *fakeSource {
	home := Scope{
		ID:    HomeScopeID,
		Title: "aforge",
		Rows: []Row{
			{
				ID:       "home",
				Kind:     RowSurface,
				Name:     "aforge",
				Composer: ComposerChat,
				Life:     LifeSettled,
			},
			{
				ID:       idWisp,
				Kind:     RowTask,
				Name:     "wisp-parity",
				Status:   "reworking NavCtx after the worker died",
				Life:     LifeWorking,
				Composer: ComposerChat,
				Meta: Telemetry{
					Model:         "K3",
					Cost:          8.65,
					HasCost:       true,
					ContextUsed:   170_000,
					ContextWindow: 1_000_000,
					Elapsed:       41 * time.Minute,
					HasElapsed:    true,
					// The census of the five parts below, and the only thing
					// line 3 says about the job's shape (§14).
					Counts: StateCounts{Running: 2, Queued: 2, Done: 1},
				},
				Workers: []Row{
					{ID: idH2, Name: "H2", Life: LifeWorking, Meta: Telemetry{
						Model: "K3", Cost: 0.37, HasCost: true,
						Elapsed: 28 * time.Minute, HasElapsed: true,
					}},
					{ID: "w2", Name: "NavCtx2", Life: LifeWorking, Meta: Telemetry{
						Model: "K3", Cost: 0.12, HasCost: true,
						Elapsed: 5 * time.Minute, HasElapsed: true,
					}},
				},
			},
			{
				ID:        idClean,
				Kind:      RowTask,
				Name:      "data-clean",
				Status:    "needs a key for the vendor API",
				Life:      LifeWorking,
				Questions: 1,
				Composer:  ComposerChat,
				Meta: Telemetry{
					Model: "Q3", Cost: 0.44, HasCost: true,
					Elapsed: 6 * time.Minute, HasElapsed: true,
				},
			},
			{
				ID:       idPerf,
				Kind:     RowTask,
				Name:     "perf-audit",
				Status:   "wrote the report and stopped",
				Life:     LifeSettled,
				Composer: ComposerChat,
				Meta: Telemetry{
					Model: "K3", Cost: 2.10, HasCost: true,
					Elapsed: 52 * time.Minute, HasElapsed: true,
				},
				Artifact: Ref{Path: "docs/perf/report-2026-08.md"},
			},
		},
	}

	task := Scope{
		ID:    idWisp,
		Title: "wisp-parity",
		Seed:  idWisp,
		Rows: []Row{
			{
				ID:       idWisp,
				Kind:     RowSurface,
				Name:     "orchestrator",
				Status:   "dropping H2 — 3 steps remain",
				Composer: ComposerChat,
				Life:     LifeWorking,
				Meta: Telemetry{
					Model: "K3", Cost: 8.65, HasCost: true,
					ContextUsed: 170_000, ContextWindow: 1_000_000,
				},
			},
			{ID: "s1", Kind: RowStep, Name: "XhrSyn", Life: LifeSettled,
				Meta: Telemetry{Elapsed: 12 * time.Minute, HasElapsed: true}},
			{ID: idH2, Kind: RowStep, Name: "H2", Life: LifeWorking, Composer: ComposerSteer,
				Meta: Telemetry{Elapsed: 28 * time.Minute, HasElapsed: true}},
			{ID: "s3", Kind: RowStep, Name: "T3Infra", Life: LifeWorking, Composer: ComposerSteer,
				Meta: Telemetry{Elapsed: 21 * time.Minute, HasElapsed: true}},
			{ID: "s4", Kind: RowStep, Name: "KeyCutter", Life: LifeQueued,
				WaitsOn: []string{"H2"}},
			{ID: "w9", Kind: RowWorker, Depth: 1, Name: "NavCtx2", Life: LifeWorking,
				Composer: ComposerSteer,
				Status:   "read → edit",
				Meta: Telemetry{
					Model: "K3", Cost: 0.37, HasCost: true,
					ContextUsed: 42_000, ContextWindow: 1_000_000,
					Elapsed: 5 * time.Minute, HasElapsed: true,
				},
			},
		},
	}

	return &fakeSource{scopes: map[string]Scope{
		HomeScopeID: home,
		idWisp:      task,
	}}
}

// crowd builds a home scope with n task rows, cycling through the lifecycles so
// the overflow policies have something to choose between.
func crowd(n int, lives ...Lifecycle) *fakeSource {
	if len(lives) == 0 {
		lives = []Lifecycle{LifeQueued}
	}
	rows := []Row{{ID: "home", Kind: RowSurface, Name: "aforge", Composer: ComposerChat}}
	for i := 0; i < n; i++ {
		rows = append(rows, Row{
			ID:       "task-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Kind:     RowTask,
			Name:     "task-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Life:     lives[i%len(lives)],
			Composer: ComposerChat,
		})
	}
	return &fakeSource{scopes: map[string]Scope{
		HomeScopeID: {ID: HomeScopeID, Title: "aforge", Rows: rows},
	}}
}

// plainView is the colourless renderer the anatomy tests read, so an assertion
// is about layout and never about an escape sequence.
func plainView() *View { return NewView(tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)) }

func colourView(f tokens.Focus) *View {
	return NewView(tokens.NewStyler(tokens.TrueColor, f))
}
