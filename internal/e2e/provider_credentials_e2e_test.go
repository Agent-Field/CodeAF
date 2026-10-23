//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

const credentialFixtureModel = "stub/credential-chat"
const credentialFixtureVideo = "stub/credential-video"
const credentialFixtureKey = "fixture-provider-key-not-a-secret"

// These are scripted-provider acceptance tests, not live-model quality tests.
// Only fake keys and loopback origins are used, through the shipped binary.
func TestProviderCredentialsStayOnOriginOnTheRealBinary(t *testing.T) {
	for _, same := range []bool{false, true} {
		name := "foreign_origin_refused"
		if same {
			name = "same_origin_307_keeps_auth"
		}
		t.Run(name, func(t *testing.T) {
			var targetCalls, authorized atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetCalls.Add(1)
				if r.Header.Get("Authorization") != "" {
					authorized.Add(1)
				}
				credentialTextResponse(w, r, "CREDENTIAL_SAME_ORIGIN_OK")
			}))
			defer target.Close()
			mux := http.NewServeMux()
			credentialCatalog(mux)
			mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
				location := target.URL + "/completion"
				if same {
					location = "/redirected-completion"
				}
				http.Redirect(w, r, location, http.StatusTemporaryRedirect)
			})
			mux.HandleFunc("/redirected-completion", func(w http.ResponseWriter, r *http.Request) {
				targetCalls.Add(1)
				if r.Header.Get("Authorization") == "Bearer "+credentialFixtureKey {
					authorized.Add(1)
				}
				credentialTextResponse(w, r, "CREDENTIAL_SAME_ORIGIN_OK")
			})
			source := httptest.NewServer(mux)
			defer source.Close()
			home := credentialFixtureHome(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := guardedCommand(t, ctx, home, credentialFixtureEnv(home, source.URL), binary(t), "chat", "--no-host", "--one-model", "--model", credentialFixtureModel, "--once", "Say CREDENTIAL_SAME_ORIGIN_OK")
			out, err := cmd.CombinedOutput()
			if same {
				if err != nil || !strings.Contains(string(out), "CREDENTIAL_SAME_ORIGIN_OK") || targetCalls.Load() == 0 || targetCalls.Load() != authorized.Load() {
					t.Fatalf("same-origin307 failed: err=%v requests=%d bearer=%d output=%s", err, targetCalls.Load(), authorized.Load(), out)
				}
			} else {
				host, _ := url.Parse(target.URL)
				if err == nil || targetCalls.Load() != 0 || authorized.Load() != 0 || !strings.Contains(string(out), host.Host) {
					t.Fatalf("foreign origin not honestly refused: err=%v requests=%d bearer=%d output=%s", err, targetCalls.Load(), authorized.Load(), out)
				}
			}
			t.Logf("PASS scripted real binary: same_origin=%t target_requests=%d credential_requests=%d output=%s", same, targetCalls.Load(), authorized.Load(), out)
		})
	}
}

func TestVideoPollingCredentialsOnTheRealTerminal(t *testing.T) {
	for _, same := range []bool{false, true} {
		name := "foreign_poll_refused"
		if same {
			name = "same_origin_poll_and_download"
		}
		t.Run(name, func(t *testing.T) {
			var foreignCalls, foreignAuth, pollAuth, downloadAuth, issued, loaded atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				foreignCalls.Add(1)
				if r.Header.Get("Authorization") != "" {
					foreignAuth.Add(1)
				}
				http.Error(w, "fixture foreign polling endpoint needs its own credential", http.StatusUnauthorized)
			}))
			defer target.Close()
			mux := http.NewServeMux()
			credentialCatalog(mux)
			var source *httptest.Server
			mux.HandleFunc("/videos", func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+credentialFixtureKey {
					t.Error("video submission lost fixture bearer")
				}
				poll := target.URL + "/poll"
				if same {
					poll = source.URL + "/poll"
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"credential-video-job","status":"queued","polling_url":%q}`, poll)
			})
			mux.HandleFunc("/poll", func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "Bearer "+credentialFixtureKey {
					pollAuth.Add(1)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"credential-video-job","status":"completed","unsigned_urls":[%q]}`, source.URL+"/video-content")
			})
			const videoBytes = "SCRIPTED_VIDEO_CONTENT_NOT_A_REAL_MODEL_RENDER"
			mux.HandleFunc("/video-content", func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "Bearer "+credentialFixtureKey {
					downloadAuth.Add(1)
				}
				w.Header().Set("Content-Type", "video/mp4")
				fmt.Fprint(w, videoBytes)
			})
			mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Stream   bool `json:"stream"`
					Messages []struct {
						Role    string          `json:"role"`
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
					Tools []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				offer, canLoad := false, false
				for _, tool := range body.Tools {
					if tool.Function.Name == "generate_video" {
						offer = true
					}
					if tool.Function.Name == "load_capability" {
						canLoad = true
					}
				}
				// Relay the provider failure the real runtime delivered. The
				// expected host is never manufactured by this scripted model.
				for _, message := range body.Messages {
					if message.Role != "user" {
						continue
					}
					note := credentialMessageText(message.Content)
					for _, line := range strings.Split(note, "\n") {
						if strings.Contains(line, "video polling at ") && strings.Contains(line, "failed:") {
							t.Logf("Actual runtime failure note: %s", line)
							credentialTextResponseStream(w, line, body.Stream)
							return
						}
					}
				}
				asked := false
				for _, m := range body.Messages {
					if strings.Contains(string(m.Content), "CREDENTIAL_VIDEO_PROBE") {
						asked = true
					}
				}
				if asked && !offer && canLoad && loaded.CompareAndSwap(0, 1) {
					credentialToolResponse(w, "load_capability", `{"group":"media"}`, body.Stream)
					return
				}
				if offer && asked && issued.CompareAndSwap(0, 1) {
					credentialToolResponse(w, "generate_video", `{"prompt":"a fictional amber lighthouse","path":"credential-proof.mp4"}`, body.Stream)
					return
				}
				credentialTextResponseStream(w, "SCRIPTED_RENDER_SUBMITTED", body.Stream)
			})
			source = httptest.NewServer(mux)
			defer source.Close()
			home := credentialFixtureHome(t)
			ws := newWorkspace(t, "credential-video", false)
			env := []string{config.APIKeyEnv + "=" + credentialFixtureKey, "CODEAF_BASE_URL=" + source.URL, "CODEAF_PROFILE_DIR="}
			stopRecoveryEngineOnCleanup(t, env, home, ws)
			r := startWithEnv(t, env, "credential_video_"+name, home, ws, 170, 46, "chat", "--model", credentialFixtureModel)
			r.skipSetup(t)
			r.lit("CREDENTIAL_VIDEO_PROBE generate a video of an amber lighthouse and save credential-proof.mp4")
			r.keys("Enter")
			deadline := time.Now().Add(30 * time.Second)
			if same {
				for {
					raw, err := os.ReadFile(filepath.Join(ws, "credential-proof.mp4"))
					if err == nil && string(raw) == videoBytes {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("same-origin video not saved: %v screen=%s", err, r.capture())
					}
					time.Sleep(pollEvery)
				}
				if pollAuth.Load() == 0 || downloadAuth.Load() == 0 {
					t.Fatalf("same-origin auth missing poll=%d download=%d", pollAuth.Load(), downloadAuth.Load())
				}
			} else {
				host, _ := url.Parse(target.URL)
				screen := r.waitFor(30*time.Second, host.Host)
				if !strings.Contains(screen, host.Host) {
					t.Fatal("video failure did not name refused host")
				}
				if _, err := os.Stat(filepath.Join(ws, "credential-proof.mp4")); err == nil {
					t.Fatal("refused video unexpectedly wrote output")
				}
			}
			expectedForeign := int64(1)
			if same {
				expectedForeign = 0
			}
			if issued.Load() != 1 || foreignCalls.Load() != expectedForeign || foreignAuth.Load() != 0 {
				t.Fatalf("tool=%d foreign requests=%d bearer=%d", issued.Load(), foreignCalls.Load(), foreignAuth.Load())
			}
			t.Logf("PASS scripted real terminal: same_origin=%t generate_video=%d poll_bearer=%d download_bearer=%d foreign_requests=%d foreign_bearer=%d\n%s", same, issued.Load(), pollAuth.Load(), downloadAuth.Load(), foreignCalls.Load(), foreignAuth.Load(), r.capture())
		})
	}
}

func credentialFixtureHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(shortRoot(t), "h")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	rows := map[string]any{"api_key": credentialFixtureKey, "model.talk": credentialFixtureModel, "models.tiers.low": credentialFixtureModel, "models.tiers.reflex": credentialFixtureModel, "models.tiers.worker": credentialFixtureModel, "models.tiers.high": credentialFixtureModel, "models.tiers.mastermind": credentialFixtureModel, "memory.enabled": false, "tools.approvalMode": "allow", "setup_seen_at": time.Now().UTC().Format(time.RFC3339), config.KeyIcons: config.IconsPlain, config.ModelSettingKey("video"): credentialFixtureVideo}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}
func credentialFixtureEnv(home, base string) []string {
	return append(os.Environ(), config.APIKeyEnv+"="+credentialFixtureKey, "CODEAF_BASE_URL="+base, "CODEAF_HOME="+home, "CODEAF_PROFILE_DIR=")
}
func credentialCatalog(mux *http.ServeMux) {
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"id":%q,"name":"Scripted text fixture","context_length":200000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools","tool_choice"]},{"id":%q,"name":"Scripted video fixture","architecture":{"input_modalities":["text"],"output_modalities":["video"]},"pricing":{"prompt":"0","completion":"0"}}]}`, credentialFixtureModel, credentialFixtureVideo)
	})
}
func credentialTextResponse(w http.ResponseWriter, r *http.Request, text string) {
	stream := false
	if r != nil {
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		stream = body.Stream
	}
	credentialTextResponseStream(w, text, stream)
}
func credentialTextResponseStream(w http.ResponseWriter, text string, stream bool) {
	message := map[string]any{"role": "assistant", "content": text}
	credentialWireResponse(w, message, "stop", stream)
}
func credentialToolResponse(w http.ResponseWriter, name, args string, stream bool) {
	message := map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "credential-" + name + "-call", "type": "function", "function": map[string]string{"name": name, "arguments": args}}}}
	credentialWireResponse(w, message, "tool_calls", stream)
}
func credentialWireResponse(w http.ResponseWriter, message map[string]any, finish string, stream bool) {
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []map[string]any{{"index": 0, "delta": message, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			payload := map[string]any{"id": "credential-fixture", "model": credentialFixtureModel, "choices": []any{choice}, "usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}}
			raw, _ := json.Marshal(payload)
			fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "credential-fixture", "model": credentialFixtureModel, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
}

// A separate, authenticated OpenRouter regression never talks to the fault
// fixtures above. Every text role is explicitly pinned to the requested model.
func TestProviderCredentialBoundaryLiveConversationRegression(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
	}
	home := recoveryShortHome(t, newHome(t, map[string]any{"memory.enabled": true, "models.crew.free_routes": false}))
	ws := newWorkspace(t, "credential-live-regression", false)
	project, err := gitidentity.ProjectKey(ws)
	if err != nil {
		t.Fatal(err)
	}
	env := []string{config.APIKeyEnv + "=" + liveKey(t), "CODEAF_BASE_URL=" + config.DefaultBaseURL, "CODEAF_PROFILE_DIR="}
	stopRecoveryEngineOnCleanup(t, env, home, ws)
	r := startWithEnv(t, env, "credential_live_regression", home, ws, 160, 46, "chat", "--model", model, "--one-model")
	r.skipSetup(t)
	first := "For the fictional Cedar demo, our standing envelope convention is copper envelopes for weekend parcels. Acknowledge in one sentence without tools."
	r.lit(first)
	r.keys("Enter")
	if answer, tools := privateMemoryAwaitAnswer(home, first, 2*time.Minute); answer == "" {
		t.Fatal("live conversation did not complete")
	} else {
		for _, tool := range tools {
			if strings.Contains(tool, "remember") {
				t.Fatal("foreground tool bypassed automatic capture")
			}
		}
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		raw, _ := os.ReadFile(filepath.Join(home, "logs", "calls.jsonl"))
		if strings.Contains(string(raw), `"tag":"reflex"`) {
			// Wait for the persistent memory rather than just the start receipt.
			st, err := store.Open(filepath.Join(home, "graph.db"))
			if err == nil {
				rows, _ := st.MemoriesFor([]string{store.OwnerUser, store.OwnerProject(project)})
				_ = st.Close()
				kept := false
				for _, m := range rows {
					if m.Status == store.MemoryActive && m.Origin == store.OriginNoticed && strings.Contains(strings.ToLower(m.Text), "copper") {
						kept = true
					}
				}
				if kept {
					break
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic memory capture did not complete")
		}
		time.Sleep(pollEvery)
	}
	r.lit("/new")
	r.keys("Enter")
	time.Sleep(time.Second)
	apply := "For the fictional Cedar demo, use write or bash to create envelope.json with a weekend_envelope_color field from our saved convention, then read the actual file to verify it exists. Do not merely show JSON in your reply."
	r.lit(apply)
	r.keys("Enter")
	if answer, _ := privateMemoryAwaitAnswer(home, apply, 2*time.Minute); answer == "" {
		t.Fatal("live saved-memory application did not finish")
	}
	raw, err := os.ReadFile(filepath.Join(ws, "envelope.json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Color string `json:"weekend_envelope_color"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || !strings.EqualFold(envelope.Color, "copper") {
		t.Fatalf("live recalled convention not applied: %s %v", raw, err)
	}
	models := callModels(t, home)
	if len(models) == 0 {
		t.Fatal("no actual model receipts")
	}
	for _, m := range models {
		if m != model {
			t.Fatalf("unexpected actual model %q", m)
		}
	}
	t.Logf("PASS actual OpenRouter: normal hosted chat automatically captured mock convention, fresh chat recalled and wrote actual JSON file; %d receipt rows all used %s\n%s", len(models), model, r.capture())
}

func credentialMessageText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var texts []string
	for _, part := range parts {
		texts = append(texts, part.Text)
	}
	return strings.Join(texts, "\n")
}
