package store

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

func TestCompetenceMapClassifiesProfileBucketsAndExportsFrontier(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	var records []profile.Record
	records = append(records, competenceProfileRecords("atomic", now, 0,
		[]float64{.1, .1, .1, .1, .1, .1, .1, .1})...)
	records = append(records, competenceProfileRecords("direct", now, 4, nil)...)
	records = append(records, competenceProfileRecords("oversized", now, 7,
		[]float64{.4, .4, .4, .4, .4, .4, .4, .4})...)
	records = append(records, competenceProfileRecords("synthesis", now.Add(-100*24*time.Hour), 0,
		[]float64{.1, .1, .1, .1, .1, .1, .1, .1})...)
	measured := &profile.Profile{Records: records}

	competence, err := graph.CompetenceMap(CompetenceOptions{Profile: measured, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	byScope := competenceByScope(competence)
	for scope, want := range map[string]CompetenceClass{
		"profile:atomic":    CompetenceStrong,
		"profile:direct":    CompetenceFrontier,
		"profile:oversized": CompetenceWeak,
		"profile:synthesis": CompetenceStale,
	} {
		if got := byScope[scope].Class; got != want {
			t.Errorf("%s class = %q, want %q (%+v)", scope, got, want, byScope[scope])
		}
	}
	if got := byScope["profile:direct"].FailureRate; got != .5 {
		t.Fatalf("direct failure rate = %v, want .5", got)
	}

	frontier := competence.Frontier()
	if len(frontier) != 1 || frontier[0].Scope != "profile:direct" {
		t.Fatalf("Frontier() = %+v, want direct only", frontier)
	}
	frontier[0].Scope = "changed"
	if competenceByScope(competence)["profile:direct"].Scope != "profile:direct" {
		t.Fatal("Frontier() aliased the source map")
	}

	thresholds := DefaultCompetenceThresholds()
	thresholds.EstablishedSamples = 10
	reclassified, err := graph.CompetenceMap(CompetenceOptions{
		Profile: measured, Now: now, Thresholds: &thresholds,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := competenceByScope(reclassified)["profile:atomic"].Class; got != CompetenceFrontier {
		t.Fatalf("atomic under ten-sample threshold = %q, want frontier", got)
	}
}

func TestCompetenceMapImprovingSurpriseKeepsHighFailureOnFrontier(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	now := time.Now().UTC()
	measured := &profile.Profile{Records: competenceProfileRecords(
		profile.BucketReflex, now, 7, []float64{1, 1, 1, 1, .4, .4, .4, .4},
	)}
	competence, err := graph.CompetenceMap(CompetenceOptions{Profile: measured, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	reflex := competenceByScope(competence)["profile:reflex"]
	if reflex.SurpriseTrend != SurpriseImproving || reflex.Class != CompetenceFrontier {
		t.Fatalf("improving reflex = %+v, want improving frontier", reflex)
	}
}

func TestCompetenceMapAggregatesTerritoryEvidenceAndInstalledSkills(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	specs := []NodeSpec{{ID: "go-job", Brief: "maintain the Go parser", Stage: 2}}
	for index := 1; index < profile.MinSamples; index++ {
		specs = append(specs, NodeSpec{
			ID: fmt.Sprintf("go-leaf-%d", index), Parent: "go-job",
			Brief: "repair one parser path", Stage: 1,
		})
	}
	if err := graph.Splice(RootID, Subtree{Nodes: specs}, Provenance{
		Origin: OriginUser, Intent: "maintain the Go parser",
	}); err != nil {
		t.Fatal(err)
	}
	for index := 1; index < profile.MinSamples; index++ {
		id := fmt.Sprintf("go-leaf-%d", index)
		if err := graph.Complete(mustClaim(t, graph, id, "worker"), "verified"); err != nil {
			t.Fatal(err)
		}
		if err := graph.RecordSurprise(NodeSurprise{NodeID: id, ActualTokens: 100, ExpectedTokens: 100, Surprise: .1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := graph.Complete(mustClaim(t, graph, "go-job", "worker"), "parser maintained"); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordSurprise(NodeSurprise{NodeID: "go-job", ActualTokens: 100, ExpectedTokens: 100, Surprise: .1}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact("go-leaf-1", "tool:go", FactLesson, "Run focused parser tests first."); err != nil {
		t.Fatal(err)
	}
	skill, err := graph.RecordSkillCandidate("go-leaf-1", "tool:go", "Repair Go parser paths with focused tests.", "/tmp/go-parser-skill")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ActivateSkill(skill.Seq, "/installed/go-parser-skill"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fold("go-job", "Go parser work stayed reliable.", nil); err != nil {
		t.Fatal(err)
	}

	competence, err := graph.CompetenceMap()
	if err != nil {
		t.Fatal(err)
	}
	goScope := competenceByScope(competence)["tool:go"]
	if goScope.Kind != CompetenceTerritory || goScope.Samples != profile.MinSamples ||
		goScope.SuccessRate != 1 || goScope.SurpriseTrend != SurpriseFlat ||
		goScope.Class != CompetenceStrong {
		t.Fatalf("tool:go competence = %+v", goScope)
	}
	if !reflect.DeepEqual(goScope.InstalledSkills, []string{"Repair Go parser paths with focused tests."}) {
		t.Fatalf("installed skills = %+v", goScope.InstalledSkills)
	}
	if goScope.LastTouched.IsZero() {
		t.Fatal("territory last-touched time is zero")
	}
}

func TestCompetenceThresholdsRejectIncoherentPolicy(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	thresholds := DefaultCompetenceThresholds()
	thresholds.FrontierFailureMin = .9
	thresholds.FrontierFailureMax = .1
	if _, err := graph.CompetenceMap(CompetenceOptions{Thresholds: &thresholds}); err == nil {
		t.Fatal("CompetenceMap accepted inverted frontier thresholds")
	}
}

func TestCompetenceSkillScopeTouchingUsesPathHierarchy(t *testing.T) {
	for _, test := range []struct {
		first, second string
		want          bool
	}{
		{"repo:/workspace/app", "file:/workspace/app/main.go", true},
		{"repo:/workspace", "repo:/workspace/app", true},
		{"file:/workspace/app.go", "file:/workspace/app.go", true},
		{"file:/workspace/app.go", "file:/workspace/app.go.bak", false},
		{"tool:go", "tool:go", true},
		{"tool:go", "tool:git", false},
	} {
		if got := scopesTouch(test.first, test.second); got != test.want {
			t.Errorf("scopesTouch(%q, %q) = %t, want %t", test.first, test.second, got, test.want)
		}
	}
}

func competenceProfileRecords(bucket string, at time.Time, failures int, surprises []float64) []profile.Record {
	records := make([]profile.Record, profile.MinSamples)
	for index := range records {
		records[index] = profile.Record{
			Title: fmt.Sprintf("%s-%d", bucket, index), Size: bucket, Time: at,
			Verdict: provider.VerdictVerifiedSuccess,
		}
		if index < failures {
			records[index].Verdict = provider.VerdictSemanticFailure
		}
		if index < len(surprises) {
			value := surprises[index]
			records[index].Surprise = &value
		}
	}
	return records
}

func competenceByScope(competence CompetenceMap) map[string]ScopeCompetence {
	result := make(map[string]ScopeCompetence, len(competence.Scopes))
	for _, scope := range competence.Scopes {
		result[scope.Scope] = scope
	}
	return result
}
