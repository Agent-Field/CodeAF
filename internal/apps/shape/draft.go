package shape

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct {
	mu         sync.Mutex
	applyMu    sync.Mutex
	replayHook func(app string, newVersion int)
	now        func() time.Time
	cfg        svc.Config
	all        *svc.Services
	drafts     map[string]*draft
	loadErr    error
	pipeline   *pipeline
}

// draft keeps replay data off the wire. The saved base makes cancellation
// independent of changes to the live definition and its working tree.
type draft struct {
	bridge.Draft
	BaseFiles spec.Files                   `json:"base_files"`
	Diffs     map[string][]bridge.FileDiff `json:"diffs"`
	s         *Service
	dir       string
}

func New(cfg svc.Config, all *svc.Services) *Service {
	s := &Service{cfg: cfg, all: all, drafts: make(map[string]*draft), now: time.Now}
	s.loadErr = s.reload()
	s.initPipeline()
	return s
}

func draftKey(scope svc.Scope) string { return scope.App + "/" + scope.Caller.Member }

func (s *Service) ready(ctx context.Context, scope svc.Scope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// An unwired skeleton has no persistent draft capability.
	if s.cfg.Root == "" {
		return bridge.NotBuilt("shape")
	}
	if !query.ValidName(scope.App) || !validMember(scope.Caller.Member) {
		return bridge.Errorf(bridge.CodeInvalid, "invalid draft app or member")
	}
	return s.loadErr
}

func validMember(member string) bool {
	return member != "" && member != "." && member != ".." && !strings.ContainsAny(member, "/\\\x00") && !filepath.IsAbs(member)
}

// draftFor is the pipeline's lookup door. Callers hold s.mu while using a
// draft; its mutation helpers persist files and metadata before publishing.
func (s *Service) draftFor(scope svc.Scope) (*draft, error) {
	d := s.drafts[draftKey(scope)]
	if d == nil {
		return nil, bridge.Errorf(bridge.CodeNotFound, "no draft")
	}
	return d, nil
}

func (s *Service) Open(ctx context.Context, scope svc.Scope, _ bridge.ShapeOpenParams) (bridge.ShapeOpenResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx, scope); err != nil {
		return bridge.Draft{}, err
	}
	if d := s.drafts[draftKey(scope)]; d != nil {
		s.startDraft(scope)
		changed, err := d.refreshOverlay(ctx)
		if err != nil {
			return bridge.Draft{}, err
		}
		if changed {
			if err := d.save(); err != nil {
				return bridge.Draft{}, err
			}
		}
		return d.snapshot(), nil
	}
	d, err := s.createDraft(ctx, scope)
	if err != nil {
		return bridge.Draft{}, err
	}
	s.drafts[draftKey(scope)] = d
	d.publish()
	return d.snapshot(), nil
}

func (s *Service) createDraft(ctx context.Context, scope svc.Scope) (*draft, error) {
	if s.all == nil || s.all.Store == nil {
		return nil, fmt.Errorf("shape requires the app store")
	}
	def, err := s.all.Store.Definition(ctx, scope.App)
	if err != nil {
		return nil, err
	}
	if def == nil || def.App == nil || def.App.Version < 1 {
		return nil, bridge.Errorf(bridge.CodeConflict, "only a kept app can be shaped")
	}
	parent := filepath.Join(s.cfg.Root, scope.App, "drafts")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	dir := filepath.Join(parent, scope.Caller.Member)
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return nil, err
		}
		return nil, bridge.Errorf(bridge.CodeConflict, "draft folder already exists")
	}
	stage, err := os.MkdirTemp(parent, ".opening-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	d := &draft{Draft: bridge.Draft{App: scope.App, Member: scope.Caller.Member, Base: def.App.Version, State: "open", Changes: []bridge.Change{}}, BaseFiles: cloneFiles(def.Files), Diffs: map[string][]bridge.FileDiff{}, s: s, dir: stage}
	if err := writeFiles(filepath.Join(stage, "files"), def.Files); err != nil {
		return nil, err
	}
	if err := d.copyOverlay(ctx); err != nil {
		return nil, err
	}
	if err := d.persist(); err != nil {
		return nil, err
	}
	if err := os.Rename(stage, dir); err != nil {
		return nil, err
	}
	d.dir = dir
	return d, nil
}

func (s *Service) Get(ctx context.Context, scope svc.Scope, _ bridge.ShapeGetParams) (bridge.ShapeGetResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx, scope); err != nil {
		return bridge.ShapeGetResult{}, err
	}
	if d := s.drafts[draftKey(scope)]; d != nil {
		s.startDraft(scope)
		value := d.snapshot()
		return bridge.ShapeGetResult{Draft: &value}, nil
	}
	return bridge.ShapeGetResult{}, nil
}

func (s *Service) Discard(ctx context.Context, scope svc.Scope, _ bridge.ShapeDiscardParams) (bridge.ShapeDiscardResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx, scope); err != nil {
		return bridge.Empty{}, err
	}
	d, err := s.draftFor(scope)
	if err != nil {
		return bridge.Empty{}, err
	}
	lock := records.DraftOverlayLock(s.cfg.Root, scope)
	lock.Lock()
	defer lock.Unlock()
	if err := os.RemoveAll(d.dir); err != nil {
		return bridge.Empty{}, err
	}
	delete(s.drafts, draftKey(scope))
	d.State = "discarded"
	d.UpdatedAt = timestamp()
	d.publish()
	return bridge.Empty{}, nil
}

func (s *Service) reload() error {
	if s.cfg.Root == "" {
		return nil
	}
	paths, err := filepath.Glob(filepath.Join(s.cfg.Root, "*", "drafts", "*", "draft.json"))
	if err != nil {
		return err
	}
	for _, name := range paths {
		// An interrupted first open never committed its member directory.
		if strings.HasPrefix(filepath.Base(filepath.Dir(name)), ".opening-") {
			continue
		}
		d := &draft{s: s, dir: filepath.Dir(name)}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, d); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		app := filepath.Base(filepath.Dir(filepath.Dir(d.dir)))
		if !query.ValidName(d.App) || !validMember(d.Member) || app != d.App || filepath.Base(d.dir) != d.Member || d.BaseFiles == nil || d.Diffs == nil {
			return fmt.Errorf("%s: invalid draft metadata", name)
		}
		// Metadata is the commit point; rebuild files if a crash interrupted a write.
		files, err := d.rebuild()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := replaceFiles(d.dir, files); err != nil {
			return err
		}
		s.drafts[d.App+"/"+d.Member] = d
	}
	return nil
}

func timestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (d *draft) snapshot() bridge.Draft {
	data, _ := json.Marshal(d.Draft)
	var value bridge.Draft
	_ = json.Unmarshal(data, &value)
	return value
}

func (d *draft) persist() error {
	d.UpdatedAt = timestamp()
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(d.dir, "draft.json"), append(data, '\n'))
}

func (d *draft) save() error {
	if err := d.persist(); err != nil {
		return err
	}
	d.publish()
	return nil
}

func (d *draft) publish() {
	if d.s.all != nil && d.s.all.Hub != nil {
		value := d.snapshot()
		d.s.all.Hub.Publish(bridge.TopicShape(d.App, d.Member), bridge.ShapeEvent{Kind: "draft", Draft: &value, At: d.UpdatedAt})
	}
}

func atomicWrite(name string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(name), ".draft-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), name)
}
