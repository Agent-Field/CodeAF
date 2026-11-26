package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

func (b *appBackend) LaunchAppAI(p appwire.AppAILaunch) (string, error) {
	if p.App == "" || p.Action == "" || (len(p.Records) == 0) == (p.Query == nil) {
		return "", appwire.Errorf(appwire.CodeBadParams, "Choose an action and either records or a query.")
	}
	files := runs.Files(home.Join("apps"))
	app, err := files.App(p.App)
	if err != nil {
		return "", appRunError(err)
	}
	index := slices.IndexFunc(app.Commands, func(c schema.Command) bool { return c.ID == p.Action && c.Run != "" })
	if index < 0 {
		return "", appwire.Errorf(appwire.CodeNotFound, "That launch action is unavailable.")
	}
	command := app.Commands[index]
	def, err := files.Load(p.App, command.Run)
	if err != nil {
		return "", appRunError(err)
	}
	if len(def.Steps) != 1 || def.Steps[0].AI == nil || def.Steps[0].AI.Propose == "" {
		return "", appwire.Errorf(appwire.CodeBadParams, "That action does not draft a proposal.")
	}
	ai := def.Steps[0].AI
	if err := b.appAIHarnesses(files).Check(ai); err != nil {
		return "", appRunError(err)
	}
	param := strings.TrimPrefix(ai.Propose, "$params.")
	params := make(map[string]any, len(p.Params)+len(command.Params))
	for name, value := range p.Params {
		if _, declared := def.Params[name]; !declared || name == param {
			return "", appwire.Errorf(appwire.CodeBadParams, "Unexpected argument %q.", name)
		}
		params[name] = value
	}
	for name, value := range command.Params {
		if ref, ok := value.(string); ok && strings.HasPrefix(ref, "$") {
			if name == param {
				continue
			}
			return "", appwire.Errorf(appwire.CodeBadParams, "This launch action needs a resolved argument for %q.", name)
		}
		params[name] = value
	}
	if p.Query != nil {
		db, err := b.dataStore("")
		if err != nil {
			return "", err
		}
		result, err := db.Query(data.Scope{App: p.App, Agent: true}, def.Params[param].To, *p.Query, "", 50)
		if err != nil {
			return "", appDataError(err)
		}
		if result.Next != "" {
			return "", appwire.Errorf(appwire.CodeBadParams, "Choose a query with at most 50 records.")
		}
		for _, row := range result.Rows {
			p.Records = append(p.Records, row.ID())
		}
	}
	if b.startAppRunsOnce != nil {
		b.startAppRunsOnce()
	}
	engine := b.appRuns.Load()
	if engine == nil {
		return "", appwire.Errorf(appwire.CodeEngine, "App runs are unavailable.")
	}
	queue, err := engine.StartFanout(p.App, command.Run, p.Screen, params, p.Records, p.ClientID)
	return queue, appRunError(err)
}

func (b *appBackend) ReviewAppAI(p appwire.AppAIReview) error {
	if p.App == "" || p.Queue == "" || p.Decision != "accept" && p.Decision != "reject" && p.Decision != "accept_safe" {
		return appwire.Errorf(appwire.CodeBadParams, "Choose a queue and a review decision.")
	}
	rows, err := b.ListRuns(p.App, "")
	if err != nil {
		return err
	}
	var typ string
	proposals := map[string]*runs.Proposal{}
	for _, row := range rows {
		if row.Queue != p.Queue || row.State != runs.Done || row.Proposal == nil {
			continue
		}
		proposals[row.ID] = row.Proposal
		if typ == "" {
			typ = row.Proposal.Type
		}
	}
	if len(proposals) == 0 {
		return appwire.Errorf(appwire.CodeNotFound, "No proposals have landed in this review queue.")
	}
	effects := make([]data.AIEffect, 0, len(p.IDs))
	for _, selected := range p.IDs {
		draft := proposals[selected.ID]
		rev, err := strconv.ParseInt(selected.ExpectedRev, 10, 64)
		if draft == nil || err != nil || rev != draft.Revision || draft.Type != typ {
			return appwire.Errorf(appwire.CodeConflict, "The selected proposal or record revision changed.")
		}
		effects = append(effects, data.AIEffect{ID: selected.ID, Record: draft.Record, Revision: rev, Set: draft.Set})
	}
	if len(effects) == 0 {
		return appwire.Errorf(appwire.CodeBadParams, "Choose the proposals to review.")
	}
	db, err := b.dataStore("")
	if err != nil {
		return err
	}
	return appDataError(db.ReviewAI(data.Scope{App: p.App}, p.Queue, typ, p.Decision, effects))
}

func appAIUnavailableError(err error) error {
	if errors.Is(err, runs.ErrAIUnavailable) {
		return appwire.Errorf(appwire.CodeNotFound, "That AI action is unavailable.")
	}
	return err
}

func (b *appBackend) SteerAppAI(p appwire.AppAgentRunControl) error {
	if engine := b.appRuns.Load(); engine != nil {
		return appRunError(engine.SteerAgent(p.App, p.Run, p.Text))
	}
	return appRunError(runs.ErrNotFound)
}

func (b *appBackend) StopAppAI(p appwire.AppAgentRunControl) error {
	if engine := b.appRuns.Load(); engine != nil {
		r, _ := engine.Get(p.Run)
		err := engine.StopAgent(p.App, p.Run)
		if err == nil && r.Approval != nil {
			b.closeRunInbox(appRunAskAgent, appRunAskPrefix+r.Approval.ID, appwire.OptionStop)
		}
		return appRunError(err)
	}
	return appRunError(runs.ErrNotFound)
}

func (b *appBackend) AnswerAppAI(p appwire.AppAgentEffectAnswer) error {
	if p.Answer != "yes" && p.Answer != "no" {
		return appwire.Errorf(appwire.CodeBadParams, "Choose yes or no.")
	}
	if p.App == "" {
		return appwire.Errorf(appwire.CodeBadParams, "Choose an app.")
	}
	r, err := b.GetRun(p.App, p.Run)
	if err != nil {
		return err
	}
	effect := appAIPendingEffect(r)
	if effect == nil || *effect != p.Effect {
		return appwire.Errorf(appwire.CodeConflict, "The pending effect changed.")
	}
	expires, _ := time.Parse(time.RFC3339Nano, effect.ExpiresAt)
	if !b.now().Before(expires) {
		return appwire.Errorf(appwire.CodeConflict, "The pending effect expired.")
	}
	option := appwire.OptionDeny
	if p.Answer == "yes" {
		option = appwire.OptionAllowOnce
	}
	return b.answerRunApproval(appRunAskAgent, appRunAskPrefix+effect.ID, option, "")
}

func appAIPendingEffect(r runs.Run) *appwire.AppAgentPendingEffect {
	a := r.Approval
	if a == nil || r.AppVersion == 0 || a.Kind == "budget" {
		return nil
	}
	payload, err := json.Marshal(a.Args)
	if err != nil {
		return nil
	}
	digest := sha256.Sum256(payload)
	destination := ""
	for _, key := range []string{"destination", "to", "channel", "recipient"} {
		if v, ok := a.Args[key].(string); ok && v != "" {
			destination = v
			break
		}
	}
	return &appwire.AppAgentPendingEffect{
		ID: a.ID, Account: a.Who, Operation: a.Tool, Destination: destination, Payload: string(payload),
		PayloadHash: hex.EncodeToString(digest[:]), AppVersion: strconv.Itoa(r.AppVersion),
		ExpiresAt: appApprovalExpiry(r).UTC().Format(time.RFC3339Nano),
	}
}

// Old run journals only recorded the checkpoint time of a pending approval.
func appApprovalExpiry(r runs.Run) time.Time {
	asked := r.Approval.AskedAt
	if asked.IsZero() {
		asked = r.UpdatedAt
	}
	return asked.Add(30 * time.Minute)
}
