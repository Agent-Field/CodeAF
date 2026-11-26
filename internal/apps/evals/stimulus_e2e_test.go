//go:build e2e

package evals

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

// The platform owns workflow inputs and expectations even when a gallery app
// does not declare a lifecycle, agent or connector. Identity and precedence
// are tested on the actual gallery app and its published grants.
type galleryProcess struct {
	*appProcess
	model *probeModel
}

func (p *galleryProcess) Exercise(ctx context.Context, name, app string, record Record) (map[string]any, error) {
	switch name {
	case probeGuardedUndo:
		return p.guardedUndo(ctx)
	case probeDuplicateTrigger, probeRecordInstructions, probeEmailInstructions:
		return p.runStimulus(ctx, name)
	case probePersonWins, probeRename:
		return p.recordStimulus(ctx, name, app, record)
	default:
		return nil, fmt.Errorf("unknown platform probe %q", name)
	}
}

func seedApp(home, fixture string) (string, error) {
	s := store.NewAt(home, nil)
	draft, err := s.NewDraft("")
	if err != nil {
		return "", err
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS(fixture)); err != nil {
		return "", err
	}
	meta, err := s.Promote(draft, "Platform eval fixture")
	return meta.ID, err
}

func (p *galleryProcess) guardedUndo(ctx context.Context) (observed map[string]any, err error) {
	scratch := filepath.Join(p.home, probeGuardedUndo)
	app, err := seedApp(scratch, "testdata/stimulus")
	if err != nil {
		return nil, err
	}
	// The fixture has a separate engine home so its checked transitions cannot
	// change the app under evaluation or its spending ledger.
	engine, err := launch(ctx, p.binary, scratch, "", "")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, engine.Close()) }()
	steps := []Step{
		{Method: "app.get", Params: map[string]any{"app": app}, Expect: []Check{equal("/app/id", app)}},
		{Method: "data.create", Params: map[string]any{"app": app, "type": "probe", "set": map[string]any{"name": "Release review"}},
			Capture: map[string]string{"id": "/rows/0/id", "rev": "/rows/0/_rev"}},
	}
	vars := map[string]any{}
	if err := runSteps(ctx, engine, steps, vars); err != nil {
		return nil, err
	}
	ref := map[string]any{"type": "probe", "id": "$id"}
	move := map[string]any{
		"app": app, "transition": "close",
		"records": []any{map[string]any{"record": ref, "expected_rev": "$rev"}},
	}
	process := map[string]any{"app": app, "record": ref}
	steps = []Step{
		{Method: "data.process", Params: process, Expect: []Check{equal("/state", "Open"), equal("/transitions/0/ready", false)}},
		{Method: "data.transition_batch", Params: move, Error: "lifecycle"},
		{Method: "data.checklist_set", Params: map[string]any{
			"app": app, "record": ref, "checklist": "checks", "step": "review", "done": true,
			"expected_rev": "$rev", "evidence": map[string]any{"kind": "note", "text": "Release checked"},
		}, Expect: []Check{equal("/checklist/steps/0/done", true)}, Capture: map[string]string{"rev": "/_rev"}},
		{Method: "data.signoffs.create", Params: map[string]any{
			"app": app, "type": "probe", "id": "$id", "statement": "Release may close", "rev": "$rev",
		}, Expect: []Check{equal("/item/current", true)}},
		{Method: "data.process", Params: process, Expect: []Check{equal("/transitions/0/ready", true)}, Capture: map[string]string{"rev": "/_rev"}},
		{Method: "data.transition_batch", Params: move, Expect: []Check{
			equal("/rows/0/stage", "Closed"), {Path: "/undo_batch", Exists: new(false)},
		}, Capture: map[string]string{"batch": "/batch", "closed_rev": "/rows/0/_rev"}},
	}
	if err := runSteps(ctx, engine, steps, vars); err != nil {
		return nil, err
	}
	_, undoErr := engine.Call(ctx, "data.undo", map[string]any{"app": app, "batch": vars["batch"]})
	body, err := engine.Call(ctx, "data.process", map[string]any{
		"app": app, "record": map[string]any{"type": "probe", "id": vars["id"]},
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"undo_refused": errors.Is(undoErr, CodeError("lifecycle")),
		"state_preserved": len(checkReply(body, []Check{
			equal("/state", "Closed"), equal("/_rev", vars["closed_rev"]), equal("/checklist/steps/0/done", true),
		})) == 0,
	}, nil
}

func (p *galleryProcess) recordStimulus(ctx context.Context, name, app string, record Record) (map[string]any, error) {
	scope := appwire.DataScope{App: app}
	typ, field := record.Type, record.UniqueBy
	values := maps.Clone(record.Value)
	original, ok := values[field].(string)
	if !ok {
		return nil, errors.New("record stimulus needs a text field for distinct values")
	}
	value := original + " " + name
	values[field] = value
	created, err := readReply[appwire.DataWriteResult](p.wire(ctx, "data.create", appwire.DataCreateParams{
		DataScope: scope, Type: typ, Set: values,
	}))
	if err != nil {
		return nil, fmt.Errorf("data.create: %w", err)
	}
	if len(created.Rows) != 1 {
		return nil, errors.New("data.create did not return one record")
	}
	id, _ := created.Rows[0]["id"].(string)
	if id == "" {
		return nil, errors.New("data.create did not return a record ID")
	}
	if created.Rows[0][field] != value {
		return nil, fmt.Errorf("data.create did not save the stimulus value for %s", field)
	}
	switch name {
	case probePersonWins:
		var held []data.Held
		err := p.withDataStore(ctx, func(db *data.Store) error {
			write, err := db.Update(data.Scope{App: app, Agent: true, AI: true}, typ, []string{id},
				map[string]any{field: original + " AI suggestion"}, data.UpdateOptions{})
			if err == nil {
				held = write.Held
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		row, err := p.stimulusRow(ctx, scope, typ, id)
		return map[string]any{
			"held": len(held), "person_value_preserved": row[field] == value &&
				slices.ContainsFunc(held, func(h data.Held) bool { return h.ID == id && h.Field == field }),
		}, err
	case probeRename:
		before, err := p.fieldID(ctx, scope, typ, field)
		if err != nil {
			return nil, err
		}
		renamed := "eval_renamed_" + field
		if err := p.withDataStore(ctx, func(db *data.Store) error {
			return db.RenameField(typ, field, renamed)
		}); err != nil {
			return nil, err
		}
		after, err := p.fieldID(ctx, scope, typ, renamed)
		if err != nil {
			return nil, err
		}
		row, err := p.stimulusRow(ctx, scope, typ, id)
		return map[string]any{
			"same_field_id": before == after, "same_record_id": row["id"] == id,
			"value_preserved": row[renamed] == value,
		}, err
	default:
		return nil, fmt.Errorf("unknown record probe %q", name)
	}
}

func (p *galleryProcess) stimulusRow(ctx context.Context, scope appwire.DataScope, typ, id string) (appwire.DataRow, error) {
	result, err := readReply[appwire.DataRowResult](p.wire(ctx, "data.get", appwire.DataRecordParams{
		DataScope: scope, Type: typ, ID: id,
	}))
	return result.Row, err
}

func (p *galleryProcess) fieldID(ctx context.Context, scope appwire.DataScope, typ, field string) (string, error) {
	result, err := readReply[appwire.DataTypesResult](p.wire(ctx, "data.types", scope))
	if err != nil {
		return "", err
	}
	for _, t := range result.Types {
		if t.Name == typ {
			for _, f := range t.Fields {
				if f.Name == field && f.ID != "" && !f.Hidden {
					return f.ID, nil
				}
			}
		}
	}
	return "", fmt.Errorf("data.types did not return a live field ID for %s.%s", typ, field)
}

// The current person wire has no AI principal or rename operation. Hand
// ownership of the disposable home to the native writer for those stimuli.
func (p *galleryProcess) withDataStore(ctx context.Context, work func(*data.Store) error) (err error) {
	defer func() {
		next, restartErr := launchWithEnv(ctx, p.binary, p.home, p.env)
		if restartErr == nil {
			*p.appProcess = *next
		}
		err = errors.Join(err, restartErr)
	}()
	if err := p.Close(); err != nil {
		return err
	}
	place := data.NewPlace(p.home, data.Options{NoRefresh: true})
	defer func() { err = errors.Join(err, place.Close()) }()
	db, err := place.Main()
	if err != nil {
		return err
	}
	return work(db)
}

func TestPlatformStimulus(t *testing.T) {
	binary := os.Getenv("CODEAF_APP_EVAL_BINARY")
	if binary == "" {
		t.Skip("set CODEAF_APP_EVAL_BINARY to the built app engine")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	scratch := t.TempDir()
	app, err := seedApp(scratch, "testdata/stimulus")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := launch(t.Context(), binary, scratch, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := engine.Close(); err != nil {
			t.Error(err)
		}
	}()
	record := Record{Type: "probe", UniqueBy: "name", Value: map[string]any{"name": "Eval release"}, Edit: map[string]any{"name": "Updated release"}}
	for _, probe := range adversarial(t.Context(), &galleryProcess{appProcess: engine}, record, app) {
		if !probe.Pass || probe.Unrun {
			t.Errorf("%s: %s", probe.Name, strings.TrimSpace(probe.Problem))
		}
	}
}
