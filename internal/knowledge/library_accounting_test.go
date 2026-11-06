package knowledge_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestLibraryAccountingPublicCalls(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			t.Run(fmt.Sprintf("owned=%t/failed=%t", owned, failed), func(t *testing.T) {
				profile := libraryAccountingProfile(t)
				v := knowledge.Open(profile)
				if err := v.StartBuild(); err != nil {
					t.Fatal(err)
				}
				captureRuntimeSource(t, v, "s")
				var global, owner []string
				knowledge.SetUsageRecorder(func(ctx context.Context, b provider.Billed) {
					if b.Cost != .125 || b.Model != "deepseek/deepseek-v4.1-flash-1017" {
						t.Errorf("receipt: %+v", b)
					}
					global = append(global, knowledge.LibraryActivity(ctx))
				})
				t.Cleanup(func() { knowledge.SetUsageRecorder(nil) })
				if owned {
					v = v.WithCalls(func(ctx context.Context) (context.Context, error) {
						activity := knowledge.LibraryActivity(ctx)
						return provider.WithBilling(ctx, func(b provider.Billed) {
							if b.Cost != .125 {
								t.Errorf("owned receipt: %+v", b)
							}
							owner = append(owner, activity)
						}), nil
					})
				}
				withKnowledgeModel(t, func(p map[string]any) map[string]any {
					if p["messages"] != nil {
						return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "scope": "Cedar", "topic": "Queues", "kind": "fact", "evidence": []any{map[string]any{"source": "s", "quote": "Cedar queues jobs."}}}}}
					}
					if p["question"] != nil {
						claim := p["claims"].([]any)[0].(map[string]any)
						return map[string]any{"answer": claim["text"], "citations": []any{claim["id"]}}
					}
					return map[string]any{"subjects": []any{}}
				})
				fixture := http.DefaultTransport
				calls := 0
				var wireActivities []string
				http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					wireActivities = append(wireActivities, knowledge.LibraryActivity(r.Context()))
					response, err := fixture.RoundTrip(r)
					if err != nil {
						return nil, err
					}
					defer response.Body.Close()
					var body map[string]any
					if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
						return nil, err
					}
					if body["model"] != "deepseek/deepseek-v4.1-flash-1017" {
						t.Errorf("wrong requested model: %v", body["model"])
					}
					body["usage"] = map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "cost": .125}
					if failed {
						body["choices"].([]any)[0].(map[string]any)["finish_reason"] = "length"
					}
					raw, err := json.Marshal(body)
					response.Body = io.NopCloser(strings.NewReader(string(raw)))
					return response, err
				})
				err := v.Flush(context.Background(), "fixture", true)
				if failed {
					if err == nil || !strings.Contains(err.Error(), "length") {
						t.Fatalf("expected paid incomplete response: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				receipts := global
				if owned {
					receipts = owner
					if len(global) != 0 {
						t.Fatalf("owned call also charged globally: %v", global)
					}
				}
				if calls == 0 || len(receipts) != calls {
					t.Fatalf("calls=%d receipts=%v", calls, receipts)
				}
				for _, activity := range receipts {
					if activity != "learning" {
						t.Fatalf("learning attribution: %q", activity)
					}
				}
				if failed {
					return
				}
				// Explicit reads carry their caller's billing context, independently of learning ownership.
				ctx := context.Background()
				if owned {
					ctx = provider.WithBilling(ctx, func(b provider.Billed) { owner = append(owner, "owned answer receipt") })
				}
				before := calls
				answer, err := v.RecallSaved(ctx, "fixture", "What does Cedar queue?", "Cedar")
				if err != nil || len(answer.Citations) != 1 {
					t.Fatalf("recall: %+v %v", answer, err)
				}
				if wireActivities[len(wireActivities)-1] != "answers" {
					t.Fatalf("paid recall attribution: %v", wireActivities)
				}
				if calls != before+1 {
					t.Fatalf("recall paid calls=%d want %d", calls, before+1)
				}
				if !owned && global[len(global)-1] != "answers" {
					t.Fatalf("recall activity: %v", global)
				}
				before = calls
				if _, err := v.RecallSaved(ctx, "fixture", "What does Cedar queue?", "Cedar"); err != nil {
					t.Fatal(err)
				}
				if calls != before || len(global)+len(owner) != calls {
					t.Fatalf("cache billed again: calls=%d global=%v owned=%v", calls, global, owner)
				}
				t.Logf("paid calls=%d; receipts=%d; cache added zero calls", calls, len(global)+len(owner))
			})
		}
	}
}

func TestLibraryAccountingReconciledFailure(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprintf("owned=%t", owned), func(t *testing.T) {
			profile := libraryAccountingProfile(t)
			v := knowledge.Open(profile)
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			captureRuntimeSource(t, v, "s")
			type receipt struct {
				activity string
				bill     provider.Billed
			}
			receipts := make(chan receipt, 4)
			record := func(ctx context.Context, b provider.Billed) { receipts <- receipt{knowledge.LibraryActivity(ctx), b} }
			knowledge.SetUsageRecorder(record)
			t.Cleanup(func() { knowledge.SetUsageRecorder(nil) })
			if owned {
				knowledge.SetUsageRecorder(func(context.Context, provider.Billed) { t.Error("owned reconciliation also billed globally") })
				v = v.WithCalls(func(ctx context.Context) (context.Context, error) {
					base := ctx
					ctx = provider.WithBilling(ctx, func(b provider.Billed) { record(base, b) })
					ctx = provider.WithReconcile(ctx, func(r provider.Reconciled) {
						if r.Found {
							record(base, r.Billed)
						}
					})
					return provider.WithUnmeteredReceipts(ctx), nil
				})
			}
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
				raw := `{"id":"library-late","model":"deepseek/deepseek-v4.1-flash-1017","choices":[{"finish_reason":"length","message":{"content":"{"}}]}`
				if strings.HasSuffix(r.URL.Path, "/generation") {
					raw = `{"data":{"total_cost":0.125,"tokens_prompt":10,"tokens_completion":2}}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}, nil
			})
			if err := v.Flush(context.Background(), "fixture", true); err == nil {
				t.Fatal("incomplete paid call succeeded")
			}
			select {
			case got := <-receipts:
				if got.activity != "learning" || got.bill.Cost != .125 || got.bill.GenerationID != "library-late" {
					t.Fatalf("late receipt: %+v", got)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("provider receipt never reached owner")
			}
			select {
			case duplicate := <-receipts:
				t.Fatalf("duplicate paid receipt: %+v", duplicate)
			default:
			}
		})
	}
}

func libraryAccountingProfile(t *testing.T) string {
	t.Helper()
	profile := t.TempDir()
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(config.KeyKnowledgeModel)
	if !ok {
		t.Fatal("knowledge model setting missing")
	}
	if err := row.Apply("deepseek/deepseek-v4.1-flash-1017"); err != nil {
		t.Fatal(err)
	}
	return profile
}
