package caps

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service checks the member's role before consulting the app's capabilities.
type Service struct {
	all   *svc.Services
	audit auditLog
}

var _ svc.Caps = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service {
	return &Service{all: all, audit: auditLog{root: cfg.Root}}
}

// Allow is also the capability door for runs and the app agent's tools.
func (s *Service) Allow(ctx context.Context, scope svc.Scope, method string, params json.RawMessage) error {
	err := s.allow(ctx, scope, method, params)
	s.audit.append(scope, method, err)
	return err
}

func (s *Service) allow(ctx context.Context, scope svc.Scope, method string, params json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if scope.Caller.Member == "" {
		return denied("a member is required")
	}
	if handled, err := checkSpaces(scope, method, params); handled {
		return err
	}
	if err := allowRole(scope.Caller, method); err != nil {
		return err
	}
	if len(params) != 0 && string(params) != "null" && (!json.Valid(params) || !strings.HasPrefix(strings.TrimSpace(string(params)), "{")) {
		return bridge.Errorf(bridge.CodeInvalid, "params must be an object")
	}
	if method == bridge.MethodAppsList || method == bridge.MethodAppsCreate {
		return nil
	}
	if scope.App == "" {
		return bridge.Errorf(bridge.CodeInvalid, "this method needs app")
	}
	if definitionMethod(method) || method == bridge.MethodAppsGet {
		return nil
	}
	if method == "sub" || method == "unsub" {
		return s.allowTopic(ctx, scope, params)
	}
	bundle, err := s.all.Store.Definition(ctx, scope.App)
	if err != nil {
		return err
	}
	if bundle == nil || bundle.App == nil {
		return denied("the app has no capabilities yet")
	}
	return allowCapability(bundle, method, params)
}

func definitionMethod(method string) bool {
	switch method {
	case bridge.MethodAppsCreate, bridge.MethodAppsKeep, bridge.MethodAppsDiscard,
		bridge.MethodBuilderPlanEdit, bridge.MethodBuilderWrite, bridge.MethodBuilderCancel:
		return true
	}
	return false
}

func allowRole(caller svc.Caller, method string) error {
	if caller.Agent && (definitionMethod(method) || method == bridge.MethodAppsList || method == bridge.MethodAppsGet) {
		return denied("the app agent cannot change or read app definitions")
	}
	switch caller.Role {
	case svc.RoleEditor:
		return nil
	case svc.RoleUser:
		if !definitionMethod(method) {
			return nil
		}
	case svc.RoleViewer:
		switch method {
		case bridge.MethodAppsList, bridge.MethodAppsGet, bridge.MethodRecordsQuery, bridge.MethodRecordsGet,
			bridge.MethodRunsGet, bridge.MethodRunsList, bridge.MethodApprovalsList, "sub", "unsub":
			return nil
		}
	}
	return denied("the app role does not allow this call")
}

func allowCapability(b *spec.Bundle, method string, raw json.RawMessage) error {
	switch method {
	case bridge.MethodRecordsQuery, bridge.MethodRecordsGet, bridge.MethodRecordsCreate, bridge.MethodRecordsUpdate, bridge.MethodRecordsDelete:
		var p struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(object(raw), &p); err != nil {
			return bridge.Errorf(bridge.CodeInvalid, "record type must be a string")
		}
		if p.Type == "" {
			return bridge.Errorf(bridge.CodeInvalid, "record type is required")
		}
		write := method != bridge.MethodRecordsQuery && method != bridge.MethodRecordsGet
		return allowRecord(b.App, p.Type, write)
	case bridge.MethodRunsStart:
		var p bridge.StartRunParams
		if err := json.Unmarshal(object(raw), &p); err != nil || p.Run == "" {
			return bridge.Errorf(bridge.CodeInvalid, "run is required")
		}
		if b.Runs[p.Run] == nil {
			return bridge.Errorf(bridge.CodeNotFound, "no run "+p.Run)
		}
		return nil
	case bridge.MethodRunsGet, bridge.MethodRunsList, bridge.MethodApprovalsList, bridge.MethodApprovalsDecide, bridge.MethodAgentSend:
		return nil
	}
	if verb, ok := strings.CutPrefix(method, "ai."); ok {
		if slices.Contains(b.App.Capabilities.AI, verb) {
			return nil
		}
		return denied("the app does not allow " + method)
	}
	connector, verb, ok := strings.Cut(method, ".")
	if ok && slices.Contains(b.App.Capabilities.Connectors[connector].Can, verb) {
		return nil
	}
	if slices.Contains(b.App.Capabilities.Tools, method) {
		return nil
	}
	return denied("the app does not allow " + method)
}

func allowRecord(app *spec.App, recordType string, write bool) error {
	access := app.Capabilities.Records[recordType]
	if app.Records[recordType] == nil || (access != spec.AccessRead && access != spec.AccessReadWrite) {
		return denied("the app cannot read records of type " + recordType)
	}
	if write && access != spec.AccessReadWrite {
		return denied("the app cannot write records of type " + recordType)
	}
	return nil
}

func (s *Service) allowTopic(ctx context.Context, scope svc.Scope, raw json.RawMessage) error {
	var p struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(object(raw), &p); err != nil {
		return bridge.Errorf(bridge.CodeInvalid, "topic must be a string")
	}
	parts := strings.Split(p.Topic, "/")
	if len(parts) < 3 || parts[0] != "app" || parts[1] != scope.App {
		return bridge.Errorf(bridge.CodeInvalid, "topic must belong to this app")
	}
	if len(parts) == 3 {
		switch parts[2] {
		case "build", "state", "runs", "approvals":
			return nil
		case "*":
			return s.allowAllRecords(ctx, scope)
		}
	}
	if len(parts) == 4 && parts[3] != "" && !strings.ContainsAny(parts[3], "*\\") {
		switch parts[2] {
		case "agent":
			return nil
		case "records":
			b, err := s.all.Store.Definition(ctx, scope.App)
			if err != nil {
				return err
			}
			if b == nil || b.App == nil {
				return denied("the app has no capabilities yet")
			}
			return allowRecord(b.App, parts[3], false)
		}
	}
	if len(parts) == 4 && parts[2] == "records" && parts[3] == "*" {
		return s.allowAllRecords(ctx, scope)
	}
	return bridge.Errorf(bridge.CodeInvalid, "unknown app topic")
}

func (s *Service) allowAllRecords(ctx context.Context, scope svc.Scope) error {
	b, err := s.all.Store.Definition(ctx, scope.App)
	if err != nil {
		return err
	}
	if b == nil || b.App == nil {
		return denied("the app has no capabilities yet")
	}
	for recordType := range b.App.Records {
		if err := allowRecord(b.App, recordType, false); err != nil {
			return err
		}
	}
	return nil
}

func object(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`)
	}
	return raw
}
func denied(message string) error { return bridge.Errorf(bridge.CodeDenied, message) }
func errorCode(err error) bridge.ErrorCode {
	if err == nil {
		return "allowed"
	}
	var typed *bridge.Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	return bridge.CodeInternal
}
