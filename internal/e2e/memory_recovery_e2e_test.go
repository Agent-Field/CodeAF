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

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

// The terminal answers normally while two injected memory stalls exhaust one
// noticing pass. Its later recovery must save the ORIGINAL exchange, and a new
// chat must recall it through the real model rather than a synthetic answer.
func TestMemoryRecoversFromTimeoutOnTheRealTerminal(t *testing.T) {
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
	stalled, forwarded := 0, 0
	var observed []string
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
			observed = append(observed, request.Model)
			if isMemory && stalled < 2 {
				stalled++
				mu.Unlock()
				// Deliberately withhold all response bytes until the app's own attempt
				// deadline cancels the request. No fabricated model response is supplied.
				<-r.Context().Done()
				return
			}
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
	home := newHome(t, map[string]any{"memory.enabled": true})
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
	r := startWithEnv(t, []string{config.APIKeyEnv + "=" + liveKey(t), "CODEAF_BASE_URL=" + server.URL + "/fixture"}, "memory_recovery", home, ws, 150, 45, "chat", "--one-model", "--debug")
	r.skipSetup(t)
	prompt := "For the fictional Juniper demo, our standing packaging convention is violet envelopes for weekend parcels. Please acknowledge in one sentence without calling any tools."
	r.lit(prompt)
	r.keys("Enter")
	answer, tools := privateMemoryAwaitAnswer(home, prompt, 2*time.Minute)
	if answer == "" {
		t.Fatal("chat did not finish during background memory outage")
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
	correction := "For the fictional Juniper demo, change our standing weekend-parcel packaging convention from violet envelopes to copper envelopes. This replaces the earlier convention. Acknowledge briefly without tools."
	r.lit(correction)
	r.keys("Enter")
	if completed, _ := privateMemoryAwaitAnswer(home, correction, 2*time.Minute); completed == "" {
		t.Fatal("correction did not finish")
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
	apply := "For the fictional Juniper demo, create packaging.json with a weekend_envelope_color field set from our current convention. Use saved memory and do not ask me to repeat it."
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
	if stalled != 2 || forwarded == 0 {
		t.Fatalf("stalled=%d live memory requests=%d", stalled, forwarded)
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
		if m != model {
			t.Fatalf("unexpected receipt model %q", m)
		}
	}
	t.Logf("PASS: two stalled memory requests recovered in background; one saved convention recalled, corrected, and applied to an actual file across fresh chats; %d requests and %d receipts used %s", len(observed), len(models), model)
	t.Logf("Live terminal after fresh-chat recall:\n%s", r.capture())
}
