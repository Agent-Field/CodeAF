package appapi

import (
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var permissionContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/permissions/{id}", Request: "PermissionRequest", Response: "ActionResponse"}, handle: (*Server).permissionHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/rules", Request: "", Response: "RulesResponse"}, handle: (*Server).permissionHandler},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/rules/{id}", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).permissionHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/rules/{id}/restore", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).permissionHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/rules/{id}/why", Request: "", Response: "RuleWhyResponse"}, handle: (*Server).permissionHandler},
}

func (s *Server) permissionHandler(w http.ResponseWriter, r *http.Request) {
	if len(r.Header.Values("Idempotency-Key")) > 1 {
		fail(w, 422, "bad_input", "use one Idempotency-Key")
		return
	}
	s.idempotent(s.handlePermission)(w, r)
}
func (s *Server) permissionProfile() string {
	if s.options.ProfileDir != "" {
		return s.options.ProfileDir
	}
	if s.options.Agents != nil {
		return s.options.Agents.home
	}
	return ""
}
func ruleWire(r config.AppPermissionRule) PermissionRule {
	return PermissionRule{ID: r.ID, Match: r.Match, Pattern: r.Pattern, ClassLabel: r.ClassLabel, Who: r.Who, AgentID: r.AgentID, MadeAt: r.MadeAt}
}
func (s *Server) handlePermission(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/app/v1/rules") {
		s.permissionRules(w, r)
		return
	}
	a := s.options.Agents
	if a == nil {
		fail(w, 503, "not_allowed", "the agent registry is unavailable")
		return
	}
	var body PermissionRequest
	if !decodeObject(w, r, &body) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	a.events = s.events
	row, err := a.findAskLocked(r.PathValue("id"))
	if err != nil {
		agentFailure(w, err)
		return
	}
	if row.Permission == nil {
		fail(w, 422, "bad_input", "this ask is not a permission")
		return
	}
	if err := askExpectation(row, body.Expect); err != nil {
		agentFailure(w, err)
		return
	}
	if row.Ask.State != "waiting" {
		askConflict(w, row, a.now())
		return
	}
	decisions := body.Items
	if len(decisions) > 0 && body.Verb != "" {
		fail(w, 422, "bad_input", "give one batch verb or item decisions")
		return
	}
	if len(decisions) == 0 {
		decisions = []PermissionDecision{{ID: row.Ask.ID, Verb: body.Verb}}
		if body.Verb != "always" {
			for _, item := range a.permissionBatchLocked(row) {
				if item.ID != row.Ask.ID {
					decisions = append(decisions, PermissionDecision{ID: item.ID, Verb: body.Verb})
				}
			}
		}
	}
	// Validate every row before releasing any waiting task call.
	seen := map[string]bool{}
	rows := map[string]savedAsk{}
	for _, item := range decisions {
		if seen[item.ID] {
			fail(w, 422, "bad_input", "a permission appears twice")
			return
		}
		seen[item.ID] = true
		target, err := a.findAskLocked(item.ID)
		if err != nil {
			agentFailure(w, err)
			return
		}
		if target.Permission == nil || askAgent(target.Ask.ID) != askAgent(row.Ask.ID) || item.ID != row.Ask.ID && (row.Question.Batch == "" || target.Question.Batch != row.Question.Batch || target.Permission.TaskID != row.Permission.TaskID) {
			fail(w, 422, "bad_input", "the permission is not in this batch")
			return
		}
		if target.Ask.State != "waiting" {
			askConflict(w, target, a.now())
			return
		}
		if item.Verb != "allow_once" && item.Verb != "deny" && item.Verb != "always" {
			fail(w, 422, "bad_input", "verb must be allow_once, deny or always")
			return
		}
		if item.Verb == "always" {
			if target.Ask.Irreversible {
				fail(w, 422, "not_allowed", "irreversible actions always ask")
				return
			}
			if body.Rule == nil || (body.Rule.Match != "exact" && body.Rule.Match != "class") || (body.Rule.Who != "agent" && body.Rule.Who != "all_agents") {
				fail(w, 422, "bad_input", "Always needs a match and who scope")
				return
			}
			if body.Rule.Match == "class" && target.Permission.Class == "" {
				fail(w, 422, "bad_input", "this command has no always-allow class")
				return
			}
			floor, err := config.AppPermissionAlwaysAsk(s.permissionProfile(), target.Tool, target.Args, target.Permission.TaskID != "")
			if err != nil {
				agentFailure(w, err)
				return
			}
			if floor {
				fail(w, 422, "not_allowed", "this action always asks")
				return
			}
		}
		rows[item.ID] = target
	}
	if body.Rule != nil {
		hasAlways := false
		for _, item := range decisions {
			hasAlways = hasAlways || item.Verb == "always"
		}
		if !hasAlways {
			fail(w, 422, "bad_input", "a rule needs the always verb")
			return
		}
	}
	if r.Context().Value(askValidateKey{}) != nil {
		write(w, 200, ActionResponse{OK: true})
		return
	}
	var receipt *DisplayEntry
	for _, item := range decisions {
		target := rows[item.ID]
		answer := session.Answer{Kind: session.QuestionConsent, ID: target.Question.ID, Ref: target.Question.Ref, Ask: target.Question.Ask, Key: "1", From: "app", At: a.now().UTC(), DecidedBy: session.DecidedByPerson}
		if r.Context().Value(inboxForwardKey{}) != nil {
			answer.From = "inbox"
		}
		if item.Verb == "deny" {
			answer.Key = "3"
		}
		var rule *config.AppPermissionRule
		createdRule := false
		if item.Verb == "always" {
			saved, created, err := config.EnsureAppPermissionRule(s.permissionProfile(), askAgent(target.Ask.ID), target.Tool, target.Args, body.Rule.Match, body.Rule.Who, target.Ask.ID, a.now())
			if err != nil {
				fail(w, 422, "bad_input", err.Error())
				return
			}
			rule = &saved
			createdRule = created
			answer.Why = "rule:" + saved.ID
			// ScopeOnce prevents a coarse session memo from surviving rule removal.
		}
		if err := a.resolveAskLocked(target, answer); err != nil {
			if createdRule {
				_, _ = config.ChangeAppPermissionRule(s.permissionProfile(), rule.ID, true)
			}
			agentFailure(w, err)
			return
		}
		resolved, _ := a.findAskLocked(target.Ask.ID)
		receipt = resolved.Ask.Receipt
		if createdRule {
			_, _ = s.events.Publish(EventRuleCreated, IDChangedEvent{ID: rule.ID})
		}
	}
	write(w, 200, ActionResponse{OK: true, Receipt: receipt})
}

func (a *AgentRegistry) permissionBatchLocked(row savedAsk) []PermissionCommand {
	out := []PermissionCommand{}
	if row.Permission == nil || row.Permission.TaskID == "" || row.Question.Batch == "" {
		return out
	}
	_ = a.asks(false, func(d *askDisk) error {
		for _, s := range d.Asks {
			if s.Ask.State == "waiting" && s.Permission != nil && s.Permission.AgentID == row.Permission.AgentID && s.Permission.TaskID == row.Permission.TaskID && s.Question.Batch == row.Question.Batch {
				out = append(out, PermissionCommand{ID: s.Ask.ID, Command: s.Permission.Command})
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (s *Server) permissionRules(w http.ResponseWriter, r *http.Request) {
	profile := s.permissionProfile()
	if profile == "" {
		fail(w, 503, "not_allowed", "the permission profile is unavailable")
		return
	}
	rows, err := config.AppPermissionRules(profile, true)
	if err != nil {
		fail(w, 500, "conflict", "could not read saved permission rules")
		return
	}
	if r.URL.Path == "/app/v1/rules" {
		out := RulesResponse{Rules: []PermissionRule{}}
		for _, row := range rows {
			if !row.Removed && permissionRuleMatches(row, r.URL.Query().Get("q")) {
				out.Rules = append(out.Rules, ruleWire(row))
			}
		}
		write(w, 200, out)
		return
	}
	id := r.PathValue("id")
	var rule *config.AppPermissionRule
	for _, row := range rows {
		if row.ID == id {
			rule = &row
			break
		}
	}
	if rule == nil {
		fail(w, 404, "not_found", "no such permission rule")
		return
	}
	if r.Method == "GET" {
		write(w, 200, RuleWhyResponse{Rule: ruleWire(*rule), Matched: PermissionCommand{ID: rule.MatchedID, Command: rule.MatchedCommand}})
		return
	}
	var body ExpectRequest
	if !decodeObject(w, r, &body) {
		return
	}
	state := "active"
	if rule.Removed {
		state = "removed"
	}
	if body.Expect != nil && (body.Expect.Version != nil || body.Expect.State != nil && *body.Expect.State != state) {
		fail(w, 409, "stale", "this rule changed; read it again before acting")
		return
	}
	removed := r.Method == "DELETE"
	var expected []bool
	if body.Expect != nil && body.Expect.State != nil {
		expected = []bool{rule.Removed}
	}
	changed, err := config.ChangeAppPermissionRule(profile, id, removed, expected...)
	if errors.Is(err, config.ErrAppStale) {
		fail(w, 409, "stale", "this rule changed; read it again before acting")
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		fail(w, 404, "not_found", "no such permission rule")
		return
	}
	if err != nil {
		fail(w, 500, "conflict", "could not save permission rule")
		return
	}
	_, _ = s.events.Publish(EventRuleChanged, IDChangedEvent{ID: changed.ID})
	text := "Permission rule restored"
	if removed {
		text = "Permission rule removed"
	}
	at := time.Now().UTC()
	if a := s.options.Agents; a != nil {
		a.mu.Lock()
		at = a.now().UTC()
		a.mu.Unlock()
	}
	write(w, 200, ActionResponse{OK: true, Receipt: &DisplayEntry{Kind: "receipt", Role: "note", Text: text, By: "you", At: &at, Link: &SourceRef{Kind: "rule", ID: id}}})
}

// Rule search follows the same plain field tokens as other app lists.
func permissionRuleMatches(row config.AppPermissionRule, q string) bool {
	text := strings.ToLower(row.Pattern + " " + row.ClassLabel + " " + row.AgentID)
	for _, token := range strings.Fields(strings.ToLower(q)) {
		key, value, field := strings.Cut(token, ":")
		if !field {
			if !strings.Contains(text, token) {
				return false
			}
			continue
		}
		switch key {
		case "match":
			if row.Match != value {
				return false
			}
		case "who":
			if row.Who != value {
				return false
			}
		case "agent":
			if row.AgentID != value {
				return false
			}
		default:
			return false
		}
	}
	return true
}
