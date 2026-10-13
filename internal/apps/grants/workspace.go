package grants

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func usableBy(c bridge.WorkspaceConnection, app string) bool {
	return c.Who == "all" || slices.Contains(c.Apps, app)
}
func validateWho(who string, apps []string) error {
	if who != "all" && who != "chosen" {
		return bridge.Errorf(bridge.CodeInvalid, "Who must be all or chosen")
	}
	for _, app := range apps {
		if app == "" {
			return bridge.Errorf(bridge.CodeInvalid, "An app id is required")
		}
	}
	if who == "all" && len(apps) > 0 {
		return bridge.Errorf(bridge.CodeInvalid, "All apps does not take a chosen list")
	}
	return nil
}
func (s *Service) validateApps(ctx context.Context, apps []string) error {
	for _, app := range apps {
		if _, err := s.definition(ctx, app); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) workspaceFor(ctx context.Context, service, app string) (bridge.WorkspaceConnection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM workspace_connections WHERE service=? ORDER BY at,id`, service)
	if err != nil {
		return bridge.WorkspaceConnection{}, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return bridge.WorkspaceConnection{}, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return bridge.WorkspaceConnection{}, err
	}
	if len(ids) == 0 {
		return bridge.WorkspaceConnection{}, sql.ErrNoRows
	}
	first := bridge.WorkspaceConnection{}
	for i, id := range ids {
		c, err := s.workspaceConnection(ctx, id)
		if err != nil {
			return c, err
		}
		if i == 0 {
			first = c
		}
		if usableBy(c, app) {
			return c, nil
		}
	}
	return first, nil
}

func (s *Service) workspaceConnection(ctx context.Context, id string) (bridge.WorkspaceConnection, error) {
	c := bridge.WorkspaceConnection{ID: id, Apps: []string{}, UsedBy: []bridge.WorkspaceConnectionUse{}}
	err := s.db.QueryRowContext(ctx, `SELECT service,account,added_by,who FROM workspace_connections WHERE id=?`, id).Scan(&c.Service, &c.Account, &c.AddedBy, &c.Who)
	if err != nil {
		return c, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT app FROM workspace_connection_apps WHERE id=? ORDER BY app`, id)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var app string
		if err := rows.Scan(&app); err != nil {
			return c, err
		}
		c.Apps = append(c.Apps, app)
	}
	return c, rows.Err()
}
func connectionError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.Errorf(bridge.CodeNotFound, "Workspace connection not found")
	}
	return err
}

func (s *Service) Workspace(ctx context.Context, caller svc.Caller) (bridge.WorkspaceConnectionsResult, error) {
	out := bridge.WorkspaceConnectionsResult{Connections: []bridge.WorkspaceConnection{}, CanEdit: s.admin(caller) == nil}
	if err := s.ready(); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM workspace_connections ORDER BY service,id`)
	if err != nil {
		return out, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return out, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return out, err
	}
	for _, id := range ids {
		c, err := s.workspaceConnection(ctx, id)
		if err != nil {
			return out, err
		}
		c.UsedBy, err = s.uses(ctx, caller, c)
		if err != nil {
			return out, err
		}
		out.Connections = append(out.Connections, c)
	}
	return out, nil
}
func (s *Service) uses(ctx context.Context, caller svc.Caller, c bridge.WorkspaceConnection) ([]bridge.WorkspaceConnectionUse, error) {
	uses := []bridge.WorkspaceConnectionUse{}
	if s.all == nil || s.all.Store == nil {
		return uses, nil
	}
	apps, err := s.all.Store.List(ctx, caller)
	if err != nil {
		return uses, err
	}
	for _, app := range apps {
		if !usableBy(c, app.ID) {
			continue
		}
		def, err := s.definition(ctx, app.ID)
		if err != nil {
			return uses, err
		}
		need, ok := def.Capabilities.Connectors[c.Service]
		if !ok {
			continue
		}
		uses = append(uses, bridge.WorkspaceConnectionUse{App: app.ID, AppName: def.Name, For: strings.Join(need.Can, " · ")})
	}
	slices.SortFunc(uses, func(a, b bridge.WorkspaceConnectionUse) int { return strings.Compare(a.App, b.App) })
	return uses, nil
}
func (s *Service) AddWorkspace(ctx context.Context, caller svc.Caller, p bridge.AddWorkspaceConnectionParams) (bridge.WorkspaceConnection, error) {
	c := bridge.WorkspaceConnection{}
	if err := s.admin(caller); err != nil {
		return c, err
	}
	if err := s.ready(); err != nil {
		return c, err
	}
	if p.Service == "" {
		return c, bridge.Errorf(bridge.CodeInvalid, "Service is required")
	}
	if err := validateWho(p.Who, p.Apps); err != nil {
		return c, err
	}
	if err := s.validateApps(ctx, p.Apps); err != nil {
		return c, err
	}

	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return c, err
	}
	id := hex.EncodeToString(random)
	authCtx := context.WithValue(ctx, authPublisherKey{}, func(url string) {
		s.publish(bridge.TopicConnectWorkspace(), bridge.WorkspaceConnection{ID: id, Service: p.Service, AuthURL: url})
	})
	signed, err := s.connections.SignIn(authCtx, id, p.Service)
	if err != nil {
		return c, err
	}
	c = bridge.WorkspaceConnection{ID: id, Service: p.Service, Account: signed.Account, AddedBy: caller.Member, Who: p.Who, Apps: append([]string{}, p.Apps...), UsedBy: []bridge.WorkspaceConnectionUse{}}
	if err = s.saveWorkspace(ctx, c, true); err != nil {
		_ = s.connections.SignOut(ctx, id, p.Service)
		return c, err
	}
	c.UsedBy, err = s.uses(ctx, caller, c)
	if err != nil {
		return c, err
	}
	if err = s.audit(ctx, caller, "", "connection.add", id, "add", ""); err != nil {
		return c, err
	}
	s.publish(bridge.TopicConnectWorkspace(), c)
	return c, nil
}
func (s *Service) saveWorkspace(ctx context.Context, c bridge.WorkspaceConnection, add bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if add {
		_, err = tx.ExecContext(ctx, `INSERT INTO workspace_connections(id,service,account,added_by,who,at) VALUES(?,?,?,?,?,?)`, c.ID, c.Service, c.Account, c.AddedBy, c.Who, s.now().UTC().Format(time.RFC3339))
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE workspace_connections SET who=? WHERE id=?`, c.Who, c.ID)
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM workspace_connection_apps WHERE id=?`, c.ID); err != nil {
		return err
	}
	for _, app := range c.Apps {
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO workspace_connection_apps(id,app) VALUES(?,?)`, c.ID, app); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Service) SetWho(ctx context.Context, caller svc.Caller, p bridge.SetWhoParams) (bridge.WorkspaceConnection, error) {
	c := bridge.WorkspaceConnection{}
	if err := s.admin(caller); err != nil {
		return c, err
	}
	if err := s.ready(); err != nil {
		return c, err
	}
	if err := validateWho(p.Who, p.Apps); err != nil {
		return c, err
	}
	if err := s.validateApps(ctx, p.Apps); err != nil {
		return c, err
	}
	c, err := s.workspaceConnection(ctx, p.ID)
	if err != nil {
		return c, connectionError(err)
	}
	c.Who = p.Who
	c.Apps = append([]string{}, p.Apps...)
	if err = s.saveWorkspace(ctx, c, false); err != nil {
		return c, err
	}
	c.UsedBy, err = s.uses(ctx, caller, c)
	if err != nil {
		return c, err
	}
	if err = s.audit(ctx, caller, "", "connection.who", c.ID, p.Who, ""); err != nil {
		return c, err
	}
	s.publish(bridge.TopicConnectWorkspace(), c)
	return c, nil
}
func (s *Service) RemovalImpact(ctx context.Context, caller svc.Caller, id string) (bridge.RemovalImpact, error) {
	out := bridge.RemovalImpact{ID: id, Apps: []bridge.RemovalImpactApp{}}
	if err := s.admin(caller); err != nil {
		return out, err
	}
	if err := s.ready(); err != nil {
		return out, err
	}
	c, err := s.workspaceConnection(ctx, id)
	if err != nil {
		return out, connectionError(err)
	}
	out.Service = c.Service
	uses, err := s.uses(ctx, caller, c)
	if err != nil {
		return out, err
	}
	for _, use := range uses {
		item := bridge.RemovalImpactApp{App: use.App, AppName: use.AppName, For: use.For}
		if s.all != nil && s.all.Runs != nil {
			runs, err := s.all.Runs.List(ctx, svc.Scope{App: use.App, Caller: caller, Data: svc.DataReal}, bridge.ListRunsParams{})
			if err != nil {
				return out, err
			}
			for _, run := range runs.Runs {
				at, err := time.Parse(time.RFC3339, run.StartedAt)
				if err == nil && !at.Before(weekStart(s.now())) && !at.After(s.now()) && !run.Fake {
					item.RunsThisWeek++
				}
			}
		}
		out.Apps = append(out.Apps, item)
	}
	return out, nil
}
func weekStart(now time.Time) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
}
func (s *Service) RemoveWorkspace(ctx context.Context, caller svc.Caller, id string) error {
	if err := s.admin(caller); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	c, err := s.workspaceConnection(ctx, id)
	if err != nil {
		return connectionError(err)
	}
	impact, err := s.RemovalImpact(ctx, caller, id)
	if err != nil {
		return err
	}
	if err = s.connections.SignOut(ctx, id, c.Service); err != nil {
		return err
	}
	if _, err = s.db.ExecContext(ctx, `DELETE FROM workspace_connections WHERE id=?`, id); err != nil {
		return err
	}
	activity := fmt.Sprintf("%s removed %s · used by %d apps", s.spaces.NameOf(caller), serviceName(c.Service), len(impact.Apps))
	if err = s.audit(ctx, caller, "", "connection.remove", id, "remove", activity); err != nil {
		return err
	}
	s.publish(bridge.TopicConnectWorkspace(), c)
	return nil
}
