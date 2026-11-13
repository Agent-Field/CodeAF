package session

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// Contract (a), (c): late reconciled and hedge calls restore their unsealed
// money and tokens as calls, never turns; ordinary call lines stay evidence.
func TestLateCallLinesRestoreSpendWithoutAddingTurns(t *testing.T) {
	for _, test := range []struct {
		name string
		arm  string
		want Usage
	}{
		{
			name: "reconciled", arm: "reconciled",
			want: Usage{Input: 90, Output: 25, CacheRead: 33, CacheWrite: 9, CostUSD: .375, Calls: 2, Turns: 1, Duration: 250 * time.Millisecond},
		},
		{
			name: "hedge", arm: "hedge",
			want: Usage{Input: 90, Output: 25, CacheRead: 33, CacheWrite: 9, CostUSD: .375, Calls: 2, Turns: 1, Duration: 250 * time.Millisecond},
		},
		{
			name: "ordinary",
			want: Usage{Input: 10, Output: 5, CacheRead: 3, CacheWrite: 2, CostUSD: .25, Calls: 1, Turns: 1, Duration: 250 * time.Millisecond},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript.jsonl")
			write(t, path,
				`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
				`{"type":"call","call":{"model":"m","input":10,"output":5,"cacheRead":3,"cacheWrite":2,"costUsd":0.25}}`,
				`{"type":"usage","usage":{"model":"m","input":10,"output":5,"cacheRead":3,"cacheWrite":2,"costUsd":0.25,"calls":1,"durationMs":250}}`,
				fmt.Sprintf(`{"type":"call","call":{"model":"m","arm":%q,"input":80,"output":20,"cacheRead":30,"cacheWrite":7,"costUsd":0.125}}`, test.arm),
			)
			file, _, err := openSessionFile(path, "/w", "m", "")
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { _ = file.Close() })
			if got := file.RestoredUsage(); got != test.want {
				t.Fatalf("restored usage = %+v, want %+v", got, test.want)
			}
		})
	}
}

// Contract (b): a cancelled .25 turn saves .375 after its late .125 receipt;
// reopening the same folder and spending .01 saves .385 rather than .26.
func TestALateReceiptSurvivesResumeAndTheNextSpendStamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	held := make(chan struct{})
	first, workspace := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return priced(toolResponse("first", "ls", `{"path":"."}`), .25), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(held)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}, func(config *Config) {
		config.Place = Place{Dir: dir, Workspace: config.Workspace}
		config.SessionFile = path
		config.usageLedger = ledger
	})
	events := mustSubmit(t, first, "look around")
	waitMetaPatch(t, held)
	first.Interrupt()
	collect(t, events)
	if meta := stampedMeta(t, first, dir); meta.SpentUSD != .25 || meta.Tokens != 27 {
		t.Fatalf("cancelled turn saved %+v, want .25 and 27 tokens", meta)
	}
	if seals := turnUsageLines(t, path); len(seals) != 1 || seals[0].CostUSD != .25 {
		t.Fatalf("cancelled turn seals = %+v, want one .25 seal", seals)
	}
	first.reconciled(provider.Reconciled{
		Billed: provider.Billed{Model: "test/model", PromptTokens: 80, CompletionTokens: 20, CachedTokens: 30, Cost: .125},
		Ref:    "late-before-resume", Reason: "torn", Found: true,
	})
	if meta := stampedMeta(t, first, dir); meta.SpentUSD != .375 || meta.Tokens != 127 {
		t.Fatalf("late receipt saved %+v, want .375 and 127 tokens", meta)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close before resume: %v", err)
	}

	second, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM",
		Place: Place{Dir: dir, Workspace: workspace}, SessionFile: path, usageLedger: ledger,
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
	if used := second.Usage(); used.CostUSD != .375 || used.Input != 100 || used.Output != 27 || used.CacheRead != 30 || used.Calls != 2 || used.Turns != 1 {
		t.Errorf("resumed meter = %+v, want the seal plus its late receipt", used)
	}
	collect(t, mustSubmit(t, second, "finish up"))
	meta := stampedMeta(t, second, dir)
	if math.Abs(meta.SpentUSD-.385) > 1e-12 || meta.Tokens != 142 {
		t.Fatalf("next turn saved %+v, want .385 and 142 tokens after resume", meta)
	}
	if used := second.Usage(); math.Abs(used.CostUSD-.385) > 1e-12 || used.Calls != 3 || used.Turns != 2 {
		t.Fatalf("next turn meter = %+v, want .385, three calls and two conversation steps", used)
	}
}

// Contract (b), review 1 major finding: a receipt pending across Close must
// keep its paid arm line, so a resume restores .375 and the next turn saves
// .385. Ordinary call lines must still be refused after close.
func TestALateReceiptAfterCloseSurvivesResumeAndTheNextSpendStamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	t.Cleanup(func() { StopUsageWriter(ledger) })
	held := make(chan struct{})
	first, workspace := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return priced(toolResponse("first", "ls", `{"path":"."}`), .25), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(held)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}, func(config *Config) {
		config.Place = Place{Dir: dir, Workspace: config.Workspace}
		config.SessionFile = path
		config.usageLedger = ledger
	})
	events := mustSubmit(t, first, "look around")
	waitMetaPatch(t, held)
	settled := first.pendingReceipt()
	first.Interrupt()
	collect(t, events)
	if meta := stampedMeta(t, first, dir); meta.SpentUSD != .25 || meta.Tokens != 27 {
		t.Fatalf("cancelled turn saved %+v, want .25 and 27 tokens", meta)
	}
	if seals := turnUsageLines(t, path); len(seals) != 1 || seals[0].CostUSD != .25 {
		t.Fatalf("cancelled turn seals = %+v, want one .25 seal", seals)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close with a pending receipt: %v", err)
	}
	lines := len(readLines(t, path))
	if first.file.writeLine(sessionEntry{
		Type: "call", Call: &journalCall{Model: "test/model", Input: 1, Output: 1, CostUSD: 1}, Timestamp: stamp(),
	}) {
		t.Error("ordinary call line was accepted after close")
	}
	if got := len(readLines(t, path)); got != lines {
		t.Fatalf("ordinary call changed the closed journal from %d to %d lines", lines, got)
	}
	first.reconciled(provider.Reconciled{
		Billed: provider.Billed{Model: "test/model", PromptTokens: 80, CompletionTokens: 20, CachedTokens: 30, Cost: .125},
		Ref:    "late-after-close", Reason: "torn", Found: true,
	})
	settled()
	if meta := stampedMeta(t, first, dir); meta.SpentUSD != .375 || meta.Tokens != 127 {
		t.Fatalf("post-close receipt saved %+v, want .375 and 127 tokens", meta)
	}
	if seals := turnUsageLines(t, path); len(seals) != 1 || seals[0].CostUSD != .25 {
		t.Fatalf("post-close receipt changed turn seals: %+v", seals)
	}
	digest, err := ReadDigest(path)
	if err != nil {
		t.Fatalf("digest after close: %v", err)
	}
	if usd, tokens := digest.Spend(); usd != .375 || tokens != 127 {
		t.Errorf("post-close digest spend = %v / %d tokens, want .375 / 127", usd, tokens)
	}

	second, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM",
		Place: Place{Dir: dir, Workspace: workspace}, SessionFile: path, usageLedger: ledger,
	}, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return priced(textResponse("done"), .01), nil
		},
	}})
	if err != nil {
		t.Fatalf("resume after post-close receipt: %v", err)
	}
	t.Cleanup(second.SettleWrites)
	t.Cleanup(func() { _ = second.Close() })
	if used := second.Usage(); used.CostUSD != .375 || used.Input != 100 || used.Output != 27 || used.CacheRead != 30 || used.Calls != 2 || used.Turns != 1 {
		t.Errorf("resumed meter = %+v, want the seal plus its post-close receipt", used)
	}
	collect(t, mustSubmit(t, second, "finish up"))
	meta := stampedMeta(t, second, dir)
	if math.Abs(meta.SpentUSD-.385) > 1e-12 || meta.Tokens != 142 {
		t.Fatalf("next turn saved %+v, want .385 and 142 tokens after the post-close receipt", meta)
	}
	if used := second.Usage(); math.Abs(used.CostUSD-.385) > 1e-12 || used.Calls != 3 || used.Turns != 2 {
		t.Fatalf("next turn meter = %+v, want .385, three calls and two conversation steps", used)
	}
}

// Contract (d), (c): the digest adds late reconciled and hedge receipts to
// saved spend while Usage still contains only the seal, never the call rows.
func TestDigestSpendIncludesLateReceiptsWithoutAddingUsageLines(t *testing.T) {
	for _, arm := range []string{"reconciled", "hedge"} {
		t.Run(arm, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript.jsonl")
			write(t, path,
				`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
				`{"type":"call","call":{"model":"m","input":10,"output":5,"cacheRead":3,"cacheWrite":2,"costUsd":0.25}}`,
				`{"type":"usage","usage":{"model":"m","input":10,"output":5,"cacheRead":3,"cacheWrite":2,"costUsd":0.25,"calls":1}}`,
				fmt.Sprintf(`{"type":"call","call":{"model":"m","arm":%q,"input":80,"output":20,"cacheRead":30,"cacheWrite":7,"costUsd":0.125}}`, arm),
			)
			digest, err := ReadDigest(path)
			if err != nil {
				t.Fatalf("digest: %v", err)
			}
			if usd, tokens := digest.Spend(); usd != .375 || tokens != 115 {
				t.Fatalf("digest spend = %v / %d tokens, want .375 / 115", usd, tokens)
			}
			if len(digest.Usage) != 1 {
				t.Fatalf("digest Usage = %+v, want only the one usage seal", digest.Usage)
			}
			if used := digest.Usage[0]; used.USD != .25 || used.Input != 10 || used.Output != 5 || used.Calls != 1 {
				t.Fatalf("digest Usage row = %+v, want the seal's figures alone", used)
			}
			if meta := digest.Derive(Meta{}); meta.SpentUSD != .375 || meta.Tokens != 115 {
				t.Fatalf("derived metadata = %+v, want .375 and 115 tokens", meta)
			}
		})
	}
}

// Contract (a), manual: the shipped page must not tell a reader that every call
// line is left out of the bill. A resume adds a line marked with an arm
// (TestLateCallLinesRestoreSpendWithoutAddingTurns), so any paragraph that
// says call lines are never added up has to name that exception beside it.
func TestTheManualNamesTheArmWhereverItSaysCallLinesAreNotAddedUp(t *testing.T) {
	page, err := os.ReadFile("../manual/chat/sessions-and-rewind.md")
	if err != nil {
		t.Fatal(err)
	}
	var denials int
	for _, paragraph := range strings.Split(string(page), "\n\n") {
		words := strings.ToLower(strings.Join(strings.Fields(paragraph), " "))
		if !strings.Contains(words, "`call` line") && !strings.Contains(words, "call lines") {
			continue
		}
		if !strings.Contains(words, "nothing adds them up") && !(strings.Contains(words, "never") && strings.Contains(words, "added up")) {
			continue
		}
		denials++
		if !strings.Contains(paragraph, "`arm`") {
			t.Errorf("this paragraph says call lines are not added up without naming the late-receipt arm a resume adds:\n%s", paragraph)
		}
	}
	if denials == 0 {
		t.Fatal("the page no longer says which call lines a resume leaves out")
	}
}
