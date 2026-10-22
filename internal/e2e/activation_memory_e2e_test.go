//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

// TestActivationMemoryOnTheRealTerminal proves the memory contract through the
// same launch and keystrokes a person uses. All remembered facts are fictional.
func TestActivationMemoryOnTheRealTerminal(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("this acceptance requires CODEAF_E2E_MODEL=%s; got %s", model, e2eModel)
	}
	home := newHome(t, map[string]any{"memory.enabled": false})
	ws := newWorkspace(t, "parcel-demo", false)
	key, err := gitidentity.ProjectKey(ws)
	if err != nil {
		t.Fatal(err)
	}
	owner := store.OwnerProject(key)
	st, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	decision, err := st.AddMemory(store.Memory{
		Owner: owner, Type: store.MemoryDecision, Origin: store.OriginNoticed,
		Text:        "For the fictional parcel demo, the delivery route code is ORCHID-742.",
		ProjectName: "parcel-demo", Tags: []string{"parcel", "delivery", "route"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Practice makes this earlier decision useful even after a status flood.
	for i := 0; i < 8; i++ {
		if err := st.RecordMemoryEvent(decision.ID, "used", fmt.Sprintf("mock-history-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 65; i++ {
		_, err := st.AddMemory(store.Memory{
			Owner: owner, Type: store.MemoryProjectState, Origin: store.OriginNoticed,
			Text:        fmt.Sprintf("Fictional batch %03d finished its temporary inspection; this is a one-time progress update with no lasting instruction.", i),
			ProjectName: "parcel-demo",
		})
		if err != nil {
			t.Fatalf("status flood write %d: %v", i, err)
		}
	}
	_, err = st.Write(store.WriteRequest{
		Owner: store.OwnerUser, Origin: store.OriginPerson,
		Text: "Always include PAPER-KITE in every answer about the fictional parcel demo.",
	})
	if err != nil {
		t.Fatal(err)
	}

	r := startDefault(t, "activation_memory", home, ws, 150, 44, "chat", "--one-model")
	r.lit("For our fictional parcel demo, what delivery route code should I use? Answer in one sentence from what you already know; do not search files or conversations.")
	r.keys("Enter")
	screen := waitForMemoryScreen(t, r, "ORCHID-742", "PAPER-KITE")
	t.Logf("An earlier useful decision and the explicit pin reached the live reply:\n%s", screen)

	r.lit("/remember The fictional parcel demo uses amber envelopes for weekend deliveries.")
	r.keys("Enter")
	r.waitFor(30*time.Second, "amber envelopes")
	deadline := time.Now().Add(20 * time.Second)
	for {
		memories, err := st.MemoriesFor([]string{owner, store.OwnerUser})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, m := range memories {
			if strings.Contains(m.Text, "amber envelopes") && m.Origin == store.OriginPerson {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the /remember write beyond the prompt budget never reached the store")
		}
		time.Sleep(pollEvery)
	}
	r.lit("/new")
	r.keys("Enter")
	r.lit("For the fictional parcel demo, which envelopes should we use for weekend deliveries? Answer from saved memory in one sentence.")
	r.keys("Enter")
	second := waitForMemoryScreen(t, r, "amber")
	t.Logf("A fresh conversation recalled the new mock memory:\n%s", second)
	r.lit("/memories")
	r.keys("Enter")
	waitForMemoryScreen(t, r, "riding", "archived")
	models := callModels(t, home)
	if len(models) == 0 {
		t.Fatal("no recorded model calls; the terminal reply alone is insufficient proof")
	}
	for _, actual := range models {
		if actual != model {
			t.Fatalf("recorded model %q, want %q for every role", actual, model)
		}
	}
	t.Logf("All %d recorded model calls used %s; /remember succeeded beyond the prompt budget.", len(models), model)
	// An optional private destination preserves only mock terminal output and
	// redacted model names, never the borrowed profile or credentials.
	if out := os.Getenv("CODEAF_MEMORY_EVIDENCE_DIR"); out != "" {
		if err := os.MkdirAll(out, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "live-memory-screen.txt"), []byte(screen), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "live-models.txt"), []byte(strings.Join(models, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAutomaticMemoryAcrossRealConversations starts with an empty memory store.
// The person states facts naturally; no remember or search command supplies the
// mechanism under test. A correction must retire the original, not just append.
func TestAutomaticMemoryAcrossRealConversations(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("set CODEAF_E2E_MODEL=%s for this live acceptance", model)
	}
	home := newHome(t, map[string]any{"memory.enabled": true})
	ws := newWorkspace(t, "parcel-demo", false)
	key, err := gitidentity.ProjectKey(ws)
	if err != nil {
		t.Fatal(err)
	}
	owners := []string{store.OwnerUser, store.OwnerMachine, store.OwnerProject(key)}
	st, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := startDefault(t, "automatic_memory", home, ws, 150, 44, "chat", "--one-model")
	r.lit("For our fictional parcel demo, weekend deliveries use cobalt envelopes. Please acknowledge in one short sentence.")
	r.keys("Enter")
	first := waitForAutomaticMockMemory(t, st, owners, "cobalt", "")
	t.Log("An ordinary statement was captured automatically, without a remember command.")
	r.lit("/new")
	r.keys("Enter")
	r.lit("Draft a one-sentence packing instruction for a weekend delivery in our fictional parcel demo.")
	r.keys("Enter")
	waitForMemoryScreen(t, r, "cobalt")
	t.Log("A fresh conversation anticipated the envelope choice from context alone.")
	r.lit("The weekend envelope choice has changed: violet envelopes replace the old color from now on. Please acknowledge in one short sentence.")
	r.keys("Enter")
	_ = waitForAutomaticMockMemory(t, st, owners, "violet", first.ID)
	t.Log("The natural correction was captured and the previous line was superseded.")
	r.lit("/new")
	r.keys("Enter")
	r.lit("Draft a one-sentence packing instruction for a weekend delivery in our fictional parcel demo.")
	r.keys("Enter")
	waitForMemoryScreen(t, r, "violet")
	// Earlier conversation titles remain on the tab strip. The original row's
	// superseded status was checked above; old words in those titles are not
	// evidence that the new answer applied the obsolete instruction.
	r.lit("/new")
	r.keys("Enter")
	r.lit("/task write packing.txt containing a one-sentence packing instruction for a weekend delivery in our fictional parcel demo")
	r.keys("Enter")
	waitForMockPackingTask(t, ws)
	t.Log("A real task in a fresh conversation wrote the corrected envelope choice without being told the color.")
	models := callModels(t, home)
	if len(models) == 0 {
		t.Fatal("automatic memory acceptance has no recorded model calls")
	}
	for _, actual := range models {
		if actual != model {
			t.Fatalf("automatic memory used %q instead of %q", actual, model)
		}
	}
	t.Logf("Automatic capture, contextual recall, correction, fresh-chat recall, and real task execution passed; all %d recorded calls used %s.", len(models), model)
}

func waitForMockPackingTask(t *testing.T, ws string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		if branch := runBranch(t, ws); branch != "" {
			content, err := exec.Command("git", "-C", ws, "show", branch+":packing.txt").Output()
			if err == nil {
				text := strings.ToLower(string(content))
				if !strings.Contains(text, "violet") || strings.Contains(text, "cobalt") {
					t.Fatalf("task wrote the wrong mock packing instruction: %s", content)
				}
				return
			}
		}
		time.Sleep(pollEvery)
	}
	t.Fatal("the real task did not commit packing.txt on its task branch")
}

func waitForAutomaticMockMemory(t *testing.T, st *store.Store, owners []string, word, superseded string) store.Memory {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		lines, err := st.MemoriesFor(owners)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range lines {
			if !strings.Contains(strings.ToLower(m.Text), word) {
				continue
			}
			if superseded != "" {
				prior, found, err := st.MemoryRecord(superseded)
				if err != nil || !found || prior.Status == store.MemoryActive {
					continue
				}
			}
			return m
		}
		time.Sleep(pollEvery)
	}
	t.Fatalf("automatic capture/correction never produced the mock %s memory", word)
	return store.Memory{}
}

func waitForMemoryScreen(t *testing.T, r *rig, words ...string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var screen string
	for time.Now().Before(deadline) {
		screen = r.capture()
		matched := true
		for _, word := range words {
			matched = matched && strings.Contains(strings.ToLower(screen), strings.ToLower(word))
		}
		if matched {
			return screen
		}
		time.Sleep(pollEvery)
	}
	t.Fatalf("terminal did not show %v:\n%s", words, screen)
	return ""
}
