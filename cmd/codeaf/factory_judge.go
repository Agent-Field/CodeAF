package main

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/route"
	"github.com/Agent-Field/codeaf/internal/factory/route/judge"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/taxonomy"
)

// factoryJudgeName is what an AI filter's question costs under on the spend
// ledger, in the role column, beside the read's `factory triage`. It is one of
// session's factory AI roles, so it counts toward the factory's day and not
// toward "your chats". The manual quotes it (internal/manual/chat/factory.md).
const factoryJudgeName = "factory filter"

// factoryJudge is the floor st's AI filter: the model-backed route.Judge the
// intake asks a rule's question through. Its model is the rule's when the
// intake names one (judge.WithRuleModel), else the factory's default model,
// else the arrival read's; every call is on the spend ledger under
// [factoryJudgeName], failed calls included, exactly as the read's are.
//
// THE SETTINGS ARE READ WHEN A QUESTION IS ASKED, NOT WHEN THE JUDGE IS BUILT,
// as the read's are: a key or a default model changed a minute ago is the one
// the next question uses.
func factoryJudge(st *store.Store, load func() (config.Config, error)) route.Judge {
	return judge.New(judge.Options{
		Call:      factoryJudgeCall(load),
		Transient: factoryJudgeTransient,
		DefaultModel: func() string {
			// A launch or a profile on ONE MODEL FOR EVERY TASK keeps that
			// model rather than the factory's default, which nobody picked
			// for it ([stageOpenModel] reads it the same way); the read's
			// road then answers with the pin.
			if st == nil || factoryWorkerParent().OneModel {
				return ""
			}
			if settings, err := load(); err == nil {
				if _, one := config.TaskOneModelAt(settings.ProfileDir); one {
					return ""
				}
			}
			spending, _ := st.Spending()
			return spending.DefaultModel
		},
		ReadModel: func() (string, error) {
			settings, err := load()
			if err != nil {
				return "", err
			}
			return factoryRoleModel(settings, roles.RoleInsightFactoryRead)
		},
	})
}

// factoryJudgeCall sends one question as a system and a user message at
// temperature zero, so the same item asked twice is answered the same way, and
// books what it cost.
func factoryJudgeCall(load func() (config.Config, error)) judge.Call {
	return func(ctx context.Context, req judge.Request) (judge.Reply, error) {
		settings, err := load()
		if err != nil {
			return judge.Reply{}, err
		}
		clientConfig := settings.ClientConfig(req.Model)
		clientConfig.DropRefusedEffort = true
		client, err := provider.NewClient(clientConfig)
		if err != nil {
			return judge.Reply{}, err
		}
		ctx = provider.WithoutStream(provider.WithCallTag(ctx, factoryJudgeName))
		ctx, recorded := factoryAIAccounting(ctx, settings, req.Model, factoryJudgeName, nil)
		response, err := client.CompleteWithMessages(ctx, []ai.Message{
			judgeMessage("system", req.System), judgeMessage("user", req.User),
		}, ai.WithTemperature(0))
		usd := recorded(response)
		if err != nil {
			return judge.Reply{USD: usd, Model: req.Model}, err
		}
		if response == nil || strings.TrimSpace(response.Text()) == "" {
			return judge.Reply{USD: usd, Model: req.Model}, errors.New("the model answered nothing")
		}
		return judge.Reply{Text: response.Text(), USD: usd, Model: req.Model}, nil
	}
}

// factoryJudgeTransient is the provider's failures a second try can cure: the
// ones internal/taxonomy classes as transport (a rate limit, a server-side
// fault, a dropped stream), or the connection gone. A request the provider
// refused as ours (a 400, a model it does not carry) is not. ONLY THE TAXONOMY
// TURNS A STATUS INTO A MOVE, so this reads its class rather than a status.
func factoryJudgeTransient(err error) bool {
	if provider.IsConnectionUnavailable(err) {
		return true
	}
	return taxonomy.Classify(provider.Evidence(err), taxonomy.Limits{}.Floored()).Class == taxonomy.Transport
}
