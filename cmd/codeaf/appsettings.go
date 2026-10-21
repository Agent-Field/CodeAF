package main

// ── codeaf app: THE SETTINGS WINDOW (wave 8) ────────────────────────────────
//
// Every row the window draws is read from where it is kept and written back
// through that place's own door. The General rows and the pinned models are
// the app's (its state file); the model rows are the profile's crew rows (the
// checks run on the high tier and plans are made on the mastermind tier, the
// same rows `/crew` writes) and its fallback list; the always-allow rules are
// the profile's approval rows; memories are the memory store; the spending
// export is the usage ledger written out as CSV.

import (
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// appMemoryStore is the memory store, or an error where the process keeps
// none (memory off, or no engine in a test).
type appMemoryStore func() (*store.Store, error)

// settingsRow reads one profile row's value, and "" for a row this build lacks.
func settingsRow(profile, key string) string {
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(key)
	if !ok {
		return ""
	}
	return strings.TrimSpace(row.Value())
}

// applyRow writes one profile row, refusing what the row refuses.
func applyRow(profile, key, raw string) error {
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(key)
	if !ok {
		return appwire.Errorf(appwire.CodeEngine, "this build has no %s setting", key)
	}
	if err := row.Apply(raw); err != nil {
		return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	return nil
}

// tierShown is a crew row as the window shows it: the model, or empty where
// the row follows the default or the router's pick.
func tierShown(value string) string {
	if value == config.CrewAuto {
		return ""
	}
	return value
}

// settingsExtra fills the wave 8 rows of settings.
func settingsExtra(settings *appwire.Settings, file appStateFile, profile string) {
	settings.Appearance, settings.TextSize = appwire.AppearanceSystem, appwire.TextMedium
	if file.Appearance != "" {
		settings.Appearance = file.Appearance
	}
	if file.TextSize != "" {
		settings.TextSize = file.TextSize
	}
	settings.OpenAtLogin = file.OpenAtLogin != nil && *file.OpenAtLogin
	settings.DockBadge = file.DockBadge == nil || *file.DockBadge
	settings.PinnedModels = append([]string{}, file.PinnedModels...)
	settings.ChecksModel = tierShown(settingsRow(profile, config.KeyTierHighModel))
	settings.PlannerModel = tierShown(settingsRow(profile, config.KeyTierMastermindModel))
	if fallbacks := settingsRow(profile, config.KeyModelFallbacks); fallbacks != "" {
		first, _, _ := strings.Cut(fallbacks, ",")
		settings.FallbackModel = strings.TrimSpace(first)
	}
}

// applyExtra writes the wave 8 rows a patch names: the profile's rows first,
// because they can refuse, then the app's own.
func (b *appBackend) applyExtra(patch appwire.SettingsPatch, profile string) error {
	for _, row := range []struct {
		key   string
		value *string
	}{
		{config.KeyTierHighModel, patch.ChecksModel},
		{config.KeyTierMastermindModel, patch.PlannerModel},
		{config.KeyModelFallbacks, patch.FallbackModel},
	} {
		if row.value == nil {
			continue
		}
		raw := *row.value
		if raw == "" && row.key != config.KeyModelFallbacks {
			raw = config.CrewAuto
		}
		if err := applyRow(profile, row.key, raw); err != nil {
			return err
		}
	}
	if patch.Appearance == nil && patch.TextSize == nil && patch.OpenAtLogin == nil && patch.DockBadge == nil {
		return nil
	}
	err := b.state.write(func(f *appStateFile) error {
		if patch.Appearance != nil {
			f.Appearance = *patch.Appearance
		}
		if patch.TextSize != nil {
			f.TextSize = *patch.TextSize
		}
		if patch.OpenAtLogin != nil {
			v := *patch.OpenAtLogin
			f.OpenAtLogin = &v
		}
		if patch.DockBadge != nil {
			v := *patch.DockBadge
			f.DockBadge = &v
		}
		return nil
	})
	if err != nil {
		return appEngineError(err)
	}
	return nil
}

// PinModels keeps the models pinned to ⌥⌘1–3, in order.
func (b *appBackend) PinModels(ids []string) (appwire.Settings, error) {
	err := b.state.write(func(f *appStateFile) error {
		f.PinnedModels = append([]string(nil), ids...)
		return nil
	})
	if err != nil {
		return appwire.Settings{}, appEngineError(err)
	}
	settings, err := b.Settings()
	if err != nil {
		return appwire.Settings{}, err
	}
	b.emit.Emit(appwire.EventSettingsChanged, appwire.SettingsChanged{Settings: settings})
	return settings, nil
}

// Rules sorts the settings list by use without changing policy match order.
func (b *appBackend) Rules() ([]appwire.Rule, error) {
	profile := config.ProfileDir()
	tools, err := config.ParseToolApprovals(config.ToolApprovalsAt(profile))
	if err != nil {
		return nil, appEngineError(err)
	}
	var rules []appwire.Rule
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		rules = append(rules, appwire.Rule{ID: appwire.RuleTool + ":" + name, Kind: appwire.RuleTool, Tool: name, Action: tools[name]})
	}
	bash, err := config.ParseBashApprovals(config.BashApprovalsAt(profile))
	if err != nil {
		return nil, appEngineError(err)
	}
	for _, rule := range bash {
		rules = append(rules, appwire.Rule{ID: appwire.RuleBash + ":" + rule.Match, Kind: appwire.RuleBash, Tool: "bash", Match: rule.Match, Action: rule.Action})
	}
	metadataRows, err := config.RuleMetadataAllAt(profile)
	if err != nil {
		return nil, appEngineError(err)
	}
	for i := range rules {
		rule := &rules[i]
		scope := rule.Tool
		if rule.Kind == appwire.RuleBash {
			scope = rule.Match
		}
		rule.Text = config.ApprovalRuleText(scope, rule.Action)
		metadata := metadataRows[rule.ID]
		rule.Origin, rule.Uses, rule.LastUsed = metadata.Origin, metadata.Uses, metadata.LastUsed
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].LastUsed.After(rules[j].LastUsed) })
	return rules, nil
}

// removeRule takes one rule out of its row, leaving the other rules in place.
func (b *appBackend) removeRule(id string) error {
	profile := config.ProfileDir()
	kind, name, ok := strings.Cut(id, ":")
	if !ok || name == "" {
		return appwire.Errorf(appwire.CodeBadParams, "%s is not a rule id", id)
	}
	switch kind {
	case appwire.RuleTool:
		tools, err := config.ParseToolApprovals(config.ToolApprovalsAt(profile))
		if err != nil {
			return appEngineError(err)
		}
		if _, held := tools[name]; !held {
			return appwire.Errorf(appwire.CodeNotFound, "there is no rule for %s", name)
		}
		delete(tools, name)
		names := make([]string, 0, len(tools))
		for n := range tools {
			names = append(names, n)
		}
		sort.Strings(names)
		pairs := make([]string, 0, len(names))
		for _, n := range names {
			pairs = append(pairs, n+":"+tools[n])
		}
		return applyRow(profile, config.KeyToolApprovals, strings.Join(pairs, ", "))
	case appwire.RuleBash:
		rules, err := config.ParseBashApprovals(config.BashApprovalsAt(profile))
		if err != nil {
			return appEngineError(err)
		}
		kept := rules[:0]
		found := false
		for _, rule := range rules {
			if rule.Match == name && !found {
				found = true
				continue
			}
			kept = append(kept, rule)
		}
		if !found {
			return appwire.Errorf(appwire.CodeNotFound, "there is no rule for %s", name)
		}
		return applyRow(profile, config.KeyBashApprovals, config.FormatBashApprovals(kept))
	}
	return appwire.Errorf(appwire.CodeBadParams, "%s is not a rule id", id)
}

// Memories is what codeaf remembers, newest first.
func (b *appBackend) Memories(limit int) ([]appwire.Memory, error) {
	brain, err := b.memoryOf()
	if err != nil {
		return nil, err
	}
	rows, err := brain.ListMemories(nil, limit)
	if err != nil {
		return nil, appEngineError(err)
	}
	out := make([]appwire.Memory, 0, len(rows))
	for _, m := range rows {
		out = append(out, appwire.Memory{ID: m.ID, Title: m.Title, Text: m.Text, Owner: m.Owner, Project: m.ProjectName, Origin: m.Origin})
	}
	return out, nil
}

// ForgetMemory forgets one memory.
func (b *appBackend) ForgetMemory(id string) error {
	brain, err := b.memoryOf()
	if err != nil {
		return err
	}
	if err := brain.ForgetMemory(id); err != nil {
		return appEngineError(err)
	}
	return nil
}

// memoryOf is the memory store, or the refusal that says there is none.
func (b *appBackend) memoryOf() (*store.Store, error) {
	if b.memoryStore == nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "memory is off")
	}
	brain, err := b.memoryStore()
	if err != nil || brain == nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "memory is off")
	}
	return brain, nil
}

// ExportSpend writes every call billed in [from, to) as CSV into the app's
// exports folder, and answers the file's path.
func (b *appBackend) ExportSpend(from, to time.Time) (string, error) {
	lines, err := session.ReadUsage(session.UsageLedgerPath(), from)
	if err != nil {
		return "", appEngineError(err)
	}
	dir := filepath.Join(home.Dir(), appStateFolder, "exports")
	if err := os.MkdirAll(dir, appStateFolderMode); err != nil {
		return "", appEngineError(err)
	}
	path := filepath.Join(dir, "spending-"+from.Format("2006-01-02")+"-to-"+to.AddDate(0, 0, -1).Format("2006-01-02")+".csv")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, appStateFileMode)
	if err != nil {
		return "", appEngineError(err)
	}
	w := csv.NewWriter(file)
	_ = w.Write([]string{"at", "day", "conversation", "model", "role", "seat", "calls", "input_tokens", "output_tokens", "usd"})
	for _, line := range lines {
		if !line.At.Before(to) || line.At.Before(from) {
			continue
		}
		_ = w.Write([]string{
			line.At.Format(time.RFC3339), line.Day, line.Session, line.Model, line.Role, string(line.Seat),
			strconv.Itoa(line.Calls), strconv.Itoa(line.Input), strconv.Itoa(line.Output), strconv.FormatFloat(line.USD, 'f', -1, 64),
		})
	}
	w.Flush()
	if err := errors.Join(w.Error(), file.Close()); err != nil {
		return "", appEngineError(err)
	}
	return path, nil
}

// appRemovedRule keeps only the five-second Undo window, not a trash history.
type appRemovedRule struct {
	rule  appwire.Rule
	index int
	until time.Time
}

const appRuleUndoWindow = 5 * time.Second

func (b *appBackend) ruleOrigin(a *appAgent) config.RuleOrigin {
	var origin config.RuleOrigin
	b.locked(func() {
		origin = config.RuleOrigin{Agent: a.view.Title, Place: filepath.Base(a.view.Workspace), At: b.now()}
	})
	return origin
}

// RemoveRule retains the removed row only for the surface's short Undo window.
func (b *appBackend) RemoveRule(id string) error {
	kind, name, valid := strings.Cut(id, ":")
	if !valid || name == "" || (kind != appwire.RuleTool && kind != appwire.RuleBash) {
		return appwire.Errorf(appwire.CodeBadParams, "%s is not a rule id", id)
	}
	b.ruleGate.Lock()
	defer b.ruleGate.Unlock()
	rules, err := b.Rules()
	if err != nil {
		return err
	}
	var removed *appwire.Rule
	for i := range rules {
		if rules[i].ID == id {
			removed = &rules[i]
			break
		}
	}
	if removed == nil {
		return appwire.Errorf(appwire.CodeNotFound, "there is no rule %s", id)
	}
	index := 0
	if removed.Kind == appwire.RuleBash {
		bash, err := config.ParseBashApprovals(config.BashApprovalsAt(config.ProfileDir()))
		if err != nil {
			return appEngineError(err)
		}
		for i, rule := range bash {
			if rule.Match == removed.Match {
				index = i
				break
			}
		}
	}
	if err = b.removeRule(id); err != nil {
		return err
	}
	if b.removedRules == nil {
		b.removedRules = map[string]appRemovedRule{}
	}
	now := b.now()
	for key, row := range b.removedRules {
		if !now.Before(row.until) {
			delete(b.removedRules, key)
		}
	}
	b.removedRules[id] = appRemovedRule{rule: *removed, index: index, until: now.Add(appRuleUndoWindow)}
	if err := b.refreshRuleGates(); err != nil {
		return appEngineError(err)
	}
	return nil
}

// RestoreRule puts back the removed row at its previous priority. A rule
// written meanwhile is never overwritten by a late Undo.
func (b *appBackend) RestoreRule(id string) error {
	b.ruleGate.Lock()
	defer b.ruleGate.Unlock()
	removed, ok := b.removedRules[id]
	if !ok || !b.now().Before(removed.until) {
		delete(b.removedRules, id)
		return appwire.Errorf(appwire.CodeNotFound, "the rule's Undo time has ended")
	}
	profile := config.ProfileDir()
	rule := removed.rule
	var err error
	if rule.Kind == appwire.RuleTool {
		tools, parseErr := config.ParseToolApprovals(config.ToolApprovalsAt(profile))
		if parseErr != nil {
			return appEngineError(parseErr)
		}
		if _, exists := tools[rule.Tool]; exists {
			return appwire.Errorf(appwire.CodeConflict, "the rule has changed")
		}
		err = config.RememberToolApproval(profile, rule.Tool, rule.Action)
	} else {
		rules, parseErr := config.ParseBashApprovals(config.BashApprovalsAt(profile))
		if parseErr != nil {
			return appEngineError(parseErr)
		}
		for _, row := range rules {
			if row.Match == rule.Match {
				return appwire.Errorf(appwire.CodeConflict, "the rule has changed")
			}
		}
		index := min(removed.index, len(rules))
		rules = append(rules, config.BashRule{})
		copy(rules[index+1:], rules[index:])
		rules[index] = config.BashRule{Match: rule.Match, Action: rule.Action}
		err = applyRow(profile, config.KeyBashApprovals, config.FormatBashApprovals(rules))
	}
	if err != nil {
		return appEngineError(err)
	}
	delete(b.removedRules, id)
	if err := b.refreshRuleGates(); err != nil {
		return appEngineError(err)
	}
	return nil
}

// refreshRuleGates reaches hosted conversations through the same explicit
// refresh used after Always. In-flight calls keep the decision already made.
func (b *appBackend) refreshRuleGates() error {
	var refreshers []interface{ RebuildApprovalGate() error }
	b.locked(func() {
		for _, a := range b.agents {
			if a.live != nil {
				if door, ok := a.live.conn.Conv.(interface{ RebuildApprovalGate() error }); ok {
					refreshers = append(refreshers, door)
				}
			}
		}
	})
	var first error
	for _, door := range refreshers {
		if err := door.RebuildApprovalGate(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func appRuleWhy(why string, rule *config.RuleReceipt) string {
	uses := strconv.FormatUint(rule.Uses, 10) + " times"
	if rule.Uses == 1 {
		uses = "1 time"
	}
	why += " · used " + uses
	if rule.Origin.Agent != "" {
		why += " · From your answer in " + rule.Origin.Agent
	}
	if rule.Origin.Place != "" {
		why += " · " + rule.Origin.Place
	}
	if !rule.Origin.At.IsZero() {
		why += " · " + rule.Origin.At.Format("2 Jan")
	}
	return why
}
