package session

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type pricedLateNamer struct{ *namingCompleter }

func (n pricedLateNamer) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	response, err := n.namingCompleter.CompleteWithMessages(ctx, messages, options...)
	if err == nil && response != nil && isTitleCall(messages) {
		cost := .0014
		response.Usage.Cost = &cost
	}
	return response, err
}

func TestATitleBilledAfterSealReachesHomesConversationTotal(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "project", "0123456789abcdef")
	if err := os.MkdirAll(filepath.Join(dir, ".cell"), 0o755); err != nil {
		t.Fatal(err)
	}
	namer := naming(&scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return pricedResponse("parser fixed", .002), nil
		},
	}}, namerReply{title: "parser migration"})
	namer.gate = make(chan struct{})
	agent, _ := newTestAgent(t, pricedLateNamer{namer}, func(c *Config) {
		c.ProfileDir = t.TempDir()
		c.Place = Place{Dir: dir, Workspace: c.Workspace}
		c.SessionFile = c.Place.Transcript()
	})
	defer func() {
		select {
		case <-namer.gate:
		default:
			close(namer.gate)
		}
	}()
	collect(t, mustSubmit(t, agent, "Investigate parser migration compatibility, reproduce the regression and fix it."))
	namer.waitAsks(t, 1, "held title call")
	before := stampedMeta(t, agent, dir)
	if before.SpentUSD != .002 {
		t.Fatalf("sealed turn spent %g, want .002 before title billing", before.SpentUSD)
	}
	close(namer.gate)
	waitTitleJob(t, agent)
	agent.SettleWrites()
	usage := agent.Usage()
	if math.Abs(usage.CostUSD-.0034) > 1e-12 {
		t.Fatalf("live conversation total = %g, want .0034", usage.CostUSD)
	}
	rows := ReadWorld(root).Sessions()
	if len(rows) != 1 || rows[0].Spend != usage.CostUSD || rows[0].Tokens != usage.Input+usage.Output {
		t.Fatalf("Home's rows disagree with the live cost after late title billing: rows=%+v usage=%+v", rows, usage)
	}
	if rows[0].At.IsZero() || rows[0].Title != "parser migration" {
		t.Fatalf("late billing lost the conversation's title or activity: %+v", rows[0])
	}
}

func TestLateSpendPreservesAnOwedOpeningStamp(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place = Place{Dir: dir}
	})
	held, release := make(chan struct{}, 1), make(chan struct{})
	userStampHeld = func() {
		held <- struct{}{}
		<-release
	}
	t.Cleanup(func() { userStampHeld = nil })
	var released bool
	defer func() {
		if !released {
			close(release)
		}
	}()
	agent.mu.Lock()
	agent.stampUserLocked("investigate the parser migration")
	agent.mu.Unlock()
	select {
	case <-held:
	case <-time.After(10 * time.Second):
		t.Fatal("opening stamp never reached its hold")
	}
	agent.addDetachedUsageAs(pricedResponse("parser migration", .0014), "test/model", 1, auxRoleTitle)
	close(release)
	released = true
	meta := stampedMeta(t, agent, dir)
	if meta.SpentUSD != .0014 || meta.Tokens != 15 || meta.LastUserAt.IsZero() || meta.Title != "investigate the parser migration" {
		t.Fatalf("late bill lost the opening stamp or its own total: %+v", meta)
	}
}
