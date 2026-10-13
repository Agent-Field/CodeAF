package grants

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) Accounts(ctx context.Context, caller svc.Caller) (bridge.AccountsResult, error) {
	out := bridge.AccountsResult{Connections: []bridge.Connection{}, Grants: []bridge.Grant{}}
	if err := s.ready(); err != nil {
		return out, err
	}
	if s.space() == "personal" {
		connections, err := s.connections.Personal(ctx, caller.Member)
		if err != nil {
			return out, err
		}
		out.Connections = connections
	}
	rows, err := s.db.QueryContext(ctx, `SELECT app,space,service FROM grants WHERE member=? ORDER BY space,app,service`, caller.Member)
	if err != nil {
		return out, err
	}
	type key struct{ app, space, service string }
	keys := []key{}
	for rows.Next() {
		var k key
		if err = rows.Scan(&k.app, &k.space, &k.service); err != nil {
			_ = rows.Close()
			return out, err
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return out, err
	}
	for _, k := range keys {
		g, err := s.grant(ctx, caller.Member, k.app, k.space, k.service)
		if err != nil {
			return out, err
		}
		out.Grants = append(out.Grants, g)
	}
	return out, nil
}

func (s *Service) Allow(ctx context.Context, scope svc.Scope, p bridge.AllowParams) (bridge.Grant, error) {
	g := bridge.Grant{}
	if err := s.ready(); err != nil {
		return g, err
	}
	app, need, err := s.connector(ctx, scope, p.Service)
	if err != nil {
		return g, err
	}
	if need.As != "member" {
		return g, bridge.Errorf(bridge.CodeInvalid, "Only personal connections use Allow")
	}
	if scope.Caller.Member == "" || scope.Caller.Agent {
		return g, bridge.Errorf(bridge.CodeDenied, "Only the person can answer Allow")
	}
	g = bridge.Grant{Member: scope.Caller.Member, App: scope.App, AppName: app.Name, Space: s.space(), Service: p.Service, Can: append([]string{}, need.Can...)}
	switch p.Answer {
	case "not_now":
		sheet, err := s.AllowSheet(ctx, scope, bridge.AllowSheetParams{Service: p.Service})
		if err != nil {
			return g, err
		}
		s.publish(bridge.TopicConnectAllow(scope.Caller.Member), sheet)
		return g, nil
	case "allow", "refresh":
	default:
		return g, bridge.Errorf(bridge.CodeInvalid, "Answer must be allow, not_now or refresh")
	}
	if s.space() == "personal" {
		if p.Credential != nil {
			return g, bridge.Errorf(bridge.CodeInvalid, "Personal uses your accounts directly")
		}
		_, err = s.personal(ctx, scope, p.Service)
		return g, err
	}
	g, err = s.saveGrant(ctx, g, p)
	if err != nil {
		return g, err
	}
	if err = s.audit(ctx, scope.Caller, g.App, "connector.allow", g.Service, p.Answer, ""); err != nil {
		return g, err
	}
	// Runs can resume by observing this member's grant update without polling secrets.
	sheet, err := s.AllowSheet(ctx, scope, bridge.AllowSheetParams{Service: p.Service})
	if err != nil {
		return g, err
	}
	s.publish(bridge.TopicConnectAllow(g.Member), sheet)
	return g, nil
}

func (s *Service) saveGrant(ctx context.Context, g bridge.Grant, p bridge.AllowParams) (bridge.Grant, error) {
	if !validCredential(p.Credential) {
		return g, bridge.Errorf(bridge.CodeInvalid, "A workspace Allow requires a credential")
	}
	if expired(*p.Credential, s.now()) {
		return g, reason(bridge.CodeConflict, bridge.ReasonExpired, "Reconnect this account")
	}
	encrypted, err := s.seal(*p.Credential, grantBinding(g.Member, g.App, g.Space, g.Service))
	if err != nil {
		return g, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return g, err
	}
	defer tx.Rollback()
	if p.Answer == "allow" {
		at := s.now().UTC().Format(time.RFC3339)
		_, err = tx.ExecContext(ctx, `INSERT INTO grants(member,app,space,service,at) VALUES(?,?,?,?,?) ON CONFLICT(member,app,space,service) DO NOTHING`, g.Member, g.App, g.Space, g.Service, at)
		if err != nil {
			return g, err
		}
	}
	err = tx.QueryRowContext(ctx, `SELECT at FROM grants WHERE member=? AND app=? AND space=? AND service=?`, g.Member, g.App, g.Space, g.Service).Scan(&g.At)
	if errors.Is(err, sql.ErrNoRows) {
		return g, reason(bridge.CodeConflict, bridge.ReasonNeedsAllow, "Allow this app first")
	}
	if err != nil {
		return g, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO grant_credentials(member,app,space,service,credential) VALUES(?,?,?,?,?) ON CONFLICT(member,app,space,service) DO UPDATE SET credential=excluded.credential`, g.Member, g.App, g.Space, g.Service, encrypted)
	if err != nil {
		return g, err
	}
	return g, tx.Commit()
}

func (s *Service) AllowSheet(ctx context.Context, scope svc.Scope, p bridge.AllowSheetParams) (bridge.AllowSheet, error) {
	sheet := bridge.AllowSheet{}
	if err := s.ready(); err != nil {
		return sheet, err
	}
	app, need, err := s.connector(ctx, scope, p.Service)
	if err != nil {
		return sheet, err
	}
	if need.As != "member" {
		return sheet, bridge.Errorf(bridge.CodeInvalid, "Workspace connections do not use Allow")
	}
	sheet = bridge.AllowSheet{App: scope.App, AppName: app.Name, Member: scope.Caller.Member, MemberName: s.spaces.NameOf(scope.Caller), Service: p.Service, Can: []string{}, Cant: []string{}}
	// A workspace server cannot inspect the person's sidecar. The desktop
	// supplies display metadata; an absent grant never invents a Connect step.
	if s.space() == "personal" {
		sheet.Missing = true
		conns, err := s.connections.Personal(ctx, scope.Caller.Member)
		if err != nil {
			return sheet, err
		}
		for _, c := range conns {
			if c.Service == p.Service {
				sheet.Account = c.Account
				sheet.Missing = false
			}
		}
	} else if p.Connected != nil {
		sheet.Missing = !*p.Connected
		sheet.Account = p.Account
	}

	verb := "use " + serviceName(p.Service)
	if len(need.Can) > 0 {
		verb = verbPhrase(p.Service, need.Can[0])
		for _, outbound := range need.Approve {
			if slices.Contains(need.Can, outbound) {
				verb = verbPhrase(p.Service, outbound)
				break
			}
		}
	}
	sheet.Title = fmt.Sprintf("%s may %s as %s", app.Name, verb, sheet.MemberName)
	for _, can := range need.Can {
		line := verbPhrase(p.Service, can)
		if slices.Contains(need.Approve, can) {
			line += " · approval needed every time"
		}
		sheet.Can = append(sheet.Can, line)
	}
	sheet.Cant = append(sheet.Cant, "Use permissions not listed in this app's Needs")
	for _, verb := range need.Approve {
		sheet.Cant = append(sheet.Cant, verbPhrase(p.Service, verb)+" without your approval")
	}
	return sheet, nil
}
func verbPhrase(service, verb string) string {
	words := strings.ReplaceAll(strings.ReplaceAll(verb, "_", " "), "-", " ")
	if words == "send" && service == "gmail" {
		return "send email"
	}
	if words == "post" && service == "slack" {
		return "post messages"
	}
	return words
}
func serviceName(service string) string {
	names := map[string]string{"gmail": "Gmail", "calendar": "Calendar", "slack": "Slack", "github": "GitHub", "stripe": "Stripe", "airtable": "Airtable", "hubspot": "HubSpot"}
	if name := names[service]; name != "" {
		return name
	}
	return service
}

func (s *Service) Revoke(ctx context.Context, caller svc.Caller, p bridge.RevokeParams) error {
	if err := s.ready(); err != nil {
		return err
	}
	if caller.Agent {
		return bridge.Errorf(bridge.CodeDenied, "Only the person can revoke Allow")
	}
	if p.App == "" || p.Service == "" || p.Space != s.space() {
		return bridge.Errorf(bridge.CodeInvalid, "App, service and this space are required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM grants WHERE member=? AND app=? AND space=? AND service=?`, caller.Member, p.App, p.Space, p.Service)
	if err != nil {
		return err
	}
	return s.audit(ctx, caller, p.App, "connector.revoke", p.Service, "revoke", "")
}

func (s *Service) personal(ctx context.Context, scope svc.Scope, service string) (svc.Credential, error) {
	c, err := s.connections.PersonalCredential(ctx, scope.Caller.Member, service)
	if err != nil {
		return svc.Credential{}, err
	}
	if expired(c, s.now()) {
		return svc.Credential{}, reason(bridge.CodeConflict, bridge.ReasonExpired, "Reconnect this account")
	}
	list, err := s.connections.Personal(ctx, scope.Caller.Member)
	if err != nil {
		return svc.Credential{}, err
	}
	for _, conn := range list {
		if conn.Service == service {
			return credentialValue(c, service, conn.ID, conn.Account, scope.Caller.Member, "personal"), nil
		}
	}
	return svc.Credential{}, bridge.Errorf(bridge.CodeNotFound, "Connect this account first")
}

func (s *Service) Resolve(ctx context.Context, scope svc.Scope, service string) (svc.Credential, error) {
	if err := s.ready(); err != nil {
		return svc.Credential{}, err
	}
	_, need, err := s.connector(ctx, scope, service)
	if err != nil {
		return svc.Credential{}, err
	}
	conn, err := s.workspaceFor(ctx, service, scope.App)
	if err == nil {
		if !usableBy(conn, scope.App) {
			return svc.Credential{}, reason(bridge.CodeDenied, bridge.ReasonNotAllowedForApp, "Ask an Admin to allow this app")
		}
		c, err := s.connections.WorkspaceCredential(ctx, conn.ID, service)
		if err != nil {
			return svc.Credential{}, err
		}
		if expired(c, s.now()) {
			return svc.Credential{}, reason(bridge.CodeConflict, bridge.ReasonExpired, "Reconnect this workspace account")
		}
		return credentialValue(c, service, conn.ID, conn.Account, "", "workspace"), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return svc.Credential{}, err
	}
	if need.As == "workspace" {
		return svc.Credential{}, reason(bridge.CodeDenied, bridge.ReasonNotAllowedForApp, "Ask an Admin for a connection")
	}
	if s.space() == "personal" {
		return s.personal(ctx, scope, service)
	}
	var encrypted []byte
	err = s.db.QueryRowContext(ctx, `SELECT credential FROM grant_credentials WHERE member=? AND app=? AND space=? AND service=?`, scope.Caller.Member, scope.App, s.space(), service).Scan(&encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		sheet, err := s.AllowSheet(ctx, scope, bridge.AllowSheetParams{Service: service})
		if err != nil {
			return svc.Credential{}, err
		}
		s.publish(bridge.TopicConnectAllow(scope.Caller.Member), sheet)
		return svc.Credential{}, reason(bridge.CodeConflict, bridge.ReasonNeedsAllow, "Allow this app to use your connection")
	}
	if err != nil {
		return svc.Credential{}, err
	}
	c, err := s.unseal(encrypted, grantBinding(scope.Caller.Member, scope.App, s.space(), service))
	if err != nil {
		return svc.Credential{}, err
	}
	if expired(c, s.now()) {
		return svc.Credential{}, reason(bridge.CodeConflict, bridge.ReasonExpired, "Reconnect this account")
	}
	return credentialValue(c, service, scope.Caller.Member+":"+scope.App+":"+service, "", scope.Caller.Member, "personal"), nil
}

func (s *Service) Needs(ctx context.Context, scope svc.Scope) (bridge.ConnectionNeeds, error) {
	out := bridge.ConnectionNeeds{Connections: []bridge.ConnectionNeed{}}
	if err := s.ready(); err != nil {
		return out, err
	}
	app, err := s.definition(ctx, scope.App)
	if err != nil {
		return out, err
	}
	services := make([]string, 0, len(app.Capabilities.Connectors))
	for name := range app.Capabilities.Connectors {
		services = append(services, name)
	}
	slices.Sort(services)
	for _, service := range services {
		connector := app.Capabilities.Connectors[service]
		need := bridge.ConnectionNeed{Service: service, Level: "personal", Label: "as each person", For: strings.Join(connector.Can, " · "), Status: "not_yet"}
		if connector.As == "workspace" {
			need.Level = "workspace"
			need.Label = s.space() + " workspace"
			need.Status = "ask_admin"
		}
		status, err := s.needStatus(ctx, scope, service)
		if err != nil {
			return out, err
		}
		if status != "" {
			need.Status = status
		}
		if scope.Caller.Role == svc.RoleEditor && connector.As == "member" {
			count, err := s.allowedCount(ctx, scope, service)
			if err != nil {
				return out, err
			}
			need.Allowed = &count
		}
		out.Connections = append(out.Connections, need)
	}
	return out, nil
}
func (s *Service) needStatus(ctx context.Context, scope svc.Scope, service string) (string, error) {
	conn, err := s.workspaceFor(ctx, service, scope.App)
	if err == nil {
		if !usableBy(conn, scope.App) {
			return "ask_admin", nil
		}
		c, err := s.connections.WorkspaceCredential(ctx, conn.ID, service)
		if err != nil {
			return "", err
		}
		if expired(c, s.now()) {
			return "expired", nil
		}
		return "set_by_admin", nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	_, need, err := s.connector(ctx, scope, service)
	if err != nil {
		return "", err
	}
	if need.As == "workspace" {
		return "ask_admin", nil
	}
	if s.space() == "personal" {
		c, err := s.connections.PersonalCredential(ctx, scope.Caller.Member, service)
		var typed *bridge.Error
		if errors.As(err, &typed) && typed.Code == bridge.CodeNotFound {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if expired(c, s.now()) {
			return "expired", nil
		}
		return "allowed", nil
	}
	var raw []byte
	err = s.db.QueryRowContext(ctx, `SELECT credential FROM grant_credentials WHERE member=? AND app=? AND space=? AND service=?`, scope.Caller.Member, scope.App, s.space(), service).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	c, err := s.unseal(raw, grantBinding(scope.Caller.Member, scope.App, s.space(), service))
	if err != nil {
		return "", err
	}
	if expired(c, s.now()) {
		return "expired", nil
	}
	return "allowed", nil
}
func (s *Service) allowedCount(ctx context.Context, scope svc.Scope, service string) (bridge.ConnectionAllowed, error) {
	members, err := s.spaces.Members(ctx, scope.App)
	if err != nil {
		return bridge.ConnectionAllowed{}, err
	}
	count := bridge.ConnectionAllowed{Of: len(members)}
	for _, member := range members {
		own := scope
		own.Caller.Member = member
		status, err := s.needStatus(ctx, own, service)
		if err != nil {
			return count, err
		}
		// Expiry stops use, but it does not withdraw the person's Allow.
		if status == "allowed" || status == "set_by_admin" || status == "expired" {
			count.N++
		}
	}
	return count, nil
}
