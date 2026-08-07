package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestWriteCompetenceUsesCalmGroupedRows(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	competence := store.CompetenceMap{Scopes: []store.ScopeCompetence{
		{Scope: "profile:atomic", Kind: store.CompetenceProfile, Class: store.CompetenceStrong,
			Samples: 12, FailureRate: .08, SurpriseTrend: store.SurpriseFlat},
		{Scope: "tool:go", Kind: store.CompetenceTerritory, Class: store.CompetenceFrontier,
			Samples: 8, FailureRate: .5, SurpriseTrend: store.SurpriseImproving,
			InstalledSkills: []string{"focused parser repair"}},
		{Scope: "domain:publishing", Kind: store.CompetenceTerritory, Class: store.CompetenceWeak,
			Samples: 10, FailureRate: .8, SurpriseTrend: store.SurpriseFlat},
	}}
	var output bytes.Buffer
	if err := writeCompetence(&output, competence, now); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		"strong\n  atomic work — 12 runs · 8% failed · surprise flat",
		"frontier\n  tool:go — 8 runs · 50% failed · surprise improving · 1 installed skill",
		"weak\n  domain:publishing — 10 runs · 80% failed · surprise flat",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("competence output = %q, want %q", got, want)
		}
	}
	for _, noisy := range []string{"SAMPLES", "SUCCESS RATE", "|"} {
		if strings.Contains(got, noisy) {
			t.Errorf("competence output contains dashboard noise %q: %q", noisy, got)
		}
	}
}
