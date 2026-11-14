package main

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cachedir"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/subharness"
)

// CacheStatus reads the app machine's shared build cache, never a host cache.
func (b *appBackend) CacheStatus() (appwire.CacheStatus, error) {
	size := cachedir.Size()
	return appwire.CacheStatus{Path: cachedir.Root(), Bytes: size, Human: cachedir.Human(size)}, nil
}

// CleanCache shares every filesystem guard with the terminal. The wire checks
// confirmation before this producer can be reached.
func (b *appBackend) CleanCache() (appwire.CacheCleanResult, error) {
	freed, err := cachedir.Clean()
	return appwire.CacheCleanResult{FreedBytes: freed}, err
}

type appAutonomyDoor interface {
	Autonomy() map[session.AskKind]session.Policy
	SetAutonomy(session.AskKind, session.Policy) error
}

// autonomyDoor resolves the conversation's own handle so host edits reach the
// same project and safety floors as typing the terminal command there.
func (b *appBackend) autonomyDoor(agent string) (appAutonomyDoor, func(), error) {
	_, live, endLive, err := b.borrowLive(agent)
	if err != nil {
		return nil, nil, err
	}
	door, ok := live.conn.Conv.(appAutonomyDoor)
	if !ok {
		endLive()
		return nil, nil, appwire.Errorf(appwire.CodeUnknownMethod, "this conversation keeps no question rules")
	}
	return door, endLive, nil
}
func appAutonomyRows(agent string, door appAutonomyDoor) (appwire.AutonomyResult, error) {
	rules := door.Autonomy()
	if rules == nil {
		return appwire.AutonomyResult{}, appwire.Errorf(appwire.CodeEngine, "question rules could not be read")
	}
	out := appwire.AutonomyResult{Agent: agent, Kinds: []appwire.AutonomyRow{}}
	for _, kind := range session.AutonomyKinds() {
		policy := rules[kind]
		row := appwire.AutonomyRow{Kind: kind, Rule: "ask"}
		if kind != session.AskConfirmation && kind != session.AskClarification {
			switch policy.Kind {
			case session.PolicyDecide:
				row.Rule = "decide"
			case session.PolicyRecommendThenAuto:
				row.Rule, row.After = "recommend", policy.After.String()
			}
		}
		out.Kinds = append(out.Kinds, row)
	}
	return out, nil
}
func (b *appBackend) GetAutonomy(agent string) (appwire.AutonomyResult, error) {
	door, endLive, err := b.autonomyDoor(agent)
	if err != nil {
		return appwire.AutonomyResult{}, err
	}
	defer endLive()
	return appAutonomyRows(agent, door)
}
func (b *appBackend) SetAutonomy(p appwire.AutonomySetParams) (appwire.AutonomyResult, error) {
	kind, policy, refused := session.ParseAutonomyLine(p.Kind + " " + p.Rule)
	if refused != "" {
		return appwire.AutonomyResult{}, appwire.Errorf(appwire.CodeBadParams, "%s", refused)
	}
	door, endLive, err := b.autonomyDoor(p.Agent)
	if err != nil {
		return appwire.AutonomyResult{}, err
	}
	defer endLive()
	if err := door.SetAutonomy(kind, policy); err != nil {
		return appwire.AutonomyResult{}, appwire.Errorf(appwire.CodeBadParams, "%s", err)
	}
	return appAutonomyRows(p.Agent, door)
}

// SavedHarnesses is the terminal panel's current pages, with bounded wire
// output. Oversize registries refuse whole rather than pretend a partial list
// is everything saved on this machine.
func (b *appBackend) SavedHarnesses() (appwire.HarnessListResult, error) {
	release, ok := b.actions.reading()
	if !ok {
		return appwire.HarnessListResult{}, appActionReadsBusy()
	}
	defer release()
	saved, err := subharness.Default().SavedBounded()
	if errors.Is(err, subharness.ErrSavedListTooLarge) {
		return appwire.HarnessListResult{}, appwire.Errorf(appwire.CodeTooLarge, "%s", err)
	}
	if err != nil {
		return appwire.HarnessListResult{}, err
	}
	out := appwire.HarnessListResult{Harnesses: []appwire.HarnessListRow{}}

	for _, item := range saved {
		id := item.Harness.Id
		row := appwire.HarnessListRow{Name: id.Name, Description: id.Desc, Version: id.Version, Runs: item.Runs}
		if item.Last != nil {
			last := subharness.TraceRun(*item.Last)
			row.LastRun = &appwire.HarnessLastRun{At: last.At, Finished: last.Finished}
		}
		out.Harnesses = append(out.Harnesses, row)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if len(raw) > 512<<10 {
		return appwire.HarnessListResult{}, appwire.Errorf(appwire.CodeTooLarge, "harness.list exceeds 512 KiB")
	}
	return out, nil
}

func (b *appBackend) CrewPolicy() (appwire.CrewPolicy, error) {
	return appwire.CrewPolicy{Models: config.CrewAllowedAt(b.profileDir).String(), CapUSD: config.CrewCapAt(b.profileDir)}, nil
}
func (b *appBackend) SetCrewPolicy(p appwire.CrewPolicyPatch) (appwire.CrewPolicy, error) {
	var err error
	switch {
	case p.Models != nil && p.Cap == nil:
		err = config.ApplyCrewModels(b.profileDir, strings.TrimSpace(*p.Models))
	case p.Cap != nil && p.Models == nil:
		err = config.SetCrewCap(b.profileDir, strings.TrimSpace(*p.Cap))
	default:
		return appwire.CrewPolicy{}, appwire.Errorf(appwire.CodeBadParams, "set exactly one of models or cap")
	}
	if err != nil {
		return appwire.CrewPolicy{}, appwire.Errorf(appwire.CodeBadParams, "%s", err)
	}
	return b.CrewPolicy()
}
