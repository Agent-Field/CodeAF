package store

import (
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMemoryV2OnlyProofRehearsesAndContradictionIsNegative(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	r, err := s.Write(WriteRequest{Owner: OwnerUser, Text: "Mock routing uses amber queues"})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"surfaced", "retrieved"} {
		if err = s.RecordMemoryEvent(r.Memory.ID, kind, "demo"); err != nil {
			t.Fatal(err)
		}
	}
	m, _, _ := s.MemoryRecord(r.Memory.ID)
	if m.Activation != 1 || m.Rehearsals != 0 {
		t.Fatalf("neutral events reinforced: mass=%f count=%d", m.Activation, m.Rehearsals)
	}
	if err = s.RecordMemoryEvent(r.Memory.ID, "confirmed", "demo"); err != nil {
		t.Fatal(err)
	}
	before, _, _ := s.MemoryRecord(r.Memory.ID)
	if err = s.RecordMemoryEvent(r.Memory.ID, "contradicted", "demo"); err != nil {
		t.Fatal(err)
	}
	after, _, _ := s.MemoryRecord(r.Memory.ID)
	if after.Activation >= before.Activation {
		t.Fatal("contradiction did not lower activation")
	}
	at := time.Now().Add(48 * time.Hour)
	if err = s.RefreshMemoryActivationDue(at); err != nil {
		t.Fatal(err)
	}
	m, _, _ = s.MemoryRecord(r.Memory.ID)
	if math.Abs(m.Activation-1/math.Sqrt(48)) > .001 {
		t.Fatalf("exact refresh reinforced neutral events or ignored negative: %f", m.Activation)
	}
}
func TestMemoryV2DerivedLabelsAndWriteLintPreserveSource(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	text := "Current status of the project: release pipeline active, PR 700 open and review pending."
	legacy, err := s.AddMemory(Memory{Owner: OwnerUser, Type: MemoryPreference, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	m, _, _ := s.MemoryRecord(legacy.ID)
	if m.Type != MemoryPreference || m.SemanticType != MemoryProjectState {
		t.Fatalf("source changed or derived label missing: declared=%s derived=%s", m.Type, m.SemanticType)
	}
	fresh, err := s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference, Text: text + " QA is pending."})
	if err != nil || fresh.Memory.Type != MemoryProjectState {
		t.Fatalf("inflow lint failed %v", err)
	}
	if err = s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	m, _, _ = s.MemoryRecord(legacy.ID)
	if m.Type != MemoryPreference || m.SemanticType != MemoryProjectState {
		t.Fatal("rebuild changed source declaration")
	}
}
func TestMemoryV2StandingInstructionsSurvivePastTenseConditions(t *testing.T) {
	for _, text := range []string{"Always require CI passed and tests verified before marking a change ready.", "No PR is marked ready until CI passed and review verified the release.", "Changes must have tests passed and review verified before being merged."} {
		kind, _ := DerivedMemoryType(MemoryPreference, text)
		if kind != MemoryPreference {
			t.Fatalf("standing instruction mislabeled: %q", text)
		}
	}
}
func TestMemoryV2TechnicalTokensAndCueStops(t *testing.T) {
	cues := memoryContextCues(MemoryContext{Keywords: strings.Fields("what does my memory say about CI")})
	if !reflect.DeepEqual(cues, []string{"ci"}) {
		t.Fatalf("question noise kept: %v", cues)
	}
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	r, err := s.Write(WriteRequest{Owner: OwnerUser, Text: "UI checks use CI on XY"})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"UI", "CI", "XY"} {
		rows, err := s.SearchMemoriesFor([]string{OwnerUser}, query, 5)
		if err != nil || len(rows) != 1 || rows[0].ID != r.Memory.ID {
			t.Fatalf("short technical token %s missing", query)
		}
	}
}
func TestMemoryV2SpecificityUsesOrderedMessageAndEntities(t *testing.T) {
	ctx := memorySpecificityContext(MemoryContext{Keywords: strings.Fields("amber queue /api/v4.1")})
	forward := memorySpecificityFor(Memory{Text: "amber queue /api/v4.1"}, ctx)
	reverse := memorySpecificityFor(Memory{Text: "queue amber /api/v4.2"}, ctx)
	if forward.Phrase <= reverse.Phrase || forward.Entities <= reverse.Entities {
		t.Fatalf("ordered phrase/version/path not distinguished: %+v %+v", forward, reverse)
	}
	goCue := memorySpecificityContext(MemoryContext{Brief: "Where do we go next?"})
	goMatch := memorySpecificityFor(Memory{Text: "We go onward"}, goCue)
	if goMatch.Entities != 0 {
		t.Fatal("English go treated as an entity")
	}
}
func TestMemoryV2DurableFloorAndDwellRemainBounded(t *testing.T) {
	now := time.Now()
	var ms []Memory
	for i := 0; i < 80; i++ {
		kind := MemoryProjectState
		if i < 40 {
			kind = MemoryPreference
		}
		ms = append(ms, Memory{ID: strings.Repeat("x", i+1), Owner: OwnerUser, Type: kind, Text: strings.Repeat("m", 100), Activation: float64(1 + i), ActivationAt: now})
	}
	b := SelectMemoryBlock(ms, MemoryContext{Owners: []string{OwnerUser}, Now: now})
	if b.RunesBySemanticType[MemoryPreference] < int(.4*MemoryBlockRunes) {
		t.Fatalf("durable floor missing: %v", b.RunesBySemanticType)
	}
	ids := []string{}
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	b = SelectMemoryBlock(ms, MemoryContext{Owners: []string{OwnerUser}, DwellIDs: ids, Now: now})
	if b.TotalRunes > MemoryBlockRunes {
		t.Fatalf("unbounded dwell %d", b.TotalRunes)
	}
}
func TestMemoryV2MutationWatermarkIgnoresTelemetry(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	r, err := s.Write(WriteRequest{Owner: OwnerUser, Text: "Mock baseline"})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.MemoryMutationSeq([]string{OwnerUser})
	for _, kind := range []string{"surfaced", "retrieved", "used"} {
		if err = s.RecordMemoryEvent(r.Memory.ID, kind, "demo"); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := s.MemoryMutationSeq([]string{OwnerUser})
	if a != b {
		t.Fatal("telemetry invalidates prompt cache")
	}
	if err = s.ForgetMemory(r.Memory.ID); err != nil {
		t.Fatal(err)
	}
	c, _ := s.MemoryMutationSeq([]string{OwnerUser})
	if c <= b {
		t.Fatal("forget did not invalidate selection")
	}
}

func TestMemoryV2UpgradeReplacesOldTriggersAndNeutralCachedMass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	s := openTestStore(t, path)
	r, err := s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference, Text: "CI always uses amber verification"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecordMemoryEvent(r.Memory.ID, "retrieved", "old-session"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DROP TRIGGER memory_topic_add`, `CREATE TRIGGER memory_topic_add AFTER INSERT ON memory_topics BEGIN SELECT 1; END`, `DROP TRIGGER memory_activation_update`, `CREATE TRIGGER memory_activation_update AFTER UPDATE ON memories BEGIN UPDATE memory_activation SET owner=new.owner,status=new.status WHERE memory_id=new.id; END`, `DELETE FROM memory_schema_versions WHERE version=2`, `DELETE FROM memory_durable_topics`, `DELETE FROM memory_durable_candidates`} {
		if _, err = s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.db.Exec(`UPDATE memory_activation SET mass=40,rehearsals=39 WHERE memory_id=?`, r.Memory.ID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	upgraded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	m, _, _ := upgraded.MemoryRecord(r.Memory.ID)
	if m.Activation > 1 || m.Rehearsals != 0 {
		t.Fatalf("v1 neutral cached mass survived: %f %d", m.Activation, m.Rehearsals)
	}
	var n int
	if err = upgraded.db.QueryRow(`SELECT COUNT(*) FROM memory_durable_topics WHERE memory_id=?`, m.ID).Scan(&n); err != nil || n == 0 {
		t.Fatal("v1 topic trigger persisted")
	}
	if err = upgraded.ForgetMemory(m.ID); err != nil {
		t.Fatal(err)
	}
	if err = upgraded.db.QueryRow(`SELECT COUNT(*) FROM memory_durable_topics WHERE memory_id=?`, m.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("v1 status trigger persisted")
	}
}
func TestMemoryV2ContextualLaneCannotBeEatenByGenericDurableOverflow(t *testing.T) {
	now := time.Now()
	var ms []Memory
	for i := 0; i < 80; i++ {
		kind := MemoryPreference
		mass := 100.0
		if i >= 40 {
			kind = MemoryFact
			mass = 1
		}
		ms = append(ms, Memory{ID: strings.Repeat("x", i+1), Owner: OwnerUser, Type: kind, Text: strings.Repeat("m", 100), Activation: mass, ActivationAt: now})
	}
	b := SelectMemoryBlock(ms, MemoryContext{Owners: []string{OwnerUser}, Now: now})
	if b.RunesBySemanticType[MemoryFact] < int(.5*MemoryBlockRunes) {
		t.Fatalf("contextual share starved: %v", b.RunesBySemanticType)
	}
}
func TestMemoryV2ExactPhraseDoesNotRequireUniqueIndividualWords(t *testing.T) {
	ctx := memorySpecificityContext(MemoryContext{Keywords: strings.Fields("bar chart")})
	score := memorySpecificityFor(Memory{Text: "bar chart", TopicFans: map[string]int{"bar": 10, "chart": 20}}, ctx)
	if score.Phrase != 2 {
		t.Fatal("distinctive joint phrase rejected because individual terms are shared")
	}
	reverse := memorySpecificityFor(Memory{Text: "chart bar", TopicFans: map[string]int{"bar": 10, "chart": 20}}, ctx)
	if reverse.Phrase != 0 {
		t.Fatal("unordered bag of words treated as phrase")
	}
}
func TestMemoryV2NegativeEvidenceSurvivesUntilReconfirmed(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	r, err := s.Write(WriteRequest{Owner: OwnerUser, Text: "Mock contradicted fact"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = s.RecordMemoryEvent(r.Memory.ID, "contradicted", "demo"); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.RecordMemoryEvent(r.Memory.ID, "confirmed", "demo"); err != nil {
		t.Fatal(err)
	}
	m, _, _ := s.MemoryRecord(r.Memory.ID)
	if m.Activation >= 0 {
		t.Fatal("negative debt discarded by incremental updates")
	}
	if err = s.RefreshMemoryActivationDue(time.Now()); err != nil {
		t.Fatal(err)
	}
	m, _, _ = s.MemoryRecord(r.Memory.ID)
	if m.Activation >= 0 {
		t.Fatal("exact history disagrees with incremental negative evidence")
	}
	if err = s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	m, _, _ = s.MemoryRecord(r.Memory.ID)
	if m.Activation >= 0 {
		t.Fatal("rebuild discarded signed negative evidence")
	}
	b := SelectMemoryBlock([]Memory{m}, MemoryContext{Owners: []string{OwnerUser}})
	if b.Decisions[m.ID].Riding {
		t.Fatal("non-positive trace revived through fallback")
	}
}
func TestMemoryV2MutationWatermarkUsesPartialIndex(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT seq FROM events INDEXED BY memory_mutation_seq WHERE ` + memoryMutationKinds + ` ORDER BY seq DESC LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	indexed := false
	for rows.Next() {
		var a, b, c int
		var detail string
		if err = rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		indexed = indexed || strings.Contains(detail, "memory_mutation_seq")
	}
	if !indexed {
		t.Fatal("per-turn mutation check scans non-memory journal history")
	}
}
