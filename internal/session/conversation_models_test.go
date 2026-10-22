package session

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestConversationModelsFallbackReturnsToMainAndKeepsNotices(t *testing.T) {
	c := chained([]string{"unselected/third"}, blindCutStep(provider.CutSilent, ""), blindCutStep(provider.CutSilent, ""), func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("fallback answer"), nil }, func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("main answer"), nil })
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	a, _ := newTestAgent(t, c, func(cfg *Config) {
		cfg.SessionFile = path
		cfg.Place = Place{Dir: filepath.Dir(path)}
		cfg.ConversationModels = &ConversationModels{Main: "test/model", Fallback: "chosen/backup"}
	})
	for range 2 {
		events, err := a.Submit(context.Background(), "respond")
		if err != nil {
			t.Fatal(err)
		}
		got := collect(t, events)
		if _, bad := firstOfKind(got, EventError); bad {
			t.Fatalf("request failed: %v", textsOfKind(got, EventError))
		}
	}
	if c.model(0) != "test/model" || c.model(2) != "chosen/backup" || c.model(3) != "test/model" {
		t.Fatalf("wrong request models: %q", c.models)
	}
	a.SettleWrites()
	record := ReadTranscript(path)
	if len(record.ModelSwitches) != 2 || !strings.Contains(record.ModelSwitches[0].Text, "Using fallback model chosen/backup") || record.ModelSwitches[1].Text != "Using main model test/model." {
		t.Fatalf("switch record: %+v", record.ModelSwitches)
	}
	for _, msgs := range c.seen {
		for _, m := range msgs {
			for _, part := range m.Content {
				if strings.Contains(part.Text, "Using fallback model") {
					t.Fatal("notice entered provider transcript")
				}
			}
		}
	}
}

func TestConversationModelsChangeDoesNotCancelSilentRequest(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	c := chained(nil, func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(started)
		select {
		case <-release:
			return textResponse("old finishes"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("new answer"), nil })
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	a, _ := newTestAgent(t, c, func(cfg *Config) {
		cfg.SessionFile = path
		cfg.Place = Place{Dir: filepath.Dir(path)}
		cfg.ConversationModels = &ConversationModels{Main: "test/model", Fallback: "backup/old"}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := a.Submit(ctx, "go")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	notices, err := a.SetConversationModels(ConversationModels{Main: "vendor/new", Fallback: "backup/new"})
	if err != nil {
		t.Fatal(err)
	}
	if len(notices) != 2 || !strings.Contains(notices[0].Text, "Applies to the next request") {
		t.Fatalf("selection receipts: %+v", notices)
	}
	if a.ModelIdentity().Active != "test/model" {
		t.Fatal("relabeled active request")
	}
	close(release)
	if _, bad := firstOfKind(collect(t, events), EventError); bad {
		t.Fatal("switch interrupted request")
	}
	events, err = a.Submit(ctx, "next")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if c.model(1) != "vendor/new" {
		t.Fatalf("next request used %s", c.model(1))
	}
	a.SettleWrites()
	if got := ReadTranscript(path).ModelSwitches; len(got) != 3 || got[2].Text != "Using main model vendor/new." {
		t.Fatalf("receipts: %+v", got)
	}
}

func TestConversationModelsSavedPairAndBoundedFallback(t *testing.T) {
	c := chained([]string{"third/model"})
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	a, _ := newTestAgent(t, c, func(cfg *Config) {
		cfg.SessionFile = path
		cfg.Place = Place{Dir: filepath.Dir(path)}
		cfg.ConversationModels = &ConversationModels{Main: "test/model", Fallback: "backup/model"}
	})
	pair, ok := a.ConversationModelPair()
	if !ok {
		t.Fatal("pair absent")
	}
	ctx := context.WithValue(context.Background(), conversationModelsKey{}, pair)
	if next, ok := a.nextFallback(ctx, pair.Main, nil); !ok || next != pair.Fallback {
		t.Fatalf("fallback = %s %v", next, ok)
	}
	if _, ok := a.nextFallback(ctx, pair.Main, []string{pair.Fallback}); ok {
		t.Fatal("third model permitted")
	}
	if _, err := a.SetConversationModels(ConversationModels{Main: "new/main", Fallback: "new/backup"}); err != nil {
		t.Fatal(err)
	}
	a.SettleWrites()
	meta, err := LoadMeta(a.config.Place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ModelIdentity == nil || meta.ModelIdentity.Models == nil || meta.ModelIdentity.Models.Main != "new/main" || meta.ModelIdentity.Models.Fallback != "new/backup" {
		t.Fatalf("saved pair: %+v", meta.ModelIdentity)
	}
	b, _ := newTestAgent(t, chained(nil), func(cfg *Config) { cfg.Place = a.config.Place })
	got, _ := b.ConversationModelPair()
	if got.Main != "new/main" || got.Fallback != "new/backup" {
		t.Fatalf("restored pair: %+v", got)
	}
}

func TestConversationModelsBothFailThenRecoveryStartsOnMain(t *testing.T) {
	c := chained([]string{"never/third"}, blindCutStep(provider.CutSilent, ""), blindCutStep(provider.CutSilent, ""), blindCutStep(provider.CutSilent, ""), blindCutStep(provider.CutSilent, ""), func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("recovered"), nil })
	a, _ := newTestAgent(t, c, func(cfg *Config) {
		cfg.ConversationModels = &ConversationModels{Main: "test/model", Fallback: "backup/model"}
	})
	events, err := a.Submit(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, bad := firstOfKind(collect(t, events), EventError); !bad {
		t.Fatal("both failed without a recoverable error")
	}
	if c.requests() != 4 || c.model(2) != "backup/model" {
		t.Fatalf("chain escaped pair: %v", c.models)
	}
	events, err = a.Submit(context.Background(), "try again")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if c.model(4) != "test/model" {
		t.Fatalf("recovery started on %s", c.model(4))
	}
}

func TestConversationModelsDefaultsAreCopiedAndSelectionsStayIndependent(t *testing.T) {
	defaults := &ConversationModels{Main: "test/model", Fallback: "backup/model"}
	makeAgent := func() *Agent {
		a, _ := newTestAgent(t, chained(nil), func(cfg *Config) { cfg.ConversationModels = defaults })
		return a
	}
	manager, step := makeAgent(), makeAgent()
	if _, err := manager.SetConversationModels(ConversationModels{Main: "manager/new", Fallback: "manager/backup"}); err != nil {
		t.Fatal(err)
	}
	pair, _ := step.ConversationModelPair()
	if pair.Main != "test/model" || pair.Fallback != "backup/model" || defaults.Main != "test/model" {
		t.Fatalf("changed another chat or defaults: %+v %+v", pair, defaults)
	}
}

func TestConversationModelsChangedMainReachesTheNextRetryWithoutCancellation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	fail := blindCutStep(provider.CutSilent, "")
	c := chained(nil, func(ctx context.Context, msgs []ai.Message) (*ai.Response, error) {
		close(started)
		select {
		case <-release:
			return fail(ctx, msgs)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("new main answers"), nil
	})
	a, _ := newTestAgent(t, c, func(cfg *Config) {
		cfg.ConversationModels = &ConversationModels{Main: "test/model", Fallback: "backup/old"}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := a.Submit(ctx, "go")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := a.SetConversationModels(ConversationModels{Main: "main/new", Fallback: "backup/new"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if _, bad := firstOfKind(collect(t, events), EventError); bad {
		t.Fatal("new main failed")
	}
	if c.requests() != 2 || c.model(1) != "main/new" {
		t.Fatalf("retried obsolete selection: %+v", c.models)
	}
}

func TestConversationModelsProviderIdentityKeepsItsOriginalSlotDuringEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	a, _ := newTestAgent(t, chained(nil), func(cfg *Config) {
		cfg.SessionFile = path
		cfg.Place = Place{Dir: filepath.Dir(path)}
		cfg.ConversationModels = &ConversationModels{Main: "main/a", Fallback: "backup/c"}
	})
	a.startConversationModelRequest("main/a", "provider", ConversationModels{Main: "main/a", Fallback: "backup/c"})
	a.endModelRequest("main/a")
	a.startConversationModelRequest("backup/c", "provider", ConversationModels{Main: "main/a", Fallback: "backup/c"})
	if _, err := a.SetConversationModels(ConversationModels{Main: "main/b", Fallback: "backup/d"}); err != nil {
		t.Fatal(err)
	}
	a.servedModel("backup/concrete")
	got := ReadTranscript(path).ModelSwitches
	if len(got) != 4 || got[3].Text != "Using fallback model backup/concrete." {
		t.Fatalf("provider receipt: %+v", got)
	}
	a.endModelRequest("backup/concrete")
}

func TestConversationModelsConcurrentPicksKeepLiveAndJournalOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	started, release := make(chan struct{}), make(chan struct{})
	c := chained(nil, func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(started)
		select {
		case <-release:
			return textResponse("done"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	a, _ := newTestAgent(t, c, func(cfg *Config) {
		cfg.SessionFile = path
		cfg.Place = Place{Dir: filepath.Dir(path)}
		cfg.ConversationModels = &ConversationModels{Main: "test/model", Fallback: "backup/a"}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := a.Submit(ctx, "go")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	var wg sync.WaitGroup
	for _, pair := range []ConversationModels{{Main: "main/b", Fallback: "backup/b"}, {Main: "main/c", Fallback: "backup/c"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.SetConversationModels(pair); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(release)
	var live []string
	for _, e := range collect(t, events) {
		if e.ModelSwitch != nil {
			live = append(live, e.ModelSwitch.ID)
		}
	}
	saved := ReadTranscript(path).ModelSwitches
	if len(live) != 4 || len(saved) != 4 {
		t.Fatalf("missing notices: live %d saved %d", len(live), len(saved))
	}
	for i, id := range live {
		if id != saved[i].ID {
			t.Fatalf("live source order differs at %d", i)
		}
	}
}
