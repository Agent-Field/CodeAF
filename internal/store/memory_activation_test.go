package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActivationWritesNeverRefuseBudget(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	for i := 0; i < 80; i++ {
		_, err := s.Write(WriteRequest{Owner: OwnerUser, Text: fmt.Sprintf("fact %d %s", i, strings.Repeat("w", 100)), Origin: OriginNoticed})
		if err != nil {
			t.Fatal(err)
		}
	}
	b, err := s.AssembleMemoryBlock(MemoryContext{Owners: []string{OwnerUser}})
	if err != nil {
		t.Fatal(err)
	}
	if b.Omitted == 0 {
		t.Fatal("expected transparent budget omissions")
	}
}
func TestActivationContextRescuesOldDecisionAndPinsBypassBudget(t *testing.T) {
	now := time.Now()
	old := Memory{ID: "old", Owner: OwnerProject("demo"), Text: "Delivery routing uses amber queues", UpdatedAt: now.Add(-24 * 30 * time.Hour)}
	ms := []Memory{old}
	for i := 0; i < 100; i++ {
		ms = append(ms, Memory{ID: fmt.Sprint(i), Owner: OwnerUser, Origin: OriginPerson, Text: strings.Repeat("p", 100), UpdatedAt: now})
	}
	b := SelectMemoryBlock(ms, MemoryContext{Owners: []string{OwnerUser, old.Owner}, Project: "demo", Brief: "delivery amber queues", Now: now})
	for _, m := range ms[1:] {
		if !b.Decisions[m.ID].Riding {
			t.Fatal("pin lost")
		}
	}
	if b.Decisions[old.ID].Reason != "prompt budget" {
		t.Fatalf("old relevance not activated: %+v", b.Decisions[old.ID])
	}
	b = SelectMemoryBlock([]Memory{old}, MemoryContext{Owners: []string{old.Owner}, Project: "demo", Brief: "delivery amber queues", Now: now})
	if !b.Decisions[old.ID].Riding {
		t.Fatal("old contextual line missing")
	}
}
func TestActivationUnusedStatusDecaysAndUseRehearses(t *testing.T) {
	now := time.Now()
	m := Memory{ID: "status", Owner: OwnerUser, Type: MemoryProjectState, Text: "Task complete", UpdatedAt: now.Add(-60 * 24 * time.Hour)}
	b := SelectMemoryBlock([]Memory{m}, MemoryContext{Owners: []string{OwnerUser}, Now: now})
	if b.Decisions[m.ID].Riding {
		t.Fatal("unused status did not decay")
	}
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	got, err := s.Write(WriteRequest{Owner: OwnerUser, Text: "Use mock amber routing"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecordMemoryEvent(got.Memory.ID, "used", "demo"); err != nil {
		t.Fatal(err)
	}
	r, _, err := s.MemoryRecord(got.Memory.ID)
	if err != nil || r.Rehearsals != 1 || r.Activation <= 1 {
		t.Fatalf("use not consumed: %+v %v", r, err)
	}
	if err = s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	r, _, err = s.MemoryRecord(got.Memory.ID)
	if err != nil || r.Rehearsals != 1 {
		t.Fatalf("rebuild lost learning: %+v %v", r, err)
	}
}
func TestActivationMergeConservativeAndPromotesPin(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	text := "Always send the mock amber delivery queue after verification succeeds"
	a, err := s.Write(WriteRequest{Owner: OwnerUser, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Write(WriteRequest{Owner: OwnerUser, Text: text + ".", Origin: OriginPerson})
	if err != nil || a.Memory.ID != b.Memory.ID {
		t.Fatalf("near duplicate not merged: %+v %v", b, err)
	}
	r, _, _ := s.MemoryRecord(a.Memory.ID)
	if r.Origin != OriginPerson || r.Rehearsals != 1 {
		t.Fatal("merge not rehearsed/pinned")
	}
	for _, distinct := range []string{"Never send the mock amber delivery queue after verification succeeds", "Always send the mock amber delivery queue after verification 2 succeeds", "Always send verification after the mock amber delivery queue succeeds"} {
		b, err = s.Write(WriteRequest{Owner: OwnerUser, Text: distinct})
		if err != nil || b.Memory.ID == a.Memory.ID {
			t.Fatalf("distinct meaning merged: %v", err)
		}
	}
}
func TestActivationIndexedSearchRespectsOwners(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	for _, o := range []string{OwnerUser, OwnerProject("other")} {
		if _, err := s.Write(WriteRequest{Owner: o, Text: "Mock amber shipping queue"}); err != nil {
			t.Fatal(err)
		}
	}
	ms, err := s.SearchMemoriesFor([]string{OwnerUser}, "amber shipping", 20)
	if err != nil || len(ms) != 1 || ms[0].Owner != OwnerUser {
		t.Fatalf("scope leak %v %v", ms, err)
	}
}
func BenchmarkAssembleMemoryBlock(b *testing.B) {
	for _, n := range []int{10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			s, err := Open(filepath.Join(b.TempDir(), "graph.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			tx, err := s.beginWrite()
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < n; i++ {
				_, err = tx.Exec(`INSERT INTO memories(id,owner,type,scope,title,text,status,created_seq,updated_seq) VALUES(?,?,?,?,?,?,?,?,?)`, fmt.Sprint(i), OwnerUser, MemoryFact, MemoryScopeUser, "Mock", "Mock benchmark memory", MemoryActive, i+1, i+1)
				if err != nil {
					b.Fatal(err)
				}
			}
			if err = tx.Commit(); err != nil {
				b.Fatal(err)
			}
			if _, err = s.db.Exec(`INSERT INTO memory_topics SELECT 'mock',memory_id FROM memory_activation; INSERT INTO memory_topics SELECT 'benchmark',memory_id FROM memory_activation`); err != nil {
				b.Fatal(err)
			}
			ctx := MemoryContext{Owners: []string{OwnerUser}, Brief: "mock benchmark", Now: time.Now()}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err = s.AssembleMemoryBlock(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestActivationLearnedContextAndDailyRefresh(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	r, err := s.Write(WriteRequest{Owner: OwnerUser, Text: "Prefer verification before dispatch"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := MemoryContext{Owners: []string{OwnerUser}, Session: "demo", Brief: "amber queue"}
	if err = s.RecordMemoryContextEvent(r.Memory.ID, "used", ctx); err != nil {
		t.Fatal(err)
	}
	ms, err := s.SearchMemoriesFor(ctx.Owners, "amber", 10)
	if err != nil || len(ms) != 1 {
		t.Fatalf("context association not searchable: %d %v", len(ms), err)
	}
	if ms[0].TopicStrength["amber"] != 1 {
		t.Fatal("cooccurrence strength missing")
	}
	now := time.Now().Add(48 * time.Hour)
	if err = s.RefreshMemoryActivationDue(now); err != nil {
		t.Fatal(err)
	}
	a, _, _ := s.MemoryRecord(r.Memory.ID)
	if err = s.RefreshMemoryActivationDue(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	b, _, _ := s.MemoryRecord(r.Memory.ID)
	if a.Activation != b.Activation || !a.ActivationAt.Equal(b.ActivationAt) {
		t.Fatal("daily gate refreshed twice")
	}
	if err = s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	ms, err = s.SearchMemoriesFor(ctx.Owners, "amber", 10)
	if err != nil || len(ms) != 1 {
		t.Fatal("rebuild lost context edges")
	}
}
func TestActivationProjectHubDoesNotOverrulePractice(t *testing.T) {
	now := time.Now()
	owner := OwnerProject("demo")
	ms := []Memory{{ID: "practice", Owner: OwnerUser, Text: "Use mocked amber queues", Activation: 8, ActivationAt: now, UpdatedAt: now}, {ID: "hub", Owner: owner, Text: "Task number 92 finished", OwnerFan: 324, Activation: 1, ActivationAt: now, UpdatedAt: now}}
	b := SelectMemoryBlock(ms, MemoryContext{Owners: []string{OwnerUser, owner}, Project: "demo", Now: now})
	if b.Decisions["hub"].Activation >= b.Decisions["practice"].Activation {
		t.Fatal("generic project hub outranked practice")
	}
}
func TestActivationNoiseStableAndBounded(t *testing.T) {
	if memoryNoise("mock", "session") != memoryNoise("mock", "session") {
		t.Fatal("noise churned")
	}
	if memoryNoise("mock", "session") == memoryNoise("mock", "other") {
		t.Fatal("no exploration")
	}
}

func TestActivationNightlyRefreshNeverRevivesIgnoredStatus(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	r, err := s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryProjectState, Text: "Mock task 12 finished"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(90 * 24 * time.Hour)
	for day := 0; day < 3; day++ {
		at := now.Add(time.Duration(day) * 24 * time.Hour)
		if err = s.RefreshMemoryActivationDue(at); err != nil {
			t.Fatal(err)
		}
		m, _, err := s.MemoryRecord(r.Memory.ID)
		if err != nil {
			t.Fatal(err)
		}
		if m.Activation >= 1 {
			t.Fatal("refresh revived fractional trace")
		}
		b := SelectMemoryBlock([]Memory{m}, MemoryContext{Owners: []string{OwnerUser}, Now: at})
		if b.Decisions[m.ID].Riding {
			t.Fatal("old ignored status resurrected")
		}
	}
}
func TestActivationSupersedePreservesExplicitPin(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	r, err := s.Write(WriteRequest{Owner: OwnerUser, Origin: OriginPerson, Text: "Prefer mocked amber queues"})
	if err != nil {
		t.Fatal(err)
	}
	replacement, _, err := s.SupersedeMemoryForOwners([]string{OwnerUser}, r.Memory.ID, Memory{Owner: OwnerUser, Type: MemoryPreference, Text: "Prefer mocked blue queues", Origin: OriginNoticed})
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := s.MemoryRecord(replacement.ID)
	if err != nil || !m.Rule || m.Origin != OriginNoticed {
		t.Fatalf("pin/provenance lost: %+v %v", m, err)
	}
}
