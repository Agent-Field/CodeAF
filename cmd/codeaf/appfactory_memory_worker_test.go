package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestMemoryPausedJobLifecycle(t *testing.T) {
	for _, end := range []string{"pause tail", "pause boundary", "pause resume", "stop tail", "quit tail"} {
		t.Run(end, func(t *testing.T) { memoryLateReceiptFactoryHeader(t, end, 0) })
	}
}

func TestMemoryPauseStopsFinishedStageTail(t *testing.T) {
	memoryLateReceiptFactoryHeader(t, "pause tail", 0)
}

func TestMemoryPauseRetainsPendingForResume(t *testing.T) {
	memoryLateReceiptFactoryHeader(t, "pause resume", 0)
}

func TestMemoryPauseClosesTailAdmission(t *testing.T) {
	memoryLateReceiptFactoryHeader(t, "pause boundary", 0)
}

func TestMemoryLateReceiptReachesFactoryHeaderAfterWorkerFinishes(t *testing.T) {
	memoryLateReceiptFactoryHeader(t, "finish", 0)
}

func TestMemoryLateReceiptReachesFactoryHeaderAfterStop(t *testing.T) {
	memoryLateReceiptFactoryHeader(t, "stop", 0)
}

func TestMemoryLateReceiptPreservesExistingJobSpend(t *testing.T) {
	memoryLateReceiptFactoryHeader(t, "stop", .30)
}

func TestMemoryLateReceiptReachesFactoryHeaderAfterQuit(t *testing.T) {
	memoryLateReceiptFactoryHeader(t, "quit", 0)
}

func TestMemoryLateReceiptReachesFactoryHeaderWhenQuitInterruptsSettlement(t *testing.T) {
	memoryLateReceiptFactoryHeader(t, "settling quit", 0)
}

func memoryLateReceiptFactoryHeader(t *testing.T, end string, existing float64) {
	pauseResume := end == "pause resume"
	pauseBoundary := end == "pause boundary"
	pauseTail := end == "pause tail" || pauseBoundary || pauseResume
	settlingQuit := end == "settling quit"
	stop, quit := end == "stop" || end == "stop tail" || settlingQuit, end == "quit" || end == "quit tail" || settlingQuit
	detachedTail := pauseTail || end == "stop tail" || end == "quit tail"
	quitDone := make(chan struct{})
	s := newLocalSetup(t)
	release := make(chan struct{})
	releaseReceipt := sync.OnceFunc(func() { close(release) })
	cancelled := make(chan struct{}, 1)
	started := make(chan struct{}, 1)
	var memoryCalls, providerUnits, stageTurns atomic.Int64
	chargeReceipt := sync.OnceFunc(func() { providerUnits.Add(2) })
	learning, finishTurn := make(chan struct{}), make(chan struct{})
	finishLearning := sync.OnceFunc(func() { close(finishTurn) })
	t.Cleanup(finishLearning)
	finishMemory := make(chan struct{})
	finishPaidCall := sync.OnceFunc(func() { close(finishMemory) })
	t.Cleanup(finishPaidCall)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			chargeReceipt()
			fmt.Fprint(w, `{"data":{"total_cost":0.02,"tokens_prompt":20,"tokens_completion":5}}`)
			return
		}
		var body struct {
			Messages []struct{ Content string } `json:"messages"`
			Tools    []struct {
				Function struct{ Name string } `json:"function"`
			} `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			return
		}
		var payload map[string]json.RawMessage
		if len(body.Messages) > 1 {
			_ = json.Unmarshal([]byte(body.Messages[1].Content), &payload)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if payload["messages"] != nil || ((pauseBoundary || pauseResume) && len(payload) > 0) {
			n := memoryCalls.Add(1)
			if pauseResume && n > 1 {
				providerUnits.Add(1)
				var p map[string]any
				_ = json.Unmarshal([]byte(body.Messages[1].Content), &p)
				result, _ := json.Marshal(memoryRichReply(p))
				raw, _ := json.Marshal(map[string]any{"model": knowledge.Model, "choices": []any{map[string]any{"delta": map[string]any{"content": string(result)}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5, "cost": .01}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
				return
			}
			select {
			case started <- struct{}{}:
			default:
			}
			fmt.Fprintf(w, "data: {\"id\":\"gen-late-memory\",\"model\":%q,\"choices\":[{\"delta\":{\"content\":\"\"}}]}\n\n", knowledge.Model)
			w.(http.Flusher).Flush()
			if pauseBoundary && n == 1 {
				select {
				case <-finishMemory:
					providerUnits.Add(2)
					var p map[string]any
					_ = json.Unmarshal([]byte(body.Messages[1].Content), &p)
					result, _ := json.Marshal(memoryRichReply(p))
					raw, _ := json.Marshal(map[string]any{"model": knowledge.Model, "choices": []any{map[string]any{"delta": map[string]any{"content": string(result)}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5, "cost": .02}})
					fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
					return
				case <-r.Context().Done():
				}
			} else {
				<-r.Context().Done()
			}
			select {
			case cancelled <- struct{}{}:
			default:
			}
			return
		}
		delta := map[string]any{"content": `{"title":"Cedar","name":"Cedar"}`}
		finish := "stop"
		for _, tool := range body.Tools {
			if tool.Function.Name == "stage_result" {
				if pauseResume && stageTurns.Add(1) == 2 {
					close(learning)
					select {
					case <-finishTurn:
					case <-r.Context().Done():
						return
					}
				}
				delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "report", "type": "function", "function": map[string]any{"name": "stage_result", "arguments": `{"done":true,"output":"Cedar queues jobs."}`}}}}
				finish = "tool_calls"
			}
		}
		providerUnits.Add(1)
		raw, _ := json.Marshal(map[string]any{"model": knowledge.Model, "choices": []any{map[string]any{"delta": delta, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5, "cost": .01}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(releaseReceipt)
	original := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		u := *r.URL
		clone.URL = &u
		if strings.Contains(u.Host, "openrouter.ai") {
			clone.URL.Scheme = "http"
			clone.URL.Host = server.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(clone)
	})}
	t.Cleanup(func() { http.DefaultClient = original })
	profile := v3Profile(t, map[string]any{config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: knowledge.Model, "models.tiers.low": knowledge.Model, "models.tiers.reflex": knowledge.Model, "models.roles": ""})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", "fixture")
	t.Setenv("CODEAF_BASE_URL", server.URL)
	freshEngineProcess(t)
	g := newRunRig(t)
	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proc.closeAll)
	launch, err := openV3Launch(proc, v3Options{Model: knowledge.Model, OneModel: true, Workspace: g.workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := knowledge.Open(profile).StartBuild(); err != nil {
		t.Fatal(err)
	}
	stageSettle = 20 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })
	local := &s.backend.local
	local.parent = func() session.Config { return launch.Config }
	local.rules = func(context.Context, string, string) (pipeline.File, error) { return pipeline.File{}, os.ErrNotExist }
	local.seamHook = func(seam factory.Seam) factory.Seam {
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
		return seam
	}
	s.backend.open = appOpenInProcess
	st := s.start()
	floor := local.host.floor
	if err := floor.SetCheckout("acme/api", g.workspace); err != nil {
		t.Fatal(err)
	}
	stages := []factory.Stage{
		{Name: "write", Kind: factory.StageChat, On: true, Until: "done", Ask: "Call stage_result with done true and output Cedar queues jobs."},
		{Name: factory.ApproveName, Kind: factory.StageGate, On: true},
	}
	if detachedTail {
		stages = append(stages[:1], factory.Stage{Name: "test", Kind: factory.StageCheck, On: true, Ask: "sleep 60"}, stages[1])
	}
	if pauseResume {
		stages[1].Ask = "if test ! -e memory-check-ran; then touch memory-check-ran; sleep 60; fi"
		stages = append(stages[:2], factory.Stage{Name: "learn", Kind: factory.StageChat, On: true, Until: "done", Ask: "Call stage_result with done true and output Cedar queues jobs."}, stages[2])
	}
	id, err := floor.Add(context.Background(), factory.Item{Repo: "acme/api", Title: "Cedar queues jobs", ChatSpent: existing, Cap: 1, Tier: factory.TierOwner, Model: knowledge.Model, Stages: stages})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.backend.closeLocalFactory() })
	var job api.Job
	s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
	var acted appwire.FactoryJobActed
	s.must("factory.job_start", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}, &acted)
	if (stop || quit) && !detachedTail {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("memory did not start")
		}
		if quit && !settlingQuit {
			go func() { s.backend.closeLocalFactory(); close(quitDone) }()
		} else {
			s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
			s.must("factory.job_stop", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}, &acted)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for detachedTail || (!stop && !quit) {
		s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
		finished := false
		for _, step := range job.Steps {
			finished = finished || (step.Name == "write" && step.State == "done")
		}
		if finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not finish: state=%s", job.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if detachedTail {
		s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
		switch {
		case quit:
			go func() { s.backend.closeLocalFactory(); close(quitDone) }()
		case stop:
			s.must("factory.job_stop", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}, &acted)
		default:
			s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
			s.must("factory.job_hold", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}, &acted)
		}
	}
	if pauseBoundary {
		finishPaidCall()
		time.Sleep(250 * time.Millisecond)
		if got := memoryCalls.Load(); got != 1 {
			t.Fatalf("Pause returned but admitted %d new memory calls", got-1)
		}
	}
	if !pauseBoundary {
		select {
		case <-cancelled:
		case <-time.After(3 * time.Second):
			t.Fatal("job end did not cancel memory")
		}
	}
	// Give the existing final-report path a chance to return its lease while
	// the receipt is deliberately still unavailable. A settlement-aware worker
	// may retain that lease; either way the public job must receive the charge.
	delay := 750 * time.Millisecond
	if stop || quit {
		delay = 6 * time.Second
	}
	until := time.Now().Add(delay)
	for time.Now().Before(until) {
		if quit {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		var runners api.Runners
		s.must("factory.runners", map[string]any{"factory": st.Factory}, &runners)
		busy := false
		for _, runner := range runners.Runners {
			for _, active := range runner.Jobs {
				busy = busy || active == id
			}
		}
		if !busy {
			t.Log("worker lease ended before receipt was released")
			if !stop {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if settlingQuit {
		// Stop has revoked the lease and the worker's five-second drop wait
		// has elapsed. Quit now interrupts its already-running settlement.
		local.host.cancel()
		go func() { s.backend.closeLocalFactory(); close(quitDone) }()
	}
	releaseReceipt()
	if quit {
		select {
		case <-quitDone:
		case <-time.After(5 * time.Second):
			t.Fatal("quit did not finish within its bounded wait")
		}
	}
	item, err := floor.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		session.FlushUsage()
		lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		usd, memoryUSD := 0.0, 0.0
		for _, line := range lines {
			if line.FactoryJob == factory.LedgerName(item) {
				usd += line.USD
				if line.Role == "memory" {
					memoryUSD += line.USD
				}
			}
		}
		if quit {
			stored, err := floor.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			job.Cost = factory.JobSpent(stored)
		} else {
			s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
		}
		if memoryUSD >= .02 && math.Abs(job.Cost-usd-existing) < .000001 && math.Abs(usd-float64(providerUnits.Load())/100) < .000001 {
			if pauseTail {
				before := memoryCalls.Load()
				time.Sleep(3 * time.Second)
				if got := memoryCalls.Load(); got != before {
					t.Fatalf("paused memory count grew: %d -> %d", before, got)
				}
				snapshot, err := knowledge.Open(profile).Snapshot()
				if err != nil || snapshot.Pending == 0 {
					t.Fatalf("pause lost pending memory: %+v %v", snapshot, err)
				}
				t.Logf("paused memory calls=%d; ledger=%.4f header=%.4f pending=%d", before, usd, job.Cost, snapshot.Pending)
				if pauseResume {
					s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
					s.must("factory.job_resume", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}, &acted)
					select {
					case <-learning:
					case <-time.After(10 * time.Second):
						t.Fatal("resumed job did not reach its later turn")
					}
					time.Sleep(250 * time.Millisecond)
					if got := memoryCalls.Load(); got != before {
						t.Fatalf("Resume started memory before a finished turn: %d -> %d", before, got)
					}
					finishLearning()
					until := time.Now().Add(15 * time.Second)
					for {
						snapshot, err = knowledge.Open(profile).Snapshot()
						s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
						learned := false
						for _, step := range job.Steps {
							learned = learned || (step.Name == "learn" && step.State == "done")
						}
						session.FlushUsage()
						ledger, _ := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
						resumedUSD := 0.0
						for _, line := range ledger {
							if line.FactoryJob == factory.LedgerName(item) {
								resumedUSD += line.USD
							}
						}
						if err == nil && learned && snapshot.Pending == 0 && memoryCalls.Load() > before && math.Abs(job.Cost-resumedUSD) < .000001 && math.Abs(resumedUSD-float64(providerUnits.Load())/100) < .000001 {
							break
						}
						if time.Now().After(until) {
							t.Fatalf("resume did not compile pending evidence: calls=%d snapshot=%+v error=%v", memoryCalls.Load(), snapshot, err)
						}
						time.Sleep(10 * time.Millisecond)
					}
					t.Logf("resume compiled retained evidence: memory_calls=%d pending=%d", memoryCalls.Load(), snapshot.Pending)
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("memory accounting differs: provider=%.4f memory=%.4f ledger=%.4f header=%.4f", float64(providerUnits.Load())/100, memoryUSD, usd, job.Cost)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
