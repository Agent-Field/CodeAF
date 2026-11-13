package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/route"
)

// routesFile keeps the factory's routing table ([route.Rule]) beside its own
// rules, with who last changed it and when. It is not a number, so
// [Store.List] never reads it as a job.
const routesFile = "factory-routes.json"

// ErrRoutesMoved refuses a write whose reader's copy of the routing table is
// no longer what the factory keeps. Nothing was written.
var ErrRoutesMoved = errors.New("the factory's routing rules changed since they were read")

// KeptRoutes is the routing table as the factory keeps it, with its last
// change. Rules is never nil, so the wire always says `[]` for none.
type KeptRoutes struct {
	Rules []route.Rule `json:"rules"`
	factory.RulesChange
}

// Routes is the factory's routing table, in order. A factory that was never
// given one answers an empty table, and intake then behaves exactly as it
// did before routing existed.
//
// A FILE THAT CANNOT BE READ IS AN ERROR, NOT AN EMPTY TABLE: a table that
// silently vanished would start jobs a rule said to hold, or the reverse.
func (st *Store) Routes() ([]route.Rule, error) {
	kept, err := st.KeptRoutes()
	return kept.Rules, err
}

// KeptRoutes are [Store.Routes] with their last change. A table that was
// never written carries no change.
func (st *Store) KeptRoutes() (KeptRoutes, error) {
	none := KeptRoutes{Rules: []route.Rule{}}
	raw, err := os.ReadFile(filepath.Join(st.root, routesFile))
	if errors.Is(err, os.ErrNotExist) {
		return none, nil
	}
	if err != nil {
		return none, err
	}
	var out KeptRoutes
	if err := json.Unmarshal(raw, &out); err != nil {
		return none, errors.New("the factory's routing rules file cannot be read")
	}
	if out.Rules, err = route.Clean(out.Rules); err != nil {
		return none, err
	}
	return out, nil
}

// SetRoutes replaces the routing table under a cross-process lock, records by
// as its last change, and answers what was kept. The table is checked whole
// by [route.Clean] and refused whole. When expect is not nil and the factory
// keeps any other table, nothing is written and the answer is
// [ErrRoutesMoved], so two people who each read the table cannot quietly undo
// each other's change.
func (st *Store) SetRoutes(rules []route.Rule, by factory.RulesChange, expect *[]route.Rule) ([]route.Rule, error) {
	next, err := route.Clean(rules)
	if err != nil {
		return nil, err
	}
	var want []route.Rule
	if expect != nil {
		if want, err = route.Clean(*expect); err != nil {
			return nil, err
		}
	}
	err = st.underLock(filepath.Join(st.root, "factory-routes.lock"), func() error {
		cur, err := st.Routes()
		if err != nil {
			return err
		}
		if expect != nil && !reflect.DeepEqual(want, cur) {
			return ErrRoutesMoved
		}
		raw, err := json.Marshal(KeptRoutes{Rules: next, RulesChange: by})
		if err != nil {
			return err
		}
		return writeAtomic(filepath.Join(st.root, routesFile), append(raw, '\n'))
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}
