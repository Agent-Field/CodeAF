//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/session"
)

// breakdownDoor is a workstream with nobody's tasks in it yet, which keeps every
// task the agent makes. It answers each ws_* call the way the server's door
// does in shape, without a store behind it.
type breakdownDoor struct {
	agentTestDoor
	mu    sync.Mutex
	made  []breakdownTask
	other []string
	// pricing puts the pricing page in the workstream, typo and all, so a
	// small fix is one Afi can make itself; edits are the changes it made.
	pricing bool
	edits   []string
}

// pricingPage is the workstream's pricing page, typo and all.
const pricingPage = "# Plans and pricing\n\n## Save hours on Recipies every week\n\nPantry Plus is $4 a month: unlimited meal plans, shared shopping lists and pantry tracking.\n"

type breakdownTask struct {
	Title   string `json:"title"`
	Request string `json:"request"`
	Owner   string `json:"owner,omitempty"`
	DoneBy  string `json:"done_by,omitempty"`
	Group   string `json:"group,omitempty"`
	// Checklist is the deliverable's steps (CodeAF #2745).
	Checklist []string `json:"checklist,omitempty"`
}

func (d *breakdownDoor) Workstream(context.Context) (factory.OrgWorkstream, error) {
	return factory.OrgWorkstream{Name: "Pantry 2.4"}, nil
}

func (d *breakdownDoor) Work(_ context.Context, tool string, args json.RawMessage) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch tool {
	case session.WSTaskCreateTool:
		var t breakdownTask
		_ = json.Unmarshal(args, &t)
		d.made = append(d.made, t)
		return fmt.Sprintf(`{"id":"tk_%d","title":%q,"state":"up_next"}`, len(d.made), t.Title), nil
	case session.WSTasksTool:
		return `{"tasks":[],"people":[{"id":"priya","name":"Priya"},{"id":"marco","name":"Marco"},{"id":"lena","name":"Lena"},{"id":"sam","name":"Sam"}]}`, nil
	case session.WSDecisionsTool:
		return `{"decisions":[]}`, nil
	case session.WSFilesTool:
		if d.pricing {
			return `{"files":[{"id":"f_pricing","name":"Pricing page","kind":"document","edited_by_person":true}]}`, nil
		}
		return `{"files":[]}`, nil
	case session.WSFileReadTool:
		if d.pricing {
			raw, _ := json.Marshal(map[string]any{"file": map[string]any{"id": "f_pricing", "name": "Pricing page", "kind": "document", "edited_by_person": true, "text": pricingPage}, "comments": []any{}})
			return string(raw), nil
		}
	case session.WSFileSuggestTool, session.WSFileCreateTool:
		d.edits = append(d.edits, tool+" "+string(args))
		return `{"ok":true,"waits":"as a suggestion for someone to take"}`, nil
	}
	d.other = append(d.other, tool)
	return `{"ok":true}`, nil
}

// offersFix is a reply that offers to do the small thing itself ("Add it to
// Files and I'll fix it at once").
var offersFix = regexp.MustCompile(`(?i)\bI(?:'|’)ll\b[^.]*\b(fix|correct|change)`)

// steps is how many lines the deliverable's checklist carries.
func (t breakdownTask) steps() int { return len(t.Checklist) }

// AFI'S BREAKDOWN, MEASURED ON THE REAL MODEL (OWNER-DECISIONS 2026-11-17 §7).
// Three requests a person really sends get 5 to 8 deliverables each, every one
// carrying its steps; a small request stays one task or none, and with none Afi does it itself, or offers what it can do now. Opt in with
// CODEAF_LIVE_FACTORY=1; CODEAF_LIVE_BREAKDOWN_OUT names a JSON file for the
// measurements. It spends about a cent per request.
func TestLiveAfiBreaksBigWorkIntoDeliverables(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 (spends a few cents on OpenRouter)", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads; not faking a pass")
	}
	requests := []struct {
		name, text string
		big        bool
		// pricing puts the page in the workstream; the small fix is then Afi's to make.
		pricing bool
	}{
		{"launch", "Plan the Pantry 2.4 launch for March 12: the shopping-list feature ships then. We need the blog post, App Store copy, the newsletter, social posts, a press list and whatever the launch day needs. Marco and Sam handle the build, Lena the visuals, I'm on the words.", true, false},
		{"research", "We need to understand why people stop using meal plans after the first week. Set up the research for next month: who we talk to, the interview guide, recruiting, running the sessions, and a readout the team can act on.", true, false},
		{"docs", "Get the help center ready for 2.4: the shopping list needs new articles, the old meal-plan pages are wrong since 2.3, screenshots are stale, and support wants a short FAQ before launch.", true, false},
		{"small", "The pricing page headline says 'Recipies' — please make a task to fix that typo.", false, false},
		{"small-here", "The pricing page headline says 'Recipies' — please make a task to fix that typo.", false, true},
	}
	type measured struct {
		Name    string          `json:"name"`
		Tasks   int             `json:"tasks"`
		Steps   []int           `json:"steps"`
		Titles  []string        `json:"titles"`
		Groups  []string        `json:"groups"`
		Seconds float64         `json:"seconds"`
		USD     float64         `json:"usd"`
		Made    []breakdownTask `json:"made"`
		Edits   []string        `json:"edits,omitempty"`
		Said    string          `json:"said"`
	}
	var out []measured
	run := factoryWorkstreamAgent(nil, func() (config.Config, error) { return config.Config{Model: liveFactoryModel}, nil })
	for _, r := range requests {
		t.Run(r.name, func(t *testing.T) {
			door := &breakdownDoor{pricing: r.pricing}
			var said strings.Builder
			ctx, cancel := context.WithTimeout(context.Background(), liveFactoryWall)
			defer cancel()
			began := time.Now()
			end, err := run(ctx, serve.AgentTurn{Workstream: "ws_live", Name: "Pantry 2.4", Turn: "turn_live_" + r.name, Asker: "priya",
				Text: r.text, Now: time.Now(), Key: liveFactoryKey, Dir: t.TempDir(), Door: door,
				Step: func(kind, text, tool string, failed bool) {
					if kind == "agent" {
						said.WriteString(text)
					}
				}})
			if err != nil {
				t.Fatalf("the turn failed: %v", err)
			}
			m := measured{Name: r.name, Tasks: len(door.made), Seconds: time.Since(began).Seconds(), USD: end.USD, Made: door.made, Edits: door.edits, Said: said.String()}
			for _, task := range door.made {
				m.Steps = append(m.Steps, task.steps())
				m.Titles = append(m.Titles, task.Title)
				m.Groups = append(m.Groups, task.Group)
			}
			out = append(out, m)
			t.Logf("%s: %d tasks, steps %v, %.0fs, $%.4f: %q", r.name, m.Tasks, m.Steps, m.Seconds, m.USD, m.Titles)
			if r.big && (m.Tasks < session.WorkstreamPlanFewest || m.Tasks > session.WorkstreamPlanMost) {
				t.Errorf("big work became %d tasks; the rule is %d to %d", m.Tasks, session.WorkstreamPlanFewest, session.WorkstreamPlanMost)
			}
			if !r.big && m.Tasks > 1 {
				t.Errorf("a small request became %d tasks", m.Tasks)
			}
			// NO TASK MEANS AFI DID IT, OR OFFERED WHAT IT CAN DO NOW: never a
			// reply that only says why there is no task.
			if r.pricing && m.Tasks == 0 && len(m.Edits) == 0 {
				t.Errorf("small work it could reach got no task and no change: %q", m.Said)
			}
			if !r.big && !r.pricing && m.Tasks == 0 && !offersFix.MatchString(m.Said) {
				t.Errorf("small work it could not reach got no offer to do it: %q", m.Said)
			}
		})
	}
	if path := os.Getenv("CODEAF_LIVE_BREAKDOWN_OUT"); path != "" {
		raw, _ := json.MarshalIndent(out, "", "  ")
		_ = os.WriteFile(path, raw, 0o644)
	}
}
