package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/reporead"
	"github.com/Agent-Field/codeaf/internal/factory/scout"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// factoryScoutName is what a look's cost is called on the spend ledger, in
// the role column, beside the read's `factory triage`.
const factoryScoutName = "factory scout"

// factoryScoutLook is Look closer on the floor st, for serve.Config.Scout: the
// job's checkout read through reporead, a model chosen as the read's is, every
// call on the spend ledger. A machine with no key answers the model's refusal
// on the look itself, so a key added later works without a restart.
func factoryScoutLook(st *store.Store, load func() (config.Config, error)) func(ctx context.Context, id int, refresh bool) (factory.Scout, bool, error) {
	if st == nil || load == nil {
		return nil
	}
	door := scout.Door{
		Floor: st, Limits: scout.DefaultLimits, Now: time.Now,
		Head: func(ctx context.Context, repo string) (string, error) {
			got, err := reporead.Read(ctx, st.CheckoutDir(repo), reporead.Request{Op: reporead.OpTree})
			return got.Commit, err
		},
		Run: func(ctx context.Context, it factory.Item) (factory.Scout, error) {
			dir := st.CheckoutDir(it.Repo)
			look := func(ctx context.Context, req reporead.Request) (api.RepoRead, error) {
				return reporead.Read(ctx, dir, req)
			}
			return scout.Run(ctx, it, look, factoryScoutChat(load), scout.DefaultBudget)
		},
	}
	return door.Look
}

// factoryScoutChat sends a look's conversation on the read's model and puts
// each call's cost on the spend ledger under [factoryScoutName].
func factoryScoutChat(load func() (config.Config, error)) scout.Chat {
	return func(ctx context.Context, msgs []scout.Message) (scout.Reply, error) {
		settings, err := load()
		if err != nil {
			return scout.Reply{}, err
		}
		model, err := factoryTriageModel(settings)
		if err != nil {
			return scout.Reply{}, err
		}
		if model == "" {
			return scout.Reply{}, errors.New("no model for the small-work seat")
		}
		client, err := provider.NewClient(settings.ClientConfig(model))
		if err != nil {
			return scout.Reply{}, err
		}
		sent := make([]ai.Message, len(msgs))
		for i, m := range msgs {
			sent[i] = judgeMessage(m.Role, m.Text)
		}
		ctx = provider.WithoutStream(provider.WithCallTag(ctx, factoryScoutName))
		response, err := client.CompleteWithMessages(ctx, sent)
		if err != nil {
			return scout.Reply{}, err
		}
		usd := recordFactoryAIUsage(settings, model, factoryScoutName, response)
		if response == nil || strings.TrimSpace(response.Text()) == "" {
			return scout.Reply{USD: usd, Model: model}, errors.New("the model answered nothing")
		}
		return scout.Reply{Text: response.Text(), USD: usd, Model: model}, nil
	}
}

// factoryClickRead is a person's Read of chosen jobs on the floor st, for
// serve.Config.Read: the arrival read's own call and prompt, each priced read
// noted on the floor and the spend ledger as the worker's are, whether or not
// new issues are read as they arrive. A machine with no key reads nothing,
// and every chosen job comes back failed.
func factoryClickRead(st *store.Store, load func() (config.Config, error)) func(ctx context.Context, ids []int) api.ReadResult {
	if st == nil || load == nil {
		return nil
	}
	return func(ctx context.Context, ids []int) api.ReadResult {
		call, ok := factoryTriageCallNoting(load, func(usd float64) { _ = st.NoteReadCost(usd) })
		if !ok {
			return api.ReadResult{Read: []int{}, Skipped: []int{}, Failed: append([]int{}, ids...)}
		}
		got := triage.ReadThese(ctx, st, call, ids, triage.Options{AskFirst: factoryIntake(st).AskFirst})
		return api.ReadResult{Read: got.Read, Skipped: got.Skipped, Failed: got.Failed}
	}
}
