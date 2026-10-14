package approles

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// EditorRequest is the durable audit detail consumed by the member's Inbox projection.
type EditorRequest struct {
	Member string `json:"member"`
	From   string `json:"from"`
	App    string `json:"app"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
}

// Waiting identifies the member whose space switcher now needs attention.
type Waiting struct {
	Member string `json:"member"`
	Count  int    `json:"count"`
}

func (s *Service) AskEditor(ctx context.Context, scope svc.Scope, p bridge.AskEditorParams) (bridge.Empty, error) {
	if err := checkScope(scope); err != nil {
		return bridge.Empty{}, err
	}
	if scope.Caller.Agent || (scope.Caller.Role != svc.RoleUser && scope.Caller.Role != svc.RoleViewer) {
		return bridge.Empty{}, bridge.Errorf(bridge.CodeDenied, "only Users and Viewers ask an Editor")
	}
	if strings.TrimSpace(p.Text) == "" || (p.Kind != "shape" && p.Kind != "role") {
		return bridge.Empty{}, bridge.Errorf(bridge.CodeInvalid, "text and kind shape or role are required")
	}
	if s.all == nil || s.all.Audit == nil || s.all.Hub == nil {
		return bridge.Empty{}, bridge.NotBuilt("approles Inbox")
	}
	name, last, err := s.appInfo(ctx, scope.App)
	if err != nil {
		return bridge.Empty{}, err
	}
	members, err := s.List(ctx, scope)
	if err != nil {
		return bridge.Empty{}, err
	}
	editor := chooseEditor(members, last)
	if editor == "" {
		return bridge.Empty{}, bridge.Errorf(bridge.CodeConflict, fmt.Sprintf("%s needs at least one Editor", name))
	}
	request := EditorRequest{Member: editor, From: scope.Caller.Member, App: scope.App, Kind: p.Kind, Text: p.Text}
	detail, err := json.Marshal(request)
	if err != nil {
		return bridge.Empty{}, err
	}
	verb := "shape"
	if p.Kind == "role" {
		verb = "change their role in"
	}
	_, err = s.all.Audit.Append(ctx, bridge.AuditEntry{Space: scope.Caller.Space, App: scope.App, Member: scope.Caller.Member, Action: bridge.MethodAppRolesAsk, Object: "member/" + editor, Decision: "allow", Detail: detail, Activity: &bridge.ActivityLine{Text: fmt.Sprintf("@%s asks @%s to %s %s %q", strings.TrimPrefix(scope.Caller.Member, "@"), strings.TrimPrefix(editor, "@"), verb, name, p.Text), View: "shape"}})
	if err != nil {
		return bridge.Empty{}, err
	}
	s.all.Hub.Publish(bridge.TopicSpaceWaiting, Waiting{Member: editor, Count: 1})
	return bridge.Empty{}, nil
}

func chooseEditor(members bridge.AppMembers, last string) string {
	fallback := ""
	for _, person := range members.People {
		if person.Role != string(svc.RoleEditor) {
			continue
		}
		if person.Member == last {
			return last
		}
		if fallback == "" {
			fallback = person.Member
		}
	}
	return fallback
}
