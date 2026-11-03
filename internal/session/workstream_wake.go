package session

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

// WorkstreamWakeAsk is a bounded snapshot for one unattended turn, with no tools.
type WorkstreamWakeAsk struct {
	ID       string
	AgentID  string
	Event    workstream.WakeEvent
	Snapshot json.RawMessage
}

type WorkstreamWakeAnswer struct {
	Text    string
	CostUSD *float64
	Model   string
}

const workstreamWakeRules = `You are the workstream's agent. You propose; the person keeps. You write only your own pages; the person's pages are theirs. Keep your agent page "` + WorkstreamWhereItStandsTitle + `" current, saying what it was updated from. Never invent numbers: counts, costs, states and dates come from the snapshot; absent numbers are unknown. On a page use a live block such as [live: jobs] instead of writing a number. You start nothing; the person launches jobs.
For this wake, answer one JSON object. Summarise in at most two sentences, rank the most useful items first, at most 10 items. The engine decides what applies under autonomy; decisions and questions are always proposed. You cannot start jobs or write the person's pages. There are no tools.`

const workstreamWakeFormat = `Answer format:
{"summary":"...","items":[{"kind":"move","factory":"f1","job":1102,"to_wave":3,"why":"..."},{"kind":"move","draft_id":"d_x","to_wave":2,"why":"..."},{"kind":"draft","title":"...","repo":"api","text":"...","size":"S","budget_usd":3,"wave":3,"why":"..."},{"kind":"page","page_id":"existing agent page id; omit for new","title":"...","markdown":"...","why":"..."},{"kind":"decision","text":"...","why":"..."},{"kind":"question","text":"...","why":"..."}]}`

func workstreamWakeQuestion(ask WorkstreamWakeAsk) string {
	event, _ := json.Marshal(ask.Event)
	snapshot := "Numbers absent below are unknown; say so and never estimate them.\n" + string(ask.Snapshot)
	if len(snapshot) > workstreamReadMax {
		const tail = "\nThe workstream read was cut."
		snapshot = workstreamCut(snapshot, workstreamReadMax-len(tail)) + tail
	}
	return "Event:\n" + string(event) + "\nWorkstream snapshot:\n" + snapshot + "\n" + workstreamWakeFormat
}

// RunWorkstreamWake resolves the credentialed sentinel ladder down to the person's model.
// It constructs call state only: no agent, transcript, tool belt or turn loop.
func RunWorkstreamWake(ctx context.Context, settings TeamProposerConfig, ask WorkstreamWakeAsk) (WorkstreamWakeAnswer, error) {
	cfg := Config{Model: settings.Model, APIKey: settings.APIKey, BaseURL: settings.BaseURL, ProfileDir: settings.ProfileDir, Sources: settings.Sources, RolesSource: settings.RolesSource}
	models, err := automationJudgeModels(cfg)
	if err != nil {
		return WorkstreamWakeAnswer{}, err
	}
	model := models[0]
	client, err := newProviderClient(cfg, model)
	if err != nil {
		return WorkstreamWakeAnswer{}, err
	}
	return workstreamWakeCall(ctx, client, model, ask, func(line UsageLine) { RecordUsage(UsageLedgerPath(), line) })
}

func workstreamWakeCall(ctx context.Context, client Completer, model string, ask WorkstreamWakeAsk, record func(UsageLine)) (WorkstreamWakeAnswer, error) {
	ctx = provider.WithRole(provider.WithRoutingIntent(provider.WithoutStream(withPurpose(ctx, purposeWorkstreamWake)), provider.IntentBackground), lane.RoleStanding)
	response, err := client.CompleteWithMessages(ctx, []ai.Message{textMessage("system", workstreamWakeRules), textMessage("user", workstreamWakeQuestion(ask))})
	answer := WorkstreamWakeAnswer{Model: model}
	line := UsageLine{Model: model, Role: string(roles.RoleSentinel), Calls: 1, Session: ask.AgentID, Run: ask.ID, Unbilled: true}
	if response == nil {
		record(line)
		return answer, workstreamWakeResponseError(err)
	}
	answer.Text = response.Text()
	if response.Usage != nil {
		line.Input, line.Output = response.Usage.PromptTokens, response.Usage.CompletionTokens
		answer.CostUSD = response.Usage.Cost
		if answer.CostUSD != nil {
			line.USD = *answer.CostUSD
			line.Price = usagePrice(line.USD, usagePriceProvider)
			line.Unbilled = false
		}
	}
	record(line)
	return answer, err
}
func workstreamWakeResponseError(err error) error {
	if err != nil {
		return err
	}
	return errors.New("the model answered nothing")
}
