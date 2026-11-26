package builder

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/cell"
)

type PreviewRequest struct {
	Draft      string `json:"draft"`
	Checkpoint string `json:"checkpoint,omitempty"`
	Data       string `json:"data,omitempty"`
}

type Preview struct {
	App             json.RawMessage              `json:"app"`
	Sample          bool                         `json:"sample"`
	Screens         []string                     `json:"screens"`
	Scope           string                       `json:"scope"`
	ResolvedRecords map[string]schema.RecordType `json:"resolved_records,omitempty"`
}

type previewScope struct {
	job, checkpoint string
	revision        uint64
	real            bool
	files           map[string]string
}

func (s *Jobs) Preview(p PreviewRequest) (Preview, error) {
	if p.Data != "" && p.Data != "sample" && p.Data != "real" {
		return Preview{}, store.ErrBadInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if scope, ok := s.scopes[p.Draft]; ok {
		if p.Checkpoint == "" && (p.Data == "" || (p.Data == "real") == scope.real) {
			return scope.result(p.Draft)
		}
		p.Draft = scope.job
	}
	j, err := s.load(p.Draft)
	if err != nil {
		return Preview{}, err
	}
	current := previewScope{job: j.Draft, checkpoint: p.Checkpoint, real: p.Data == "real"}
	if p.Checkpoint != "" {
		snap, ok := j.Saved[p.Checkpoint]
		if !ok {
			return Preview{}, store.ErrNotFound
		}
		current.files = snap.Files
	} else if j.WritingFiles != nil {
		current.files = j.WritingFiles
		current.revision = j.Revision
	} else {
		current.files = j.Working.Files
		if n := len(j.Checkpoints); j.Working.Passed && n > 0 {
			current.checkpoint = j.Checkpoints[n-1].ID
		} else {
			current.revision = j.Revision
		}
	}
	for id, known := range s.scopes {
		if known.job == current.job && known.checkpoint == current.checkpoint && known.revision == current.revision && known.real == current.real {
			return known.result(id)
		}
	}
	return s.openPreview(current)
}

// PreviewKept draws a saved definition using a temporary sample scope.
func (s *Jobs) PreviewKept(def store.Definition) (Preview, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.openPreview(previewScope{files: def.Files()})
	if err != nil {
		return Preview{}, nil, err
	}
	return p, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.removeScope(p.Scope)
	}, nil
}

func (s *Jobs) openPreview(current previewScope) (Preview, error) {
	id, err := cell.NewID(time.Now())
	if err != nil {
		return Preview{}, err
	}
	result, err := current.result(id)
	if err != nil {
		return Preview{}, err
	}
	if s.opts.SeedPreview != nil {
		dir := filepath.Join(s.root, id)
		if err := os.Mkdir(dir, 0o700); err != nil {
			return Preview{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, "preview"), nil, 0o600); err != nil {
			s.removeScope(id)
			return Preview{}, err
		}
		if err := s.opts.SeedPreview(id, definition(current.files), current.real); err != nil {
			s.removeScope(id)
			return Preview{}, err
		}
	} else if current.real {
		return Preview{}, errors.New("trying the app with your records is unavailable")
	}
	s.scopes[id] = current
	return result, nil
}

func (p previewScope) result(id string) (Preview, error) {
	app := json.RawMessage(p.files[schema.AppFile])
	ids := screens(p.files)
	if !json.Valid(app) || len(ids) == 0 {
		return Preview{}, store.ErrNotFound
	}
	return Preview{App: app, Sample: !p.real, Screens: ids, Scope: id}, nil
}

// Screen reads the exact definition used to create an isolated preview scope.
func (s *Jobs) Screen(draft, screen string) (json.RawMessage, error) {
	path := "screens/" + screen + ".json"
	if !definitionPath.MatchString(path) {
		return nil, store.ErrBadInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	scope, ok := s.scopes[draft]
	if !ok {
		j, err := s.load(draft)
		if err != nil {
			return nil, err
		}
		scope.files = j.Working.Files
		if j.WritingFiles != nil {
			scope.files = j.WritingFiles
		}
	}
	raw := json.RawMessage(scope.files[path])
	if !json.Valid(raw) {
		return nil, store.ErrNotFound
	}
	return raw, nil
}

func (s *Jobs) removeScope(id string) {
	delete(s.scopes, id)
	if s.opts.ClosePreview != nil {
		s.opts.ClosePreview(id)
	}
	_ = os.RemoveAll(filepath.Join(s.root, id))
}

func (s *Jobs) closeScopes(jobID string) {
	for id, scope := range s.scopes {
		if scope.job == jobID {
			s.removeScope(id)
		}
	}
	if s.opts.ClosePreview != nil {
		s.opts.ClosePreview(jobID)
	}
}

func (s *Jobs) Restore(id, checkpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.load(id)
	if err != nil {
		return err
	}
	if j.Publishing || j.Proposal != nil {
		return store.ErrConflict
	}
	snap, ok := j.Saved[checkpoint]
	if !ok {
		return store.ErrNotFound
	}
	catalog, err := s.catalog(j.App)
	if err != nil {
		return err
	}
	j.Working, j.State.State, j.Problem, j.Dirty = snap, Ready, "", true
	j.WritingFiles = nil
	j.Presence = nil
	j.Progress, j.Queue, j.Paused, j.PendingProposal = []Event{}, nil, nil, nil
	j.describe(catalog)
	if err := j.checkpoint("I brought back this earlier version of the app."); err != nil {
		return err
	}
	if err := s.save(j, Event{Line: "Earlier version restored."}); err != nil {
		return err
	}
	if cancel := s.workers[id]; cancel != nil {
		cancel()
	}
	return nil
}

// Apply publishes a checked revision and leaves its editing history available.
func (s *Jobs) Apply(id string) (store.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.load(id)
	if err != nil {
		return store.Meta{}, err
	}
	if len(j.Queue) != 0 || j.PendingProposal != nil {
		return store.Meta{}, fmt.Errorf("%w: finish checking before keeping the app", store.ErrConflict)
	}
	if !j.Dirty && j.App != "" {
		live, err := s.opts.Store.Get(j.App)
		return live.Meta, err
	}
	if !j.Working.CheckComplete {
		// A pre-Look build may have stopped after publishing but before
		// recording the receipt. It can finish only that same publication.
		app := j.App
		if app == "" {
			app = j.Draft
		}
		live, err := s.opts.Store.Get(app)
		if !j.Publishing || err != nil || !samePublication(live, j) {
			if j.Publishing {
				j.Publishing = false
				if err := s.save(j, Event{}); err != nil {
					return store.Meta{}, err
				}
			}
			return store.Meta{}, fmt.Errorf("%w: finish checking before keeping the app", store.ErrConflict)
		}
	}
	app := j.App
	if app == "" {
		app = j.Draft
	}
	live, err := s.opts.Store.Get(app)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Meta{}, err
	}
	if !samePublication(live, j) {
		// Record the intent before publication. If the process stops between
		// committing and recording the result, retry recognizes its own published files.
		if !j.Publishing {
			j.Publishing = true
			if err := s.save(j, Event{}); err != nil {
				return store.Meta{}, err
			}
		}
		meta, err := s.opts.Store.PublishBuild(j.Draft, j.App, j.Base, definition(j.Working.Files), j.LastRequest)
		if err != nil {
			j.Publishing = false
			var event Event
			if errors.Is(err, store.ErrConflict) {
				j.State.State = NeedsYou
				j.Problem = "The saved app changed while you were editing. Discard this draft to start from the latest version."
				event.Line = j.Problem
			}
			return store.Meta{}, errors.Join(err, s.save(j, event))
		}
		live, err = s.opts.Store.Get(meta.ID)
		if err != nil {
			return store.Meta{}, err
		}
	}

	j.App, j.Base, j.Name = live.Meta.ID, live.Meta.Version, live.Meta.Name
	j.Dirty, j.Publishing, j.Editable = false, false, true
	j.Proposal, j.PendingProposal = nil, nil
	j.Working = snapshot{Files: live.Files(), Passed: true}
	j.acceptKeptUses()
	j.KeptMessages, j.KeptCheckpoints, j.KeptMarks = len(j.Transcript), len(j.Checkpoints), len(j.Marks)
	if err := s.save(j, Event{Line: "Your app is live."}); err != nil {
		return store.Meta{}, err
	}
	s.closeScopes(id)
	return live.Meta, nil
}

func samePublication(live store.Definition, j *job) bool {
	if live.Meta.Version != j.Base+1 {
		return false
	}
	files := live.Files()
	if len(files) != len(j.Working.Files) {
		return false
	}
	for path, raw := range j.Working.Files {
		if path != schema.AppFile && files[path] != raw {
			return false
		}
	}
	var proposed map[string]json.RawMessage
	if json.Unmarshal([]byte(j.Working.Files[schema.AppFile]), &proposed) != nil || proposed == nil {
		return false
	}
	proposed["id"], _ = json.Marshal(live.Meta.ID)
	proposed["version"], _ = json.Marshal(live.Meta.Version)
	stamped, _ := json.MarshalIndent(proposed, "", "  ")
	return string(stamped)+"\n" == string(live.App)
}

func (s *Jobs) Discard(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.load(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if j.Publishing {
		return fmt.Errorf("%w: finish keeping the app first", store.ErrConflict)
	}
	if cancel := s.workers[id]; cancel != nil {
		cancel()
	}
	if j.App == "" {
		s.closeScopes(id)
		if err := os.RemoveAll(filepath.Join(s.root, id)); err != nil {
			return err
		}
		close(s.changes)
		s.changes = make(chan struct{})
		if s.opts.Changed != nil {
			s.opts.Changed(Event{Draft: id, Stage: Ready})
		}
		return nil
	}
	live, err := s.opts.Store.Get(j.App)
	if err != nil {
		return err
	}
	j.Base, j.Name = live.Meta.Version, live.Meta.Name
	j.Working = snapshot{Files: live.Files(), Passed: true}
	j.acceptKeptUses()
	j.Queue, j.Paused, j.Dirty, j.Problem, j.State.State = nil, nil, false, "", Ready
	j.Proposal, j.PendingProposal = nil, nil
	j.WritingFiles = nil
	j.Presence = nil
	j.Progress = []Event{}
	for _, checkpoint := range j.Checkpoints[j.KeptCheckpoints:] {
		delete(j.Saved, checkpoint.ID)
	}
	j.Checkpoints = j.Checkpoints[:j.KeptCheckpoints]
	j.Transcript = j.Transcript[:j.KeptMessages]
	j.Marks = j.Marks[:j.KeptMarks]
	s.describeKept(j)
	if err := s.save(j, Event{}); err != nil {
		return err
	}
	s.closeScopes(id)
	return nil
}
