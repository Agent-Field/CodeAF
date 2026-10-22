//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

// Fictional instructions exercise capture, fresh-chat recall, correction, and
// application without exposing the evaluator or an expected answer to the app.
func TestMemoryCapturePreservesConditionalChartInstructions(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("requires model %s", model)
	}
	home := newHome(t, map[string]any{"memory.enabled": true, "vision_model": model, "model.image": model, "model.speech": model, "model.music": model, "model.video": model, "model.voice": model})
	ws := newWorkspace(t, "cedar-rainfall", false)
	key, err := gitidentity.ProjectKey(ws)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	owners := []string{store.OwnerUser, store.OwnerMachine, store.OwnerProject(key)}
	var answers []string
	var admittedSources []string
	var expectedSource string
	r := startPrivateMemory(t, "capture_integrity", home, ws, 160, 50, "chat", "--one-model", "--debug")
	ask := func(prompt string, fresh bool) string {
		if fresh {
			r.lit("/new")
			r.keys("Enter")
			time.Sleep(time.Second)
		}
		r.lit(prompt)
		r.keys("Enter")
		answer, tools := privateMemoryAwaitAnswer(home, prompt, 3*time.Minute)
		if answer == "" {
			t.Fatal("ordinary chat produced no completed answer")
		}
		if privateMemoryLookupUsed(tools) {
			t.Logf("ordinary chat used lookup tools: %v", tools)
		}
		if fresh && expectedSource != "" {
			present, block, _ := privateMemoryPromptGroundTruth(home, prompt, expectedSource)
			if present == nil || !*present {
				t.Fatal("no first-request trace proves automatic source admission")
			}
			admittedSources = append(admittedSources, block)
		}
		answers = append(answers, answer)
		return answer
	}
	ask("For the fictional Cedar rainfall demo, if the entire chart cannot fit in the chat viewport, never squeeze it into the message; provide a link to open the full chart instead. This is a standing presentation rule. Separately, propose a possible chart orientation as an example, not as a preference or requirement from me.", false)
	first := awaitChartCapture(t, st, owners, "link", "")
	expectedSource = first.Text
	answer := ask("How should we present an oversized chart in the fictional Cedar rainfall demo, and what changes if it fits in chat? Answer briefly.", true)
	if !strings.Contains(strings.ToLower(answer), "link") {
		t.Fatalf("fresh reply lost overflow delivery: %s", answer)
	}
	ask("For the fictional Cedar rainfall demo, replace the previous chart-link rule: if the entire chart cannot fit in the chat viewport, never put it inline; save the full chart as an SVG attachment instead.", true)
	corrected := awaitChartCapture(t, st, owners, "svg", first.ID)
	expectedSource = corrected.Text
	ask("Create presentation-plan.json for the fictional Cedar rainfall demo using keys fits_in_chat and does_not_fit_in_chat. Each value must be an object with delivery (a short instruction) and inline_allowed (a boolean). Apply our current chart-presentation rule. Do not render a chart.", true)
	body, err := os.ReadFile(filepath.Join(ws, "presentation-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]struct {
		Delivery string `json:"delivery"`
		Inline   *bool  `json:"inline_allowed"`
	}
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatal(err)
	}
	fits, overflow := plan["fits_in_chat"], plan["does_not_fit_in_chat"]
	if fits.Inline == nil || !*fits.Inline || overflow.Inline == nil || *overflow.Inline || !strings.Contains(strings.ToLower(overflow.Delivery), "svg") || !strings.Contains(strings.ToLower(overflow.Delivery), "attach") {
		t.Fatalf("task did not apply corrected conditional rule: %s", body)
	}
	models := callModels(t, home)
	if len(models) == 0 {
		t.Fatal("no model receipts")
	}
	for _, actual := range models {
		if actual != model {
			t.Fatalf("unexpected actual model %q", actual)
		}
	}
	if evidence := os.Getenv("CODEAF_MEMORY_EVIDENCE_DIR"); evidence != "" {
		rows, err := st.MemoriesFor(owners)
		if err != nil {
			t.Fatal(err)
		}
		texts := make([]string, 0, len(rows))
		for _, row := range rows {
			texts = append(texts, fmt.Sprintf("%s: %s", row.Status, row.Text))
		}
		if err := os.MkdirAll(evidence, 0700); err != nil {
			t.Fatal(err)
		}
		privateMemoryWrite(t, filepath.Join(evidence, "mock-capture-integrity.json"), map[string]any{"answers": answers, "stored_rows": texts, "presentation_plan": json.RawMessage(body), "actual_models": models, "first_request_memory_blocks": admittedSources})
	}
	t.Logf("four ordinary chats preserved the conditional rule, corrected it, and applied it to a real file; %d exact-model receipts", len(models))
}

var chartGeometryGloss = regexp.MustCompile(`(?i)\b(horizontal|vertical|landscape|portrait|axes|axis|rotation|rotated)\b`)

func chartCaptureComplete(text, action string) bool {
	text = strings.ToLower(text)
	condition := strings.Contains(text, "fit") && (strings.Contains(text, "cannot") || strings.Contains(text, "can't") || strings.Contains(text, "not fit") || strings.Contains(text, "doesn't") || strings.Contains(text, "won't"))
	condition = condition || strings.Contains(text, "overflow") || strings.Contains(text, "oversized")
	force := strings.Contains(text, "never") || strings.Contains(text, "must not") || strings.Contains(text, "do not") || strings.Contains(text, "don't")
	negativeAction := strings.Contains(text, "never provide a "+action) || strings.Contains(text, "never use a "+action) || strings.Contains(text, "do not provide a "+action)
	return strings.Contains(text, "chart") && condition && force && strings.Contains(text, action) && !negativeAction
}

func awaitChartCapture(t *testing.T, st *store.Store, owners []string, action, oldID string) store.Memory {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		rows, err := st.MemoriesFor(owners)
		if err != nil {
			t.Fatal(err)
		}
		var found store.Memory
		oldActive := false
		for _, m := range rows {
			if m.Status != store.MemoryActive {
				continue
			}
			if m.ID == oldID {
				oldActive = true
			}
			if !strings.Contains(strings.ToLower(m.Text), "chart") {
				continue
			}
			if chartGeometryGloss.MatchString(m.Text) {
				t.Fatalf("stored an unaccepted assistant geometry gloss: %s", m.Text)
			}
			if chartCaptureComplete(m.Text, action) {
				found = m
			}
		}
		if found.ID != "" && !oldActive {
			return found
		}
		if time.Now().After(deadline) {
			t.Fatalf("capture failed to retain condition and prohibition for %s, or left the replaced rule active", action)
		}
		time.Sleep(pollEvery)
	}
}

func TestChartCaptureSemanticChecks(t *testing.T) {
	good := "If a chart cannot fit in chat, never squeeze it inline; supply a full-chart link."
	if !chartCaptureComplete(good, "link") {
		t.Fatal("valid paraphrase rejected")
	}
	for _, bad := range []string{"If chart cannot fit, never provide a link; squeeze inline.", "Prefer chart links.", "Never embed a chart; provide a link.", "If a chart cannot fit, a link is preferred."} {
		if chartCaptureComplete(bad, "link") {
			t.Fatalf("lost force or condition accepted: %s", bad)
		}
	}
	if !chartGeometryGloss.MatchString("Charts should use horizontal axes") {
		t.Fatal("geometry contamination not recognized")
	}
}
