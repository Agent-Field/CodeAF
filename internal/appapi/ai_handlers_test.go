package appapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

const aiTestRole roles.Role = "app.test"

func init() {
	if err := roles.RegisterApp(aiTestRole, roles.TierLow); err != nil {
		panic(err)
	}
}

const aiTestBody = `{"role":"app.test","messages":[{"role":"user","content":"hello"}],"max_tokens":8,"budget":{"usd":"0.05"},"stream":false}`

func aiTestServer(t *testing.T, handler http.HandlerFunc) (*Server, *atomic.Int32, string) {
	t.Helper()
	calls := new(atomic.Int32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); handler(w, r) }))
	t.Cleanup(upstream.Close)
	home := t.TempDir()
	now := time.Now()
	runtime := AICallRuntime{Default: "test/model", ClientConfig: func(model string) provider.Config {
		return provider.Config{APIKey: "test-ai-key", BaseURL: upstream.URL, Model: model, Direct: true, ModelPrice: func(string) (float64, float64, bool) { return .000001, .000002, true }}
	}}
	s := resourceServer(t, home, &now, WithAICallRuntime(context.Background(), runtime))
	return s, calls, home
}
func aiTestAnswer(w http.ResponseWriter, r *http.Request) {
	var request map[string]any
	_ = json.NewDecoder(r.Body).Decode(&request)
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: {\"id\":\"test-generation\",\"model\":\"test/model\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
	fmt.Fprint(w, "data: {\"id\":\"test-generation\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1,\"cost\":0.0012}}\n\ndata: [DONE]\n\n")
}
func TestAICallUsesFunnelAndDurableAppSpending(t *testing.T) {
	s, calls, home := aiTestServer(t, aiTestAnswer)
	w := resourceRequest(s, "POST", "/app/v1/ai/call", aiTestBody, "paid")
	var response AICallResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if response.Text != "hello" || response.Model != "test/model" || response.Usage != (AIUsage{In: 3, Out: 1}) || response.Cost.USD != "0.00120000" || response.LedgerID == "" {
		t.Fatalf("response: %+v", response)
	}
	replay := resourceRequest(s, "POST", "/app/v1/ai/call", aiTestBody, "paid")
	if replay.Body.String() != w.Body.String() || calls.Load() != 1 {
		t.Fatal("replay paid again")
	}
	if changed := resourceRequest(s, "POST", "/app/v1/ai/call", strings.ReplaceAll(aiTestBody, "hello", "changed"), "paid"); changed.Code != 409 {
		t.Fatal(changed.Body.String())
	}
	lines, err := session.ReadUsage(filepath.Join(home, "v3", session.UsageLedgerName), time.Time{})
	if err != nil || len(lines) != 1 || lines[0].ID != response.LedgerID || lines[0].Role != string(aiTestRole) || lines[0].USD != .0012 {
		t.Fatalf("ledger: %+v %v", lines, err)
	}
	rows, err := (nativeSpending{s: s}).SpendingRows(context.Background())
	if err != nil || len(rows) != 1 || rows[0].Who.Kind != "app" || rows[0].Who.Purpose != "test" {
		t.Fatalf("spending: %+v %v", rows, err)
	}
	now := s.resourceNow()
	restart := resourceServer(t, home, &now, nil)
	if rows, err := (nativeSpending{s: restart}).SpendingRows(context.Background()); err != nil || len(rows) != 1 {
		t.Fatalf("restart: %+v %v", rows, err)
	}
}
func TestAICallRefusesUnregisteredRolesAndInvalidInputBeforeTokens(t *testing.T) {
	s, calls, _ := aiTestServer(t, aiTestAnswer)
	bodies := []string{`{}`, `null`, aiTestBody + ` {}`, strings.ReplaceAll(aiTestBody, "app.test", "app.title"), strings.ReplaceAll(aiTestBody, "app.test", "title"), strings.ReplaceAll(aiTestBody, `"max_tokens":8`, `"max_tokens":0`), strings.ReplaceAll(aiTestBody, `"user"`, `"tool"`), strings.ReplaceAll(aiTestBody, `"hello"`, `""`), strings.ReplaceAll(aiTestBody, `"0.05"`, `"0.00"`), strings.ReplaceAll(aiTestBody, `"0.05"`, `"0.005"`), strings.TrimSuffix(aiTestBody, "}") + `,"tier":"high"}`, strings.TrimSuffix(aiTestBody, "}") + `,"model":"app-chosen"}`, strings.TrimSuffix(aiTestBody, "}") + `,"expect":{"version":1}}`, strings.TrimSuffix(aiTestBody, "}") + `,"json_schema":[]}`}
	for _, body := range bodies {
		w := resourceRequest(s, "POST", "/app/v1/ai/call", body, "")
		if w.Code != 422 {
			t.Errorf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input bought tokens")
	}
	req := httptest.NewRequest("POST", "/app/v1/ai/call", strings.NewReader(aiTestBody))
	req.Host = "127.0.0.1:4000"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := resourceRequest(s, "GET", "/app/v1/ai/call", "", ""); w.Code != 405 {
		t.Fatal(w.Code)
	}
}
func TestAICallDayAndPerCallBudgetRefuseBeforeProvider(t *testing.T) {
	for _, mode := range []string{"day", "remaining", "call", "unknown_price", "ledger"} {
		t.Run(mode, func(t *testing.T) {
			s, calls, home := aiTestServer(t, aiTestAnswer)
			body := aiTestBody
			switch mode {
			case "day":
				writeResourceLedger(t, home, []session.UsageLine{{At: s.resourceNow(), USD: 20}})
			case "remaining":
				writeResourceLedger(t, home, []session.UsageLine{{At: s.resourceNow(), USD: 19.99999}})
			case "call":
				body = strings.ReplaceAll(body, "0.05", "0.01")
				runtime := s.options.Context.Value(aiRuntimeKey{}).(AICallRuntime)
				original := runtime.ClientConfig
				runtime.ClientConfig = func(model string) provider.Config {
					cfg := original(model)
					cfg.ModelPrice = func(string) (float64, float64, bool) { return 1, 1, true }
					return cfg
				}
				s.options.Context = WithAICallRuntime(s.options.Context, runtime)
			case "unknown_price":
				runtime := s.options.Context.Value(aiRuntimeKey{}).(AICallRuntime)
				original := runtime.ClientConfig
				runtime.ClientConfig = func(model string) provider.Config { cfg := original(model); cfg.ModelPrice = nil; return cfg }
				s.options.Context = WithAICallRuntime(s.options.Context, runtime)
			case "ledger":
				if err := os.MkdirAll(filepath.Join(home, "v3", session.UsageLedgerName), 0700); err != nil {
					t.Fatal(err)
				}
			}
			w := resourceRequest(s, "POST", "/app/v1/ai/call", body, "")
			want := 402
			if mode == "unknown_price" {
				want = 503
			}
			if mode == "ledger" {
				want = 503
			}
			if w.Code != want || calls.Load() != 0 {
				t.Fatalf("%d %s, calls=%d", w.Code, w.Body.String(), calls.Load())
			}
		})
	}
}
func TestAICallStreamsAndReplaysWithoutMainStreamEvents(t *testing.T) {
	s, calls, _ := aiTestServer(t, aiTestAnswer)
	body := strings.ReplaceAll(aiTestBody, `"stream":false`, `"stream":true`)
	w := resourceRequest(s, "POST", "/app/v1/ai/call", body, "stream")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "event: ai.delta") || !strings.Contains(w.Body.String(), "event: ai.done") || w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	replay := resourceRequest(s, "POST", "/app/v1/ai/call", body, "stream")
	if replay.Body.String() != w.Body.String() || calls.Load() != 1 {
		t.Fatal("stream replay changed or paid again")
	}
}
func TestAICallJSONFormatReachesTheProvider(t *testing.T) {
	s, _, _ := aiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ResponseFormat json.RawMessage `json:"response_format"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || !strings.Contains(string(request.ResponseFormat), "app_response") {
			t.Error("schema lost")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"test/model","choices":[{"message":{"role":"assistant","content":"{\"ok\":true}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"cost":0.0012}}`)
	})
	body := strings.TrimSuffix(aiTestBody, "}") + `,"json_schema":{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}}`
	w := resourceRequest(s, "POST", "/app/v1/ai/call", body, "")
	var result AICallResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || string(result.JSON) != `{"ok":true}` {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestAICallRequiresAReportedCostRatherThanTreatingMissingCostAsFree(t *testing.T) {
	for _, mode := range []string{"no_usage", "token_only", "reported_zero", "recovered", "unrecoverable"} {
		t.Run(mode, func(t *testing.T) {
			s, calls, home := aiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/generation" {
					if r.URL.Query().Get("id") != "cost-missing" {
						t.Error("receipt did not name the original generation")
					}
					if mode == "unrecoverable" {
						w.WriteHeader(404)
						fmt.Fprint(w, `{"error":"Cannot GET /generation"}`)
					} else {
						fmt.Fprint(w, `{"data":{"total_cost":0.0012,"tokens_prompt":3,"tokens_completion":1}}`)
					}
					return
				}
				usage := `,"usage":{"prompt_tokens":3,"completion_tokens":1}`
				if mode == "no_usage" {
					usage = ""
				} else if mode == "reported_zero" {
					usage = `,"usage":{"prompt_tokens":3,"completion_tokens":1,"cost":0}`
				}
				fmt.Fprintf(w, `{"id":"cost-missing","model":"test/model","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]%s}`, usage)
			})
			if mode == "recovered" || mode == "unrecoverable" {
				runtime := s.options.Context.Value(aiRuntimeKey{}).(AICallRuntime)
				original := runtime.ClientConfig
				runtime.ClientConfig = func(model string) provider.Config { cfg := original(model); cfg.Direct = false; return cfg }
				s.options.Context = WithAICallRuntime(s.options.Context, runtime)
			}
			w := resourceRequest(s, "POST", "/app/v1/ai/call", aiTestBody, "")
			want, sends := 502, int32(1)
			if mode == "reported_zero" || mode == "recovered" {
				want = 200
			}
			if mode == "recovered" || mode == "unrecoverable" {
				sends = 2
			}
			if w.Code != want || calls.Load() != sends {
				t.Fatalf("%d %s; sends=%d, want status=%d sends=%d", w.Code, w.Body.String(), calls.Load(), want, sends)
			}
			lines, err := session.ReadUsage(filepath.Join(home, "v3", session.UsageLedgerName), time.Time{})
			if err != nil || len(lines) != 1 || lines[0].Unbilled != (want != 200) || lines[0].Reconciled != (mode == "recovered") {
				t.Fatalf("receipt: %+v %v", lines, err)
			}
			if mode == "recovered" && (lines[0].USD != .0012 || lines[0].Input != 3 || lines[0].Output != 1) {
				t.Fatalf("recovered receipt: %+v", lines[0])
			}
		})
	}
}

// This structural law keeps app tokens on the provider funnel's one call path.
func TestAIResourceHasOneProviderCallDoor(t *testing.T) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "ai_handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "CompleteWithMessages" {
			count++
		}
		return true
	})
	if count != 1 {
		t.Fatalf("AI provider doors=%d, want 1", count)
	}
}

func TestAICallSerializesHomesAndCancelsWithItsCaller(t *testing.T) {
	entered := make(chan struct{})
	ended := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	s, _, home := aiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
		close(ended)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("POST", "/app/v1/ai/call", strings.NewReader(aiTestBody)).WithContext(ctx)
	req.Host = "127.0.0.1:4000"
	req.Header.Set("Authorization", "Bearer test-resource-token")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); s.ServeHTTP(w, req); done <- w }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not start")
	}
	now := s.resourceNow()
	other := resourceServer(t, home, &now, s.options.Context)
	if w := resourceRequest(other, "POST", "/app/v1/ai/call", aiTestBody, ""); w.Code != 409 {
		t.Fatalf("parallel home call: %d %s", w.Code, w.Body.String())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled call stayed running")
	}
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("provider context was not canceled")
	}
}

func TestAICallFlushesDeltaBeforeTheAnswerEnds(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	s, _, _ := aiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"live\",\"choices\":[{\"delta\":{\"content\":\"live delta\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1,\"cost\":0.0012}}\n\ndata: [DONE]\n\n")
	})
	server := httptest.NewServer(s)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/app/v1/ai/call", strings.NewReader(strings.ReplaceAll(aiTestBody, `"stream":false`, `"stream":true`)))
	req.Header.Set("Authorization", "Bearer test-resource-token")
	req.Header.Set("Idempotency-Key", "live")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, "live delta") {
			break
		}
	}
	cancel()
}
