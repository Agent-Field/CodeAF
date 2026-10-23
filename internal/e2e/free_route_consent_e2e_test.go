//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
)

// This scripted-provider test uses the real command and a known-low account
// record. No real credentials or model calls enter either fixture endpoint.
func TestLowBalanceFreeRoutesRequireConsentOnTheRealBinary(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		name := "off_keeps_context_off_free_routes"
		if allowed {
			name = "opt_in_allows_free_default"
		}
		t.Run(name, func(t *testing.T) {
			const key = "fixture-free-consent-key-not-a-secret"
			var mu sync.Mutex
			var sent []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/models":
					var rows []map[string]any
					for _, model := range []string{strings.TrimPrefix(config.DefaultModel, "~"), config.DefaultLowModel, config.DefaultReflexModel, config.FreeChatModel, "nvidia/nemotron-3.5-lightning:free", "thinkingmachines/inkling-small:free"} {
						price := "0.000001"
						if strings.HasSuffix(model, ":free") {
							price = "0"
						}
						rows = append(rows, map[string]any{"id": model, "canonical_slug": model, "name": model, "context_length": 200000, "architecture": map[string]any{"input_modalities": []string{"text"}, "output_modalities": []string{"text"}}, "pricing": map[string]string{"prompt": price, "completion": price}, "supported_parameters": []string{"tools", "tool_choice"}})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": rows})
				case "/key", "/auth/key":
					fmt.Fprint(w, `{"data":{"limit":1,"usage":0.75,"limit_remaining":0.25}}`)
				case "/credits":
					fmt.Fprint(w, `{"data":{"total_credits":1,"total_usage":0.75}}`)
				case "/chat/completions":
					var body struct {
						Stream bool   `json:"stream"`
						Model  string `json:"model"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						http.Error(w, "fixture decode", 400)
						return
					}
					mu.Lock()
					sent = append(sent, body.Model)
					mu.Unlock()

					freeConsentResponse(w, body.Model, "FREE_CONSENT_CONTROL_OK", nil, body.Stream)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			home := emptyHome(t)
			profile := map[string]any{"api_key": key, "memory.enabled": false, "tools.approvalMode": "allow", "models.crew.free_routes": allowed, "setup_seen_at": time.Now().UTC().Format(time.RFC3339)}
			raw, _ := json.Marshal(profile)
			if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := config.WriteCreditsReading(home, key, credits.Reading{Known: true, Low: true}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			env := append(os.Environ(), config.APIKeyEnv+"="+key, "CODEAF_BASE_URL="+server.URL, "CODEAF_HOME="+home, "CODEAF_PROFILE_DIR=")
			cmd := guardedCommand(t, ctx, home, env, binary(t), "chat", "--no-host", "--once", "Reply with FREE_CONSENT_CONTROL_OK")
			out, err := cmd.CombinedOutput()
			mu.Lock()
			defer mu.Unlock()
			free := 0
			for _, model := range sent {
				if strings.HasSuffix(model, ":free") {
					free++
				}
			}
			if !allowed {
				if free != 0 {
					t.Fatalf("off sent context to free models: %v", sent)
				}
				if err != nil || len(sent) == 0 || !strings.Contains(string(out), "FREE_CONSENT_CONTROL_OK") {
					t.Fatalf("known low positive balance should retain usable paid route: err=%v models=%v output=%s", err, sent, out)
				}
			} else if err != nil || free == 0 || !strings.Contains(string(out), "FREE_CONSENT_CONTROL_OK") {
				t.Fatalf("opt-in positive control failed: err=%v models=%v output=%s", err, sent, out)
			}
			t.Logf("PASS scripted real binary: automatic_free_consent=%t free_requests=%d models=%v output=%s", allowed, free, sent, out)
		})
	}
}

// Consent is changed only inside the paid request handler in the positive
// control: routing must first choose a paid worker, then receive a 402, before
// any free request can be admitted. Explicit seat pins are deliberately absent.
func TestAutomaticTask402FreeFallbackRequiresConsentOnTheRealTerminal(t *testing.T) {
	for _, enableOn402 := range []bool{false, true} {
		name := "off_refuses_free_fallback"
		if enableOn402 {
			name = "consent_before_402_allows_fallback"
		}
		t.Run(name, func(t *testing.T) {
			const key = "fixture-task-free-key-not-a-secret"
			const manager = "stub/free-consent-manager"
			const paid = "z-ai/glm-5.3-flash"
			const free = paid + ":free"
			home := filepath.Join(shortRoot(t), "h")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatal(err)
			}
			profile := map[string]any{"api_key": key, "model.talk": manager, "models.tiers.low": manager, "models.tiers.reflex": manager, "models.crew.free_routes": false, "memory.enabled": false, "tools.approvalMode": "allow", "task.audit": "off", "task.max_load": 0, "task.parallel": 1, "setup_seen_at": time.Now().UTC().Format(time.RFC3339), config.KeyIcons: config.IconsPlain}
			saveProfile := func() error {
				raw, err := json.Marshal(profile)
				if err != nil {
					return err
				}
				path := filepath.Join(home, "config.json")
				if err := os.WriteFile(path+".fixture", raw, 0o600); err != nil {
					return err
				}
				return os.Rename(path+".fixture", path)
			}
			if err := saveProfile(); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			paidRequests, freeRequests := 0, 0
			wrote := false
			var models []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/models":
					var rows []map[string]any
					for _, model := range []string{manager, paid, free} {
						price := "0.00000015"
						if model == free {
							price = "0"
						}
						rows = append(rows, map[string]any{"id": model, "canonical_slug": model, "name": model, "hugging_face_id": "fixture/glm", "benchmarks": map[string]any{"artificial_analysis": map[string]float64{"intelligence_index": 41.8, "coding_index": 71.5, "agentic_index": 50.9}}, "context_length": 1310720, "architecture": map[string]any{"input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "instruct_type": "chat", "modality": "text->text"}, "pricing": map[string]string{"prompt": price, "completion": price}, "supported_parameters": []string{"tools", "tool_choice", "max_tokens"}})
						if model == manager {
							delete(rows[len(rows)-1], "benchmarks")
							delete(rows[len(rows)-1], "hugging_face_id")
						}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": rows})
				case "/key", "/auth/key":
					fmt.Fprint(w, `{"data":{"limit":100,"usage":0,"limit_remaining":100}}`)
				case "/credits":
					fmt.Fprint(w, `{"data":{"total_credits":100,"total_usage":0}}`)
				case "/chat/completions":
					var body struct {
						Stream   bool   `json:"stream"`
						Model    string `json:"model"`
						Messages []struct {
							Role string `json:"role"`
						} `json:"messages"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						http.Error(w, "fixturedecode", 400)
						return
					}
					mu.Lock()
					models = append(models, body.Model)
					if body.Model == paid {
						paidRequests++
						if enableOn402 && paidRequests == 1 {
							profile["models.crew.free_routes"] = true
							if err := saveProfile(); err != nil {
								t.Error(err)
							}
						}
						mu.Unlock()
						w.WriteHeader(http.StatusPaymentRequired)
						fmt.Fprint(w, `{"error":{"message":"Insufficient credits","code":402}}`)
						return
					}
					if strings.HasSuffix(body.Model, ":free") {
						freeRequests++
						if !wrote {
							wrote = true
							mu.Unlock()
							args := `{"command":"printf FREE_TASK_FALLBACK_OK > free-consent-proof.txt"}`
							freeConsentResponse(w, body.Model, "", map[string]any{"index": 0, "id": "free-task-write", "type": "function", "function": map[string]string{"name": "bash", "arguments": args}}, body.Stream)
							return
						}
					}
					mu.Unlock()
					freeConsentResponse(w, body.Model, "The scripted task completed.", nil, body.Stream)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			ws := newWorkspace(t, "free-consent-task", false)
			env := []string{config.APIKeyEnv + "=" + key, "CODEAF_BASE_URL=" + server.URL, "CODEAF_PROFILE_DIR="}
			stopRecoveryEngineOnCleanup(t, env, home, ws)
			rig := startWithEnv(t, env, "free_402_"+name, home, ws, 170, 48, "chat", "--model", manager)
			rig.skipSetup(t)
			rig.lit("/task fix: the parser crashes on empty input; write free-consent-proof.txt containing FREE_TASK_FALLBACK_OK")
			rig.keys("Enter")
			deadline := time.Now().Add(45 * time.Second)
			for {
				mu.Lock()
				paidN, freeN := paidRequests, freeRequests
				mu.Unlock()
				if !enableOn402 && paidN > 0 && strings.Contains(strings.ToLower(rig.capture()), "credit") {
					break
				}
				if enableOn402 && paidN > 0 && freeN > 0 && (freeConsentFileExists(home) || freeConsentFileExists(ws)) && strings.Contains(rig.capture(), say(t, "taskDoneWord")) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("automatic402 acceptance incomplete: on=%t paid=%d free=%d screen=%s", enableOn402, paidN, freeN, rig.capture())
				}
				time.Sleep(pollEvery)
			}
			mu.Lock()
			defer mu.Unlock()
			if paidRequests == 0 || (!enableOn402 && freeRequests != 0) || (enableOn402 && freeRequests == 0) {
				t.Fatalf("wrong consent behavior: on=%t paid=%d free=%d models=%v", enableOn402, paidRequests, freeRequests, models)
			}
			t.Logf("PASS scripted real terminal: consent_at_402=%t paid_requests=%d free_requests=%d models=%v\n%s", enableOn402, paidRequests, freeRequests, models, rig.capture())
		})
	}
}
func freeConsentResponse(w http.ResponseWriter, model, text string, call any, stream bool) {
	message := map[string]any{"role": "assistant", "content": text}
	finish := "stop"
	if call != nil {
		message["tool_calls"] = []any{call}
		finish = "tool_calls"
	}
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []map[string]any{{"index": 0, "delta": message, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			raw, _ := json.Marshal(map[string]any{"id": "free-task-fixture", "model": model, "choices": []any{choice}, "usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
			fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "free-task-fixture", "model": model, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
}
func freeConsentFileExists(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && entry.Name() == "free-consent-proof.txt" {
			raw, _ := os.ReadFile(path)
			found = string(raw) == "FREE_TASK_FALLBACK_OK"
			if found {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}
