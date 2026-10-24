//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/store"
)

// Invalid helper IDs are rejected by this local fault injector, never sent to
// OpenRouter. The real terminal must recover through the conversation model,
// save automatically, recall in a fresh chat, correct, and apply the saved rule.
func TestMemoryRecoversFromInvalidHelpersOnTheRealTerminal(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
	}
	target, _ := url.Parse(config.DefaultBaseURL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) { director(r); r.Host = target.Host }
	proxy.FlushInterval = -1
	var mu sync.Mutex
	rejected := map[string]int{}
	forwarded := 0
	var observed []string
	const primary = "fixture/unavailable-memory-primary"
	const low = "fixture/unavailable-memory-low"
	const updated = "fixture/unavailable-memory-updated"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "read", 400)
				return
			}
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			var request struct {
				Model    string `json:"model"`
				Messages []struct {
					Content json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			if json.Unmarshal(body, &request) != nil {
				http.Error(w, "json", 400)
				return
			}
			isMemory := len(request.Messages) > 0 && strings.Contains(string(request.Messages[0].Content), "You read one exchange between a person and an assistant")
			mu.Lock()
			if request.Model == primary || request.Model == low || request.Model == updated {
				rejected[request.Model]++
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if request.Model == low {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":{"message":"fixture/unavailable-memory-low is not a valid model ID","code":400}}`)
				} else {
					w.WriteHeader(http.StatusNotFound)
					_, _ = io.WriteString(w, `{"error":{"message":"No endpoints found for fixture model","code":404}}`)
				}
				return
			}
			if request.Model != model {
				mu.Unlock()
				http.Error(w, "unexpected model blocked by acceptance fixture", 400)
				return
			}
			observed = append(observed, request.Model)
			if isMemory {
				forwarded++
			}
			mu.Unlock()
		}
		// Strip the fixture prefix; everything after the injected outage is an
		// authenticated request to the actual OpenRouter endpoint.
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/fixture")
		proxy.ServeHTTP(w, r)
	}))
	defer server.Close()
	var pins []string
	for _, role := range textRoles {
		chosen := model
		if role == roles.RoleReflex {
			chosen = primary
		}
		pins = append(pins, string(role)+":"+chosen)
	}
	home := newHome(t, map[string]any{
		"memory.enabled":          true,
		config.KeyTierReflexModel: primary,
		config.KeyTierLowModel:    low,
		config.KeyModelRoles:      strings.Join(pins, ","),
		config.KeyModelFallbacks:  "",
	})
	home = recoveryShortHome(t, home)
	ws := newWorkspace(t, "juniper-recovery", false)
	project, err := gitidentity.ProjectKey(ws)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	stateEnv := []string{config.APIKeyEnv + "=" + liveKey(t), "CODEAF_BASE_URL=" + server.URL + "/fixture"}
	stopRecoveryEngineOnCleanup(t, stateEnv, home, ws)
	r := startWithEnv(t, stateEnv, "memory_invalid_helpers", home, ws, 150, 45, "chat", "--model", model)
	r.skipSetup(t)
	prompt := "For the fictional Juniper demo, our standing packaging convention is violet envelopes for weekend parcels. Please acknowledge in one sentence without calling any tools."
	r.lit(prompt)
	r.keys("Enter")
	answer, tools := privateMemoryAwaitAnswer(home, prompt, 2*time.Minute)
	if answer == "" {
		t.Fatal("chat did not finish during invalid helper recovery")
	}
	for _, tool := range tools {
		if strings.Contains(tool, "remember") {
			t.Fatal("main turn bypassed background recovery")
		}
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		rows, err := st.MemoriesFor([]string{store.OwnerProject(project), store.OwnerUser})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, m := range rows {
			if m.Status == store.MemoryActive && strings.Contains(strings.ToLower(m.Text), "violet") && strings.Contains(strings.ToLower(m.Text), "weekend") {
				if m.Origin != store.OriginNoticed {
					t.Fatalf("memory was not saved by background noticing: origin=%s", m.Origin)
				}
				n++
			}
		}
		if n > 1 {
			t.Fatal("recovery duplicated the memory")
		}
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background recovery never saved original packaging convention")
		}
		time.Sleep(pollEvery)
	}

	// A different process edits the profile while the original engine remains
	// attached. A NEW unavailable ID must be attempted, proving this was a
	// fresh settings read rather than the cached original failed helper.
	beforeStatus := upgradeEngineCommand(t, binary(t), stateEnv, home, ws, "--status")
	beforePID := upgradePID(beforeStatus)
	if beforePID == "" {
		t.Fatalf("fixture did not start a background engine: %s", beforeStatus)
	}
	configPath := filepath.Join(home, "config.json")
	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	if err := json.Unmarshal(rawConfig, &profile); err != nil {
		t.Fatal(err)
	}
	profile[config.KeyTierReflexModel] = updated
	profile[config.KeyModelRoles] = strings.ReplaceAll(profile[config.KeyModelRoles].(string), primary, updated)
	changedConfig, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath+".updated", changedConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(configPath+".updated", configPath); err != nil {
		t.Fatal(err)
	}
	newRule := "For the fictional Juniper demo, our standing label convention is silver text on auburn labels. Reply only with a brief acknowledgment and call no tools; background memory handles this."
	r.lit(newRule)
	r.keys("Enter")
	if answer, tools := privateMemoryAwaitAnswer(home, newRule, 2*time.Minute); answer == "" {
		t.Fatal("hot-settings chat did not finish")
	} else {
		for _, tool := range tools {
			if strings.Contains(tool, "remember") {
				t.Fatal("foreground tool bypassed hot-settings background capture")
			}
		}
	}
	deadline = time.Now().Add(90 * time.Second)
	for {
		rows, err := st.MemoriesFor([]string{store.OwnerProject(project), store.OwnerUser})
		if err != nil {
			t.Fatal(err)
		}
		saved := false
		for _, m := range rows {
			if m.Status == store.MemoryActive && m.Origin == store.OriginNoticed && strings.Contains(strings.ToLower(m.Text), "auburn") {
				saved = true
			}
		}
		mu.Lock()
		changedAttempt := rejected[updated] > 0
		mu.Unlock()
		if saved && changedAttempt {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("external profile edit not recovered: saved=%t new helper attempted=%t", saved, changedAttempt)
		}
		time.Sleep(pollEvery)
	}
	if beforePID == "" || upgradePID(upgradeEngineCommand(t, binary(t), stateEnv, home, ws, "--status")) != beforePID {
		t.Fatal("external settings recovery restarted the engine")
	}
	r.lit("/new")
	r.keys("Enter")
	time.Sleep(time.Second)
	recall := "For the fictional Juniper demo, which envelopes do we use for weekend parcels? Answer from saved memory, without tools."
	r.lit(recall)
	r.keys("Enter")
	recalled, _ := privateMemoryAwaitAnswer(home, recall, 2*time.Minute)
	if !strings.Contains(strings.ToLower(recalled), "violet") {
		t.Fatalf("fresh chat failed recall: %s", recalled)
	}
	if strings.Contains(r.capture(), "couldn't save this exchange") {
		t.Fatal("recovered memory displayed terminal failure")
	}
	correction := "For the fictional Juniper demo, our standing weekend-parcel packaging convention is now copper envelopes, replacing violet envelopes. Reply with only a one-sentence acknowledgment. Do not call tools, including remember or forget; background memory handles this independently."
	r.lit(correction)
	r.keys("Enter")
	completed, correctionTools := privateMemoryAwaitAnswer(home, correction, 2*time.Minute)
	if completed == "" {
		t.Fatal("correction did not finish")
	}
	for _, tool := range correctionTools {
		if strings.Contains(tool, "remember") || strings.Contains(tool, "forget") {
			t.Fatal("main turn bypassed background correction")
		}
	}
	deadline = time.Now().Add(90 * time.Second)
	for {
		rows, err := st.MemoriesFor([]string{store.OwnerProject(project), store.OwnerUser})
		if err != nil {
			t.Fatal(err)
		}
		copper, violet := 0, 0
		for _, m := range rows {
			if m.Status != store.MemoryActive {
				continue
			}
			text := strings.ToLower(m.Text)
			if strings.Contains(text, "weekend") {
				if strings.Contains(text, "copper") {
					if m.Origin != store.OriginNoticed {
						t.Fatalf("correction bypassed background noticing: %s", m.Origin)
					}
					copper++
				}
				if strings.Contains(text, "violet") && !strings.Contains(text, "copper") {
					violet++
				}
			}
		}
		if copper == 1 && violet == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("correction not applied: copper=%d old=%d", copper, violet)
		}
		time.Sleep(pollEvery)
	}
	r.lit("/new")
	r.keys("Enter")
	time.Sleep(time.Second)
	apply := "For the fictional Juniper demo, use the write or bash tool to create packaging.json in this project with a weekend_envelope_color field set from our current convention, then read the actual file to verify it exists. Use saved memory and do not ask me to repeat it. Do not merely display JSON in your reply."
	r.lit(apply)
	r.keys("Enter")
	if completed, _ := privateMemoryAwaitAnswer(home, apply, 2*time.Minute); completed == "" {
		t.Fatal("application did not finish")
	}
	body, err := os.ReadFile(filepath.Join(ws, "packaging.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packaging struct {
		Color string `json:"weekend_envelope_color"`
	}
	if err := json.Unmarshal(body, &packaging); err != nil || !strings.EqualFold(packaging.Color, "copper") {
		t.Fatalf("saved correction not applied: %s (%v)", body, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if rejected[primary] == 0 || rejected[low] == 0 || forwarded == 0 {
		t.Fatalf("local primary rejections=%d local fallback rejections=%d live memory requests=%d", rejected[primary], rejected[low], forwarded)
	}

	if len(observed) == 0 {
		t.Fatal("no requests observed")
	}
	for _, m := range observed {
		if m != model {
			t.Fatalf("unexpected actual model %q", m)
		}
	}
	models := callModels(t, home)
	if len(models) == 0 {
		t.Fatal("no model receipts")
	}
	for _, m := range models {
		if m != model && m != primary && m != low && m != updated {
			t.Fatalf("unexpected receipt model %q", m)
		}
	}
	rawReceipts, err := os.ReadFile(filepath.Join(home, "logs", "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	successful := 0
	for _, line := range strings.Split(string(rawReceipts), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var receipt calllog.Record
		if err := json.Unmarshal([]byte(line), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Status >= 200 && receipt.Status < 300 {
			successful++
			if receipt.Model != model {
				t.Fatalf("successful receipt used unexpected model %q", receipt.Model)
			}
		}
		if (receipt.Model == primary || receipt.Model == low || receipt.Model == updated) && receipt.Status != 0 && receipt.Status != http.StatusNotFound && !(receipt.Model == low && receipt.Status == http.StatusBadRequest) {
			t.Fatalf("locally rejected fixture got unexpected status %d", receipt.Status)
		}
	}
	if successful == 0 {
		t.Fatal("no successful real-model receipt")
	}

	t.Logf("PASS: two locally rejected helper models recovered in background; external profile edit recovered without restart; one saved convention recalled, corrected, and applied to an actual file across fresh chats; %d actual OpenRouter requests used %s; %d total receipts include locally injected failures", len(observed), model, len(models))
	t.Logf("Live terminal after fresh-chat recall:\n%s", r.capture())
}
