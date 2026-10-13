package records

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) tick(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-ticker.C:
			_ = s.Refresh(ctx, at)
		}
	}
}

// Refresh is also the deterministic clock door for tests. Only databases that
// have been accessed are eligible; it never creates real data for a preview.
func (s *Service) Refresh(ctx context.Context, at time.Time) error {
	s.mu.Lock()
	scopes := make([]svc.Scope, 0, len(s.scopes))
	for _, scope := range s.scopes {
		scopes = append(scopes, scope)
	}
	s.mu.Unlock()
	var failures []error
	for _, scope := range scopes {
		if err := s.refresh(ctx, scope, at); err != nil {
			var e *bridge.Error
			if errors.As(err, &e) && e.Code == bridge.CodeNotFound {
				s.mu.Lock()
				delete(s.scopes, scope.App+"/"+string(scope.Data))
				s.mu.Unlock()
				continue
			}
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func (s *Service) refresh(ctx context.Context, scope svc.Scope, at time.Time) error {
	c, err := s.open(ctx, scope, true, false)
	if err != nil {
		return err
	}
	defer c.close()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var changed []bridge.Record
	for _, typ := range sorted(c.def.App.Records) {
		rules, err := NewRules(c.def.App.Records[typ])
		if err != nil {
			return err
		}
		if !rules.Timed {
			continue
		}
		table, _ := query.Table(typ)
		rows, err := tx.QueryContext(ctx, "SELECT record FROM "+table+" ORDER BY id")
		if err != nil {
			return err
		}
		var before []bridge.Record
		for rows.Next() {
			var data string
			if err := rows.Scan(&data); err != nil {
				rows.Close()
				return err
			}
			var row bridge.Record
			if err := json.Unmarshal([]byte(data), &row); err != nil {
				rows.Close()
				return err
			}
			before = append(before, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, row := range before {
			after, err := rules.Prepare(row, svc.Owner, at, false)
			if err != nil {
				return err
			}
			if reflect.DeepEqual(row.Fields, after.Fields) {
				continue
			}
			after.Version++
			after.UpdatedAt, after.UpdatedBy = stamp(at), svc.Owner
			if err := save(ctx, tx, &row, &after, svc.Owner); err != nil {
				return err
			}
			changed = append(changed, after)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for i := range changed {
		row := changed[i]
		s.publish(scope, bridge.RecordDiff{Op: bridge.DiffUpsert, Type: row.Type, ID: row.ID, Record: &row, By: svc.Owner})
	}
	return nil
}
