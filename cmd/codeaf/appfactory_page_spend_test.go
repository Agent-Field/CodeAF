package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A page's failed generation costs the same as a successful one. Spending
// must show each provider charge exactly once through the real app wire.
func TestAppFactoryPageChargesReachSpendingOnce(t *testing.T) {
	for _, method := range []string{"factory.read", "factory.scout"} {
		for _, failed := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/failed=%v", method, failed), func(t *testing.T) {
				s := newLocalSetup(t)
				var calls atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost {
						fmt.Fprint(w, `{"data":[]}`)
						return
					}
					calls.Add(1)
					text := `{"read":"A read","rubric":{"scope":{"score":0},"clarity":{"score":0},"coupling":{"score":0},"proof":{"score":1},"novelty":{"score":0}}}`
					if method == "factory.scout" {
						text = `{"answer":{"plan":["Check retries"]}}`
					}
					finish := "stop"
					if failed {
						finish = "error"
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"model": readLoopsPin, "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5, "cost": 0.01}})
				}))
				t.Cleanup(server.Close)
				readLoopsProfile(t, &readLoopsWorld{model: server}, readLoopsPin)
				st := s.start()
				dir := t.TempDir()
				gitCheckout(t, dir, "acme/api")
				if out, err := exec.Command("git", "-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.com", "commit", "--allow-empty", "-qm", "fixture").CombinedOutput(); err != nil {
					t.Fatalf("checkout: %v: %s", err, out)
				}
				if err := s.backend.local.host.floor.SetCheckout("acme/api", dir); err != nil {
					t.Fatal(err)
				}
				item, err := s.backend.local.host.floor.Create(factory.Item{Tier: factory.TierOwner, Repo: "acme/api", Num: 7, Title: "Check retries", Kind: factory.KindIssue, State: factory.StateNew})
				if err != nil {
					t.Fatal(err)
				}
				params := map[string]any{"factory": st.Factory, "job": item.ID}
				if method == "factory.read" {
					delete(params, "job")
					params["jobs"] = []int{item.ID}
					var got api.ReadResult
					s.must(method, params, &got)
					if failed && len(got.Failed) != 1 || !failed && len(got.Read) != 1 {
						t.Fatalf("read outcome: %+v", got)
					}
				} else {
					_, code, _ := s.call(method, params)
					if (code != "") != failed {
						t.Fatalf("scout failure=%v, code=%s", failed, code)
					}
				}
				if !session.FlushUsage() {
					t.Fatal("ledger did not flush")
				}
				var got appwire.SpendLedgerResult
				today := time.Now().Format("2006-01-02")
				s.must("spend.ledger", map[string]any{"from": today, "to": today}, &got)
				want := float64(calls.Load()) * .01
				if calls.Load() == 0 || got.Totals.Calls != int(calls.Load()) || got.Totals.CostUSD == nil || math.Abs(*got.Totals.CostUSD-want) > 1e-9 {
					t.Fatalf("charged page calls=%d cost=%.2f; Spending calls=%d cost=%v: every charge must appear once", calls.Load(), want, got.Totals.Calls, got.Totals.CostUSD)
				}
			})
		}
	}
}
