package approles

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type roleRow struct {
	member      string
	role        svc.Role
	setBy       string
	at          time.Time
	maker       bool
	makerMember string
}

type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readRoles(ctx context.Context, db querier, app string) ([]roleRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT r.member, r.role, r.set_by, r.at, r.member = COALESCE(m.member, ''), COALESCE(m.member, '') FROM app_roles r LEFT JOIN app_role_makers m ON m.app = r.app WHERE r.app = ? ORDER BY r.member`, app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []roleRow{}
	for rows.Next() {
		var row roleRow
		var at string
		if err := rows.Scan(&row.member, &row.role, &row.setBy, &at, &row.maker, &row.makerMember); err != nil {
			return nil, err
		}
		row.at, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Service) roster(ctx context.Context, caller svc.Caller) ([]bridge.Member, error) {
	if s.all == nil || s.all.Members == nil {
		return nil, nil
	}
	list, err := s.all.Members.List(ctx, caller)
	return list.Members, err
}

func membersView(rows []roleRow, roster []bridge.Member) bridge.AppMembers {
	result := bridge.AppMembers{People: []bridge.AppMember{}}
	people := map[string]bridge.AppMember{}
	maker := ""
	known := map[string]bool{}
	for _, member := range roster {
		known[member.ID] = true
	}
	for _, row := range rows {
		maker = row.makerMember
		if row.member == "*" {
			result.Everyone = string(row.role)
			continue
		}
		if len(roster) > 0 && !known[row.member] {
			continue
		}
		at := row.at
		people[row.member] = bridge.AppMember{Member: row.member, Role: string(row.role), From: "set", SetBy: row.setBy, At: &at, Maker: row.maker}
	}
	for _, member := range roster {
		if _, set := people[member.ID]; !set && result.Everyone != "" {
			people[member.ID] = bridge.AppMember{Member: member.ID, Role: result.Everyone, From: "everyone", Maker: member.ID == maker}
		}
	}
	for _, person := range people {
		result.People = append(result.People, person)
	}
	sort.Slice(result.People, func(i, j int) bool { return result.People[i].Member < result.People[j].Member })
	return result
}

func (s *Service) List(ctx context.Context, scope svc.Scope) (bridge.AppMembers, error) {
	if err := checkScope(scope); err != nil {
		return bridge.AppMembers{}, err
	}
	if !scope.Caller.Role.AtLeast(svc.RoleViewer) && !scope.Caller.IsAdmin() {
		return bridge.AppMembers{}, bridge.Errorf(bridge.CodeDenied, "the app role does not allow Members")
	}
	if err := s.open(); err != nil {
		return bridge.AppMembers{}, err
	}
	rows, err := readRoles(ctx, s.db, scope.App)
	if err != nil {
		return bridge.AppMembers{}, err
	}
	roster, err := s.roster(ctx, scope.Caller)
	if err != nil {
		return bridge.AppMembers{}, err
	}
	if len(rows) == 0 && personal(scope.Caller) {
		return bridge.AppMembers{Everyone: string(svc.RoleUser), People: []bridge.AppMember{{Member: scope.Caller.Member, Role: string(svc.RoleEditor), From: "set", Maker: true}}}, nil
	}
	return membersView(rows, roster), nil
}

func checkScope(scope svc.Scope) error {
	if scope.App == "" {
		return bridge.Errorf(bridge.CodeInvalid, "this method needs app")
	}
	if scope.Caller.Member == "" {
		return bridge.Errorf(bridge.CodeDenied, "a member is required")
	}
	return nil
}

func personal(caller svc.Caller) bool { return caller.Space == "" || caller.Space == svc.PersonalSpace }

func validateSet(scope svc.Scope, p bridge.AppRoleSetParams) error {
	if err := checkScope(scope); err != nil {
		return err
	}
	if scope.Caller.Agent || (!scope.Caller.CanShape() && !scope.Caller.IsAdmin()) {
		return bridge.Errorf(bridge.CodeDenied, "only Editors or workspace Owner or Admin can change Members")
	}
	if p.Member == "" {
		return bridge.Errorf(bridge.CodeInvalid, "member is required")
	}
	if p.Role == nil {
		if p.Member == "*" {
			return bridge.Errorf(bridge.CodeInvalid, "Everyone needs a role")
		}
		return nil
	}
	if !svc.Role(*p.Role).AtLeast(svc.RoleViewer) {
		return bridge.Errorf(bridge.CodeInvalid, "role must be editor, user or viewer")
	}
	return nil
}

func (s *Service) Set(ctx context.Context, scope svc.Scope, p bridge.AppRoleSetParams) (bridge.AppMembers, error) {
	if err := validateSet(scope, p); err != nil {
		return bridge.AppMembers{}, err
	}
	if s.all == nil || s.all.Audit == nil {
		return bridge.AppMembers{}, bridge.NotBuilt("approles audit")
	}
	roster, err := s.roster(ctx, scope.Caller)
	if err != nil {
		return bridge.AppMembers{}, err
	}
	if len(roster) > 0 && p.Member != "*" && p.Role != nil && !rosterContains(roster, p.Member) {
		return bridge.AppMembers{}, bridge.Errorf(bridge.CodeInvalid, "the person must belong to the workspace")
	}
	name, _, err := s.appInfo(ctx, scope.App)
	if err != nil {
		return bridge.AppMembers{}, err
	}
	if err := s.open(); err != nil {
		return bridge.AppMembers{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.setRow(ctx, scope, p, roster, name)
	if err != nil {
		return bridge.AppMembers{}, err
	}
	// Publish committed membership even when the independent audit append fails.
	if s.all.Hub != nil {
		s.all.Hub.Publish(bridge.TopicRoles(scope.App), result)
	}
	detail, _ := json.Marshal(p)
	_, err = s.all.Audit.Append(ctx, bridge.AuditEntry{Space: scope.Caller.Space, App: scope.App, Member: scope.Caller.Member, Action: bridge.MethodAppRolesSet, Object: "member/" + p.Member, Decision: "allow", Detail: detail, Activity: &bridge.ActivityLine{Text: roleActivity(scope.Caller.Member, p, name), View: "members"}})
	return result, err
}

func (s *Service) setRow(ctx context.Context, scope svc.Scope, p bridge.AppRoleSetParams, roster []bridge.Member, name string) (bridge.AppMembers, error) {
	// BEGIN IMMEDIATE serializes the last-Editor check across separate service instances.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return bridge.AppMembers{}, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return bridge.AppMembers{}, err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
	if p.Role == nil {
		_, err = conn.ExecContext(ctx, `DELETE FROM app_roles WHERE app = ? AND member = ?`, scope.App, p.Member)
	} else {
		_, err = conn.ExecContext(ctx, `INSERT INTO app_roles (app, member, role, set_by, at) VALUES (?, ?, ?, ?, ?) ON CONFLICT (app, member) DO UPDATE SET role = excluded.role, set_by = excluded.set_by, at = excluded.at`, scope.App, p.Member, *p.Role, scope.Caller.Member, s.now().UTC().Format(time.RFC3339Nano))
	}
	if err != nil {
		return bridge.AppMembers{}, err
	}
	rows, err := readRoles(ctx, conn, scope.App)
	if err != nil {
		return bridge.AppMembers{}, err
	}
	result := membersView(rows, roster)
	if !hasEditor(result) {
		return bridge.AppMembers{}, bridge.Errorf(bridge.CodeConflict, fmt.Sprintf("%s needs at least one Editor", name))
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return bridge.AppMembers{}, err
	}
	return result, nil
}

func hasEditor(members bridge.AppMembers) bool {
	for _, person := range members.People {
		if person.Role == string(svc.RoleEditor) {
			return true
		}
	}
	return false
}

func roleActivity(actor string, p bridge.AppRoleSetParams, name string) string {
	who := "@" + strings.TrimPrefix(p.Member, "@")
	if p.Member == "*" {
		who = "Everyone"
	}
	if p.Role == nil {
		return fmt.Sprintf("@%s reset %s to Everyone in %s", strings.TrimPrefix(actor, "@"), who, name)
	}
	titles := map[string]string{"editor": "Editor", "user": "User", "viewer": "Viewer"}
	return fmt.Sprintf("@%s made %s %s of %s", strings.TrimPrefix(actor, "@"), who, titles[*p.Role], name)
}

func (s *Service) appInfo(ctx context.Context, app string) (string, string, error) {
	if s.all == nil || s.all.Store == nil {
		return app, "", nil
	}
	detail, err := s.all.Store.Get(ctx, app)
	if err != nil {
		return "", "", err
	}
	name := detail.Name
	if name == "" {
		name = app
	}
	// Reading the summary's wire spelling will pick up shaped_by when Store adds it.
	raw, err := json.Marshal(detail.AppSummary)
	if err != nil {
		return "", "", err
	}
	var summary struct {
		ShapedBy string `json:"shaped_by"`
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		return "", "", err
	}
	return name, summary.ShapedBy, nil
}

func rosterContains(roster []bridge.Member, member string) bool {
	for _, person := range roster {
		if person.ID == member {
			return true
		}
	}
	return false
}
