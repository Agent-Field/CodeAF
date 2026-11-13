package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestChatOnceEnvelopeAccountsForOnlyThisInvocation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("failure=%t", fail), func(t *testing.T) {
			appDayProfile(t)
			freshEngineProcess(t)
			workspace := t.TempDir()
			if err := os.WriteFile(filepath.Join(workspace, "receipt.txt"), []byte("paid\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var calls, work atomic.Int64
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
					http.Error(w, "no catalog", 503)
					return
				}
				raw, _ := io.ReadAll(r.Body)
				var req recordedRequest
				_ = json.Unmarshal(raw, &req)
				n := int64(0)
				if len(req.Tools) > 0 {
					n = work.Add(1)
				}
				if fail && n > 1 {
					http.Error(w, "scripted provider failure", 400)
					return
				}
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if fail && n == 1 {
					fmt.Fprint(w, `data: {"id":"paid","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"read-1","type":"function","function":{"name":"read","arguments":"{\"path\":\"receipt.txt\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"cost":0.125}}`+"\n\ndata: [DONE]\n\n")
					return
				}
				fmt.Fprint(w, `data: {"id":"paid","choices":[{"index":0,"delta":{"role":"assistant","content":"The answer is ready."},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"cost":0.125}}`+"\n\ndata: [DONE]\n\n")
			}))
			t.Cleanup(model.Close)
			file, err := appMint(workspace)
			if err != nil {
				t.Fatal(err)
			}
			cfg := session.Config{Workspace: workspace, Place: session.Place{Dir: session.DirOf(file)}, SessionFile: file, Model: "openai/gpt-4.1-mini", Sources: modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(model.URL), Key: "test-key", Address: model.URL})}
			prior := chatOnceLive
			chatOnceLive = onceLive{json: true}
			t.Cleanup(func() { chatOnceLive = prior })
			invocations := 2
			if fail {
				invocations = 1
			}
			for i := 0; i < invocations; i++ {
				before := calls.Load()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				out, err := captureStdout(t, func() error { return runChatV3Once(ctx, cfg, workspace, "read the receipt and answer", "", i > 0) })
				cancel()
				if (err != nil) != fail {
					t.Fatalf("failure=%t: %v; stdout=%s", fail, err, out)
				}
				var got struct {
					OK     bool            `json:"ok"`
					Stop   string          `json:"stop"`
					Answer string          `json:"answer"`
					Error  string          `json:"error"`
					Tokens *envelopeTokens `json:"tokens"`
					Spend  *float64        `json:"spend_usd"`
				}
				if err := json.Unmarshal([]byte(out), &got); err != nil {
					t.Fatal(err)
				}
				n := calls.Load() - before
				if got.Tokens == nil || got.Spend == nil {
					t.Fatalf("watched envelope omits exec accounting: %s", out)
				}
				if n == 0 || got.Tokens.In != int(n)*100 || got.Tokens.Out != int(n)*20 || *got.Spend != float64(n)*0.125 {
					t.Fatalf("invocation %d accounting=%+v spend=%v; %d paid calls", i+1, got.Tokens, *got.Spend, n)
				}
				if fail {
					if got.OK || got.Stop != "error" || got.Error == "" {
						t.Fatalf("failure envelope: %s", out)
					}
				} else if !got.OK || got.Stop != "done" || got.Answer != "The answer is ready." || got.Error != "" {
					t.Fatalf("success envelope: %s", out)
				}
			}
		})
	}
}
