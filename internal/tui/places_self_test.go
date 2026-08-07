package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type selfSeedBackend struct {
	*fakeBackend
	receipts   []store.SelfReceipt
	selfSpend  float64
	competence store.CompetenceMap
	facts      []store.Fact
	skills     []store.Fact
	charters   []store.Charter
	services   []store.Service
}

func (b *selfSeedBackend) SkillFacts(status string, limit int) ([]store.Fact, error) {
	skills := append([]store.Fact(nil), b.skills...)
	if limit > 0 && len(skills) > limit {
		skills = skills[:limit]
	}
	return skills, nil
}

func (b *selfSeedBackend) ActiveServices() ([]store.Service, error) {
	return append([]store.Service(nil), b.services...), nil
}

func (b *selfSeedBackend) SelfReceipts(time.Time) ([]store.SelfReceipt, error) {
	return append([]store.SelfReceipt(nil), b.receipts...), nil
}

func (b *selfSeedBackend) SelfSpendToday() (float64, error) { return b.selfSpend, nil }

func (b *selfSeedBackend) CompetenceMap(...store.CompetenceOptions) (store.CompetenceMap, error) {
	return b.competence, nil
}

func (b *selfSeedBackend) RecentFacts(limit int) ([]store.Fact, error) {
	facts := append([]store.Fact(nil), b.facts...)
	if limit > 0 && len(facts) > limit {
		facts = facts[:limit]
	}
	return facts, nil
}

func (b *selfSeedBackend) Charters(...store.CharterStatus) ([]store.Charter, error) {
	return append([]store.Charter(nil), b.charters...), nil
}

func seededSelfBackend(now time.Time) *selfSeedBackend {
	delta := 0.25
	snapshot := store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "practice-live", Parent: store.RootID, Title: "Parser practice", Group: store.PracticeGroup,
			Status: store.Running, CreatedSeq: 8, Provenance: store.Provenance{Origin: store.OriginSelf}},
		// A charter and one journaled firing: Self reads tenure from the
		// store record and last-fired from the same projection the rail uses.
		{ID: "parser-watch", Parent: store.RootID, Group: "charter", Title: "parser watch",
			Provenance: store.Provenance{Intent: "Keep parser recovery healthy"}},
		{ID: "parser-watch-firing", Parent: store.RootID, Group: "charter:firing:parser-watch",
			Title: "recheck the parser", Status: store.Done, FinishedAt: now.Add(-2 * time.Hour),
			Provenance: store.Provenance{Origin: store.OriginTrigger, CharterID: "parser-watch"}},
	}}
	return &selfSeedBackend{
		fakeBackend: &fakeBackend{snapshot: snapshot},
		receipts: []store.SelfReceipt{{
			Seq: 9, Time: now.Add(-time.Hour), NodeID: "practice-live",
			Origin: "compare parser recovery", Scope: "repo:/parser", Cost: 0.43,
			FactIDs: []int64{21}, SurpriseDelta: &delta,
		}},
		skills: []store.Fact{{
			Seq: 30, Time: now.Add(-2 * time.Hour), Kind: store.FactSkill, Status: store.FactActive,
			Body: "csvsplit — split a csv by column", Artifact: "skills/csvsplit",
		}},
		services: []store.Service{{
			ID: "svc-1", Name: "docs preview", Status: store.ServiceRunning,
			StartedAt: now.Add(-90 * time.Minute),
		}},
		selfSpend: 0.43,
		competence: store.CompetenceMap{Scopes: []store.ScopeCompetence{
			{Scope: "repo:/strong", Class: store.CompetenceStrong, Samples: 10, FailureRate: 0.1},
			{Scope: "repo:/frontier", Class: store.CompetenceFrontier, Samples: 8, FailureRate: 0.5},
			{Scope: "repo:/weak", Class: store.CompetenceWeak, Samples: 5, FailureRate: 0.8},
		}},
		facts: []store.Fact{{
			Seq: 21, Time: now.Add(-time.Hour), Scope: "repo:/parser", Kind: store.FactLesson,
			Body: "Retry malformed records one at a time.", Status: store.FactActive,
		}},
		charters: []store.Charter{{
			ID: "parser-watch", Invariant: "Keep parser recovery healthy", Status: store.CharterActive,
			Autonomy: store.CharterProbation, GreenFirings: 2,
		}},
	}
}

func altPlaceKey(number rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{number}, Alt: true}
}

func TestHeaderPlaceHitTargetsSwitchThreadBoardAndSelf(t *testing.T) {
	model := New(seededSelfBackend(time.Now()), "places")
	model.setSize(140, 32)
	_ = model.View()
	for name, bounds := range map[string]paneBounds{
		"thread": model.headerThreadBounds,
		"board":  model.headerBoardBounds,
		"self":   model.headerSelfBounds,
	} {
		if bounds.width == 0 {
			t.Fatalf("%s place has no header click bounds", name)
		}
	}

	_, _ = model.updateMouseClick(model.headerBoardBounds.x, model.headerBoardBounds.y)
	if !model.graphOpen || model.selfOpen || model.focus != focusGraph {
		t.Fatalf("board click = graph %t self %t focus %v", model.graphOpen, model.selfOpen, model.focus)
	}
	_ = model.View()
	_, _ = model.updateMouseClick(model.headerSelfBounds.x, model.headerSelfBounds.y)
	if model.graphOpen || !model.selfOpen || model.focus != focusSelf {
		t.Fatalf("self click = graph %t self %t focus %v", model.graphOpen, model.selfOpen, model.focus)
	}
	_ = model.View()
	_, _ = model.updateMouseClick(model.headerThreadBounds.x, model.headerThreadBounds.y)
	if model.graphOpen || model.selfOpen || model.focus != focusInput {
		t.Fatalf("thread click = graph %t self %t focus %v", model.graphOpen, model.selfOpen, model.focus)
	}
}

func TestHeaderPlaceAttentionDotsFollowTheirOwnSources(t *testing.T) {
	model := New(&fakeBackend{}, "places")
	model.setSize(140, 32)
	model.agentQuestions = []store.AgentQuestion{{
		Seq: 1, Status: store.QuestionPending, Urgency: store.QuestionWhenever,
	}}
	model.selfCharters = []store.Charter{{
		ID: "proposal", Invariant: "Watch the release", Status: store.CharterProposed,
	}}
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "failed", Parent: store.RootID, Status: store.Failed},
	}}
	header := ansi.Strip(strings.Split(model.View(), "\n")[0])
	if !strings.Contains(header, "thread ● · board ● · self ●") {
		t.Fatalf("place attention dots do not follow thread/board/self sources: %q", header)
	}
}

func TestAltNumberKeysSwitchPlaces(t *testing.T) {
	model := New(&fakeBackend{}, "places")
	model.notebookOpen = true
	_, _ = model.Update(altPlaceKey('2'))
	if model.activePlace() != placeBoard || !model.graphOpen {
		t.Fatalf("alt+2 place = %v graph=%t", model.activePlace(), model.graphOpen)
	}
	if model.notebookOpen {
		t.Fatal("leaving the thread left the notebook open behind the board")
	}
	_, _ = model.Update(altPlaceKey('3'))
	if model.activePlace() != placeSelf || !model.selfOpen || model.graphOpen {
		t.Fatalf("alt+3 place = %v graph=%t self=%t", model.activePlace(), model.graphOpen, model.selfOpen)
	}
	_, _ = model.Update(altPlaceKey('1'))
	if model.activePlace() != placeThread || model.graphOpen || model.selfOpen {
		t.Fatalf("alt+1 place = %v graph=%t self=%t", model.activePlace(), model.graphOpen, model.selfOpen)
	}
}

func TestSelfEscLadderWalksBackOutThenReturnsHomeToThread(t *testing.T) {
	model := New(&fakeBackend{}, "self")
	_, _ = model.Update(altPlaceKey('3'))
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.selfOpen || model.selfRoute != selfRouteCrafts {
		t.Fatalf("enter did not drill into the first Self row: open=%t route=%d", model.selfOpen, model.selfRoute)
	}
	_, quit := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if quit != nil || !model.selfOpen || model.selfRoute != selfRouteRoot {
		t.Fatalf("first esc should only leave the drill-in: quit=%v open=%t route=%d",
			quit, model.selfOpen, model.selfRoute)
	}
	_, quit = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if quit != nil || model.activePlace() != placeThread || model.selfOpen || model.focus != focusInput {
		t.Fatalf("second esc did not return home: quit=%v place=%v self=%t focus=%v",
			quit, model.activePlace(), model.selfOpen, model.focus)
	}
}

func TestSelfSlashCommandOpensSelfPlace(t *testing.T) {
	model := New(&fakeBackend{}, "self")
	_ = model.executeSlash("/self")
	if model.activePlace() != placeSelf || model.focus != focusSelf {
		t.Fatalf("/self place=%v focus=%v", model.activePlace(), model.focus)
	}
}
