package session

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// Contract A1, A2: abandoned steps survive a restart, and a later stamp grows
// from .26 to .27 rather than forgetting the abandoned .25.
func TestAnAbandonedTurnsStepSpendSurvivesResumeAndTheNextStamp(t *testing.T) {
	first, entered, dir, path, ledger := abandonSpendAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return priced(toolResponse("paid", "wedge", `{}`), .25), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return priced(textResponse("after the stop"), .01), nil
		},
	}})
	events := mustSubmit(t, first, "park")
	waitMetaPatch(t, entered)
	first.Interrupt()
	if _, ok := first.Abandon(AbandonStopTimeout); !ok {
		t.Fatal("the parked turn was not abandoned")
	}
	collect(t, events)
	waitAbandonSpendExit(t, path, 1)
	seals := turnUsageLines(t, path)
	if len(seals) != 1 || seals[0].CostUSD != .25 || seals[0].DurationMS != 0 {
		t.Fatalf("abandoned seals = %+v, want one .25 seal with no duration", seals)
	}
	if lines := abandonedLines(t, path); len(lines) != 1 {
		t.Fatalf("abandoned lines = %+v, want exactly one", lines)
	}
	collect(t, mustSubmit(t, first, "continue"))
	before := stampedMeta(t, first, dir)
	if math.Abs(before.SpentUSD-.26) > 1e-12 || before.Tokens != 42 {
		t.Fatalf("before restart saved %+v, want .26 and 42 tokens", before)
	}
	live := first.Usage()
	if math.Abs(live.CostUSD-.26) > 1e-12 || live.Input != 30 || live.Output != 12 || live.Calls != 2 || live.Turns != 2 {
		t.Fatalf("live meter = %+v, want .26, 30/12 tokens, two calls and two conversation steps", live)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close before restart: %v", err)
	}
	second, err := newAgent(Config{
		Workspace: first.config.Workspace, Model: "test/model", System: "SYSTEM",
		Place: Place{Dir: dir, Workspace: first.config.Workspace}, SessionFile: path, usageLedger: ledger,
	}, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return priced(textResponse("done"), .01), nil
		},
	}})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	t.Cleanup(second.SettleWrites)
	t.Cleanup(func() { _ = second.Close() })
	assertAbandonSpendMeter(t, second.Usage(), live)
	collect(t, mustSubmit(t, second, "finish"))
	after := stampedMeta(t, second, dir)
	if math.Abs(after.SpentUSD-.27) > 1e-12 || after.SpentUSD < before.SpentUSD || after.Tokens != 57 {
		t.Fatalf("next stamp = %+v, want .27 and 57 tokens, above the previous %+v", after, before)
	}
	if used := second.Usage(); used.Calls != 3 || used.Turns != 3 {
		t.Fatalf("next meter = %+v, want three calls and three conversation steps", used)
	}
}

// Contract A3, A5: on-clock errands retain only their own seals, late receipts
// retain their arm lines, and abandonment changes neither duration nor ledger.
func TestAnAbandonedTurnIsJournaledOnceWithItsErrands(t *testing.T) {
	agent, entered, _, path, ledger := abandonSpendAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := priced(toolResponse("paid", "wedge", `{}`), .25)
			response.Usage.CacheReadInputTokens = 3
			response.Usage.CacheCreationInputTokens = 2
			return response, nil
		},
	}})
	events := mustSubmit(t, agent, "park")
	waitMetaPatch(t, entered)
	errands := priced(textResponse("checked"), .125)
	errands.Usage.CacheReadInputTokens = 4
	errands.Usage.CacheCreationInputTokens = 1
	agent.addAuxiliaryUsageAs(errands, "test/helper", 1, "guardian")
	agent.reconciled(provider.Reconciled{
		Billed: provider.Billed{Model: "test/model", PromptTokens: 80, CompletionTokens: 20, CachedTokens: 30, Cost: .125},
		Ref:    "late-abandoned", Reason: "torn", Found: true,
	})
	live := agent.Usage()
	assertAbandonSpendMeter(t, live, Usage{CostUSD: .5, Input: 110, Output: 32, CacheRead: 37, CacheWrite: 3, Calls: 3, Turns: 1})
	FlushUsage()
	before, err := ReadUsage(ledger, time.Time{})
	if err != nil || len(before) != 3 {
		t.Fatalf("ledger before abandon = %+v, %v; want three paid calls", before, err)
	}
	agent.Interrupt()
	spend, ok := agent.Abandon(AbandonStopTimeout)
	if !ok || spend.CostUSD != .375 || spend.Calls != 2 {
		t.Fatalf("abandon = %+v, %v; want only the step and on-clock errand", spend, ok)
	}
	collect(t, events)
	waitAbandonSpendExit(t, path, 1)
	if _, again := agent.Abandon(AbandonStopTimeout); again {
		t.Fatal("a second abandon let go of the same turn")
	}
	if lines := abandonedLines(t, path); len(lines) != 1 || lines[0] != (journalAbandoned{
		Reason: string(AbandonStopTimeout), Input: 30, Output: 12, CacheRead: 7, CacheWrite: 3, CostUSD: .375, Calls: 2,
	}) {
		t.Fatalf("abandoned evidence = %+v, want the original mixed tally without the late receipt", lines)
	}
	seals := turnUsageLines(t, path)
	if len(seals) != 1 || seals[0].CostUSD != .25 || seals[0].Calls != 1 || seals[0].DurationMS != 0 {
		t.Fatalf("step seals = %+v, want exactly one .25 step with no duration", seals)
	}
	if lines := journalUsageLines(t, path); len(lines) != 2 || !lines[0].Aux || lines[0].CostUSD != .125 || lines[0].Role != "guardian" {
		t.Fatalf("usage rows = %+v, want one auxiliary errand and one step seal", lines)
	}
	var arms int
	for _, line := range readLines(t, path) {
		var entry sessionEntry
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Call != nil && entry.Call.Arm != "" {
			arms++
			if entry.Call.Arm != "reconciled" || entry.Call.CostUSD != .125 {
				t.Fatalf("late receipt changed: %+v", entry.Call)
			}
		}
	}
	if arms != 1 {
		t.Fatalf("late arms = %d, want one", arms)
	}
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	assertAbandonSpendMeter(t, replayed.usage, live)
	if got := agent.Usage(); got != live {
		t.Fatalf("abandon changed the live meter or duration: before %+v, after %+v", live, got)
	}
	FlushUsage()
	after, err := ReadUsage(ledger, time.Time{})
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("abandon changed the machine ledger: before %+v, after %+v, %v", before, after, err)
	}
}

// Contract A4, A3: digest metadata and replay agree on abandoned step spend,
// including the ordinary per-model split when two models answered the turn.
func TestAnAbandonedTurnsDigestAgreesWithReplay(t *testing.T) {
	for _, split := range []bool{false, true} {
		name := "one model"
		if split {
			name = "two models"
		}
		t.Run(name, func(t *testing.T) {
			steps := []step{}
			want := Usage{CostUSD: .25, Input: 20, Output: 7, Calls: 1, Turns: 1}
			seals := 1
			if split {
				steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
					response := priced(toolResponse("earlier", "ls", `{"path":"."}`), .125)
					response.Model = "test/earlier"
					return response, nil
				})
				want = Usage{CostUSD: .375, Input: 40, Output: 14, Calls: 2, Turns: 2}
				seals = 2
			}
			steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
				response := priced(toolResponse("paid", "wedge", `{}`), .25)
				response.Model = "test/model"
				return response, nil
			})
			agent, entered, _, path, _ := abandonSpendAgent(t, &scriptedCompleter{steps: steps})
			events := mustSubmit(t, agent, "park")
			waitMetaPatch(t, entered)
			agent.Interrupt()
			if _, ok := agent.Abandon(AbandonStopTimeout); !ok {
				t.Fatal("the parked turn was not abandoned")
			}
			collect(t, events)
			waitAbandonSpendExit(t, path, seals)
			live := agent.Usage()
			if err := agent.Close(); err != nil {
				t.Fatalf("close before reopen: %v", err)
			}
			file, _, err := openSessionFile(path, agent.config.Workspace, "test/model", "")
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			t.Cleanup(func() { _ = file.Close() })
			assertAbandonSpendMeter(t, file.RestoredUsage(), want)
			assertAbandonSpendMeter(t, file.RestoredUsage(), live)
			digest, err := ReadDigest(path)
			if err != nil {
				t.Fatalf("digest: %v", err)
			}
			if usd, tokens := digest.Spend(); usd != want.CostUSD || tokens != want.Input+want.Output {
				t.Fatalf("digest spend = %v / %d tokens, want %+v", usd, tokens, want)
			}
			if meta := digest.Derive(Meta{}); meta.SpentUSD != want.CostUSD || meta.Tokens != want.Input+want.Output {
				t.Fatalf("derived metadata = %+v, want %+v", meta, want)
			}
			lines := turnUsageLines(t, path)
			if len(lines) != seals || len(digest.Usage) != seals {
				t.Fatalf("turn seals = %+v, digest usage = %+v, want %d", lines, digest.Usage, seals)
			}
			for _, line := range lines {
				cost := .25
				if line.Model == "test/earlier" && split {
					cost = .125
				} else if line.Model != "test/model" {
					t.Fatalf("unexpected seal model: %+v", line)
				}
				if line.CostUSD != cost || line.Input != 20 || line.Output != 7 || line.Calls != 1 || line.DurationMS != 0 {
					t.Fatalf("per-model seal = %+v, want its own paid call with no duration", line)
				}
			}
		})
	}
}

// Contract A5: an abandoned request with no usage writes only its evidence,
// keeping the live duration and machine ledger unchanged.
func TestAnAbandonedTurnThatSpentNothingWritesNoSeal(t *testing.T) {
	agent, entered, _, path, ledger := abandonSpendAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := toolResponse("unpaid", "wedge", `{}`)
			response.Usage = nil
			return response, nil
		},
	}})
	events := mustSubmit(t, agent, "park without usage")
	waitMetaPatch(t, entered)
	before := agent.Usage()
	if before != (Usage{}) {
		t.Fatalf("unpaid request banked %+v", before)
	}
	agent.Interrupt()
	if spend, ok := agent.Abandon(AbandonStopTimeout); !ok || spend != (Usage{}) {
		t.Fatalf("abandon = %+v, %v; want no spend", spend, ok)
	}
	collect(t, events)
	waitAbandonSpendExit(t, path, 0)
	if _, again := agent.Abandon(AbandonStopTimeout); again {
		t.Fatal("the empty turn was abandoned twice")
	}
	if lines := abandonedLines(t, path); len(lines) != 1 || lines[0] != (journalAbandoned{Reason: string(AbandonStopTimeout)}) {
		t.Fatalf("empty abandoned evidence = %+v, want exactly one reason-only line", lines)
	}
	if seals := journalUsageLines(t, path); len(seals) != 0 {
		t.Fatalf("an empty abandoned turn wrote usage seals: %+v", seals)
	}
	if after := agent.Usage(); after != before {
		t.Fatalf("empty abandon changed meter or duration: before %+v, after %+v", before, after)
	}
	if usd, rows := ledgerUSD(t, ledger); usd != 0 || rows != 0 {
		t.Fatalf("empty abandon wrote machine ledger spend: %v in %d rows", usd, rows)
	}
}

// Contract A6: historical abandoned evidence never becomes a charge, even
// when its conversation steps have no exit seal to recover.
func TestAnOldAbandonedLineIsStillNotBilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"usage","usage":{"model":"m","input":10,"output":5,"cacheRead":3,"cacheWrite":2,"costUsd":0.25,"calls":1,"durationMs":250}}`,
		`{"type":"call","call":{"model":"m","input":20,"output":7,"costUsd":0.5}}`,
		`{"type":"abandoned","abandoned":{"reason":"stop timeout","input":20,"output":7,"costUsd":0.5,"calls":1}}`,
	)
	file, _, err := openSessionFile(path, "/w", "m", "")
	if err != nil {
		t.Fatalf("open historical journal: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	want := Usage{Input: 10, Output: 5, CacheRead: 3, CacheWrite: 2, CostUSD: .25, Calls: 1, Turns: 1, Duration: 250 * time.Millisecond}
	if got := file.RestoredUsage(); got != want {
		t.Fatalf("historical replay = %+v, want only the existing seal %+v", got, want)
	}
	digest, err := ReadDigest(path)
	if err != nil {
		t.Fatalf("historical digest: %v", err)
	}
	if usd, tokens := digest.Spend(); usd != .25 || tokens != 15 {
		t.Fatalf("historical digest spend = %v / %d tokens, want .25 / 15", usd, tokens)
	}
}

// Contract A3: a displaced turn returning through its ordinary cancellation
// ending writes that seal alone, without an additional abandoned exit seal.
func TestAnAbandonedTurnEndingOrdinarilySealsOnlyOnce(t *testing.T) {
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	letReturn := func() { once.Do(func() { close(release) }) }
	agent, _, _, path, _ := abandonSpendAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return priced(toolResponse("paid", "ls", `{"path":"."}`), .25), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(held)
			<-release
			return nil, ctx.Err()
		},
	}})
	t.Cleanup(letReturn)
	events := mustSubmit(t, agent, "read and wait")
	waitMetaPatch(t, held)
	live := agent.Usage()
	agent.Interrupt()
	if _, ok := agent.Abandon(AbandonStopTimeout); !ok {
		t.Fatal("the held request was not abandoned")
	}
	collect(t, events)
	letReturn()
	waitAbandonSpendExit(t, path, 1)
	if seals := turnUsageLines(t, path); len(seals) != 1 || seals[0].CostUSD != .25 || seals[0].Calls != 1 {
		t.Fatalf("ordinary ending seals = %+v, want just one .25 seal", seals)
	}
	if lines := abandonedLines(t, path); len(lines) != 1 {
		t.Fatalf("ordinary ending abandoned evidence = %+v, want one", lines)
	}
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("ordinary ending replay: %v", err)
	}
	assertAbandonSpendMeter(t, replayed.usage, live)
}

// Contract A3, exit audit: the settle bound and final-report returns must seal
// their paid steps once, without duration or ordinary turn-ending acts.
func TestOtherTurnEndingsSealTheirStepSpend(t *testing.T) {
	for _, ending := range []string{"settle bound", "final report"} {
		t.Run(ending, func(t *testing.T) {
			ctx := context.Background()
			tool, args := "ls", `{"path":"."}`
			var configure func(*Config)
			if ending == "settle bound" {
				ctx = withSettleWake(ctx, settleWake{ceiling: 1})
			} else {
				tool, args = "stage_result", `{"done":true,"output":"finished"}`
				configure = func(c *Config) {
					c.Stage = &fakeStageDoor{}
					c.Unattended = true
				}
			}
			agent, _, _, path, _ := abandonSpendAgent(t, &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return priced(toolResponse("paid", tool, args), .25), nil
				},
			}}, configure)
			events, err := agent.Submit(ctx, "finish at the boundary")
			if err != nil {
				t.Fatalf("submit: %v", err)
			}
			collect(t, events)
			seals := turnUsageLines(t, path)
			if len(seals) != 1 || seals[0].CostUSD != .25 || seals[0].Calls != 1 || seals[0].DurationMS != 0 {
				t.Fatalf("%s seals = %+v, want one .25 seal with no duration", ending, seals)
			}
			replayed, err := replaySessionFile(path)
			if err != nil {
				t.Fatalf("%s replay: %v", ending, err)
			}
			assertAbandonSpendMeter(t, replayed.usage, Usage{CostUSD: .25, Input: 20, Output: 7, Calls: 1, Turns: 1})
			live := agent.Usage()
			assertAbandonSpendMeter(t, replayed.usage, live)
			if live.Duration != 0 {
				t.Fatalf("%s added live duration: %v", ending, live.Duration)
			}
		})
	}
}

// abandonSpendAgent uses the existing releasable wedge and constructor with a
// fixed session folder and private ledger, so resume observes the same record.
func abandonSpendAgent(t *testing.T, completer Completer, configure ...func(*Config)) (*Agent, <-chan struct{}, string, string, string) {
	t.Helper()
	if scripted, ok := completer.(*scriptedCompleter); ok {
		scripted.aside = func(messages []ai.Message) (*ai.Response, bool) {
			if isCaptionCall(messages) || isTitleCall(messages) {
				response := textResponse("")
				response.Usage = nil
				return response, true
			}
			return nil, false
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	t.Cleanup(func() { StopUsageWriter(ledger) })
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Place = Place{Dir: dir, Workspace: c.Workspace}
		c.SessionFile = path
		c.usageLedger = ledger
		for _, mutate := range configure {
			if mutate != nil {
				mutate(c)
			}
		}
	})
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	agent.tools = append(agent.tools, releasableWedgeTool(entered, release))
	definitions, err := toolDefinitions(agent.tools)
	if err != nil {
		t.Fatalf("tool definitions: %v", err)
	}
	agent.definitions = definitions
	return agent, entered, dir, path, ledger
}

// waitAbandonSpendExit waits for the loop's deferred pace row, which follows
// its exit seal even when the abandoned turn's event stream closed earlier.
func waitAbandonSpendExit(t *testing.T, path string, seals int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var ended bool
		var written []journalUsage
		for _, line := range readLines(t, path) {
			var entry sessionEntry
			if json.Unmarshal([]byte(line), &entry) != nil {
				continue
			}
			if entry.Type == "pace" {
				ended = true
			}
			if entry.Type == "usage" && entry.Usage != nil && !entry.Usage.Aux {
				written = append(written, *entry.Usage)
			}
		}
		if ended {
			if len(written) != seals {
				t.Fatalf("turn exit wrote %+v, want %d step seals", written, seals)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the displaced turn did not journal its exit within 10 seconds")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// assertAbandonSpendMeter compares the accounting that must survive replay;
// duration is rounded to milliseconds by the journal's existing format.
func assertAbandonSpendMeter(t *testing.T, got, want Usage) {
	t.Helper()
	if math.Abs(got.CostUSD-want.CostUSD) > 1e-12 || got.Input != want.Input || got.Output != want.Output ||
		got.CacheRead != want.CacheRead || got.CacheWrite != want.CacheWrite || got.Calls != want.Calls || got.Turns != want.Turns {
		t.Fatalf("replayed meter = %+v, want live accounting %+v", got, want)
	}
}
