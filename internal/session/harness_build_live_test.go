package session

// THE REPRODUCTION. This file drives a REAL design against a REAL model on the
// session's own path — [Agent.designPage], not the rig's copy of it — because
// the complaint was about the chat and the chat is the only place the belt, the
// briefs and the role ladder come together the way they do here.
//
// It is OPT-IN and costs money:
//
//	AFORGE_LIVE_DESIGN=1 go test ./internal/session -run LiveDesign -v
//	AFORGE_LIVE_DESIGN=1 AFORGE_LIVE_MODEL=deepseek/deepseek-v4-pro go test ...

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func liveKey(t *testing.T) string {
	t.Helper()
	if os.Getenv("AFORGE_LIVE_DESIGN") == "" {
		t.Skip("set AFORGE_LIVE_DESIGN=1 to spend real design turns on this")
	}
	if key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")); key != "" {
		return key
	}
	// The rig reads the environment; a person's install keeps the key in the
	// same file the surface reads it from.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no OPENROUTER_API_KEY and no home directory to look in")
	}
	data, err := os.ReadFile(filepath.Join(home, ".aforge", "config.json"))
	if err != nil {
		t.Skip("no OPENROUTER_API_KEY and no ~/.aforge/config.json")
	}
	var config struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(data, &config); err != nil || strings.TrimSpace(config.APIKey) == "" {
		t.Skip("no OPENROUTER_API_KEY and ~/.aforge/config.json has none")
	}
	return strings.TrimSpace(config.APIKey)
}

// TestLiveDesignResearchHelper is the user's own sentence, on the user's own
// model, through the session's own designer.
func TestLiveDesignResearchHelper(t *testing.T) {
	key := liveKey(t)
	model := os.Getenv("AFORGE_LIVE_MODEL")
	if model == "" {
		model = "deepseek/deepseek-v4-pro"
	}
	dir := t.TempDir()
	agent, err := New(Config{
		Workspace:    dir,
		Model:        model,
		APIKey:       key,
		BaseURL:      "https://openrouter.ai/api/v1",
		HarnessStore: subharness.At(filepath.Join(dir, "harnesses")),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()

	trials := 1
	if n := os.Getenv("AFORGE_LIVE_TRIALS"); n != "" {
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 {
			trials = parsed
		}
	}
	goal := os.Getenv("AFORGE_LIVE_GOAL")
	if goal == "" {
		goal = "research helper: given a topic, fetch 3 sources and summarize with citations"
	}
	for trial := 1; trial <= trials; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) { oneLiveDesign(t, agent, goal, model) })
	}
}

func oneLiveDesign(t *testing.T, agent *Agent, goal, model string) {
	ctx, cancel := context.WithTimeout(context.Background(), harnessDesignWindow)
	defer cancel()

	// The loop is [Agent.designPage]'s, spelled out, so that EVERY attempt's
	// error and the raw reply behind it land in the log — the failure this rig
	// exists to explain is the one that is different on every try.
	designer, reviewer, err := agent.harnessBriefs()
	if err != nil {
		t.Fatal(err)
	}
	_ = reviewer
	history := []ai.Message{
		textMessage("system", designer),
		textMessage("user", "THE GOAL:\n\n"+goal+"\n\nDesign the sub-harness for it."),
	}
	var (
		draft harnessDesign
		page  subharness.Harness
		cues  []string
	)
	began := time.Now()
	for tries := 0; ; tries++ {
		at := time.Now()
		var raw string
		// The seat is empty: this rig has no node, so there is no room to stream
		// into and no journal to keep the reply in — the log below is the record.
		draft, page, raw, err = agent.designHarnessOnce(ctx, history, model, goal, tries+1, designSeat{})
		if err == nil {
			t.Logf("attempt %d ACCEPTED in %s", tries+1, time.Since(at).Round(time.Millisecond))
			cues = draft.Cues
			break
		}
		t.Logf("attempt %d REFUSED in %s: %v", tries+1, time.Since(at).Round(time.Millisecond), err)
		t.Logf("attempt %d raw reply (%d bytes):\n%s", tries+1, len(raw), raw)
		if tries >= harnessDesignRetries {
			t.Fatalf("no valid design in %d attempts: %v", tries+1, err)
		}
		history = append(history,
			textMessage("assistant", raw),
			textMessage("user", "That harness was REFUSED:\n\n"+err.Error()+
				"\n\nFix exactly that and reply with the whole envelope again — one JSON object, no prose."))
	}
	took := time.Since(began)
	t.Logf("design accepted in %s · %s · %d nodes · cues %s",
		took.Round(time.Millisecond), page.Id.Name, len(page.Program.Nodes), strings.Join(cues, " · "))
	if _, err := subharness.Encode(page); err != nil {
		t.Fatalf("the accepted page will not re-encode: %v", err)
	}
	// END TO END MEANS SAVED. A page that validates and will not write is a
	// design the person watched succeed and then lost.
	saved, err := agent.saveHarness(page, cues)
	if err != nil {
		t.Fatalf("the accepted page would not save: %v", err)
	}
	t.Logf("saved %s v%d", saved.Id.Name, saved.Id.Version)
}
