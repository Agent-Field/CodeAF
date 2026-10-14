package builder

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// LeftOutAutomation names work omitted so the rest of the app can be previewed.
type LeftOutAutomation struct {
	File   string `json:"file"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type BuildResult struct {
	LeftOut []LeftOutAutomation `json:"left_out"`
}

// ResultEvent adds the build result to the existing progress envelope.
type ResultEvent struct {
	bridge.BuildEvent
	BuildResult
}

type savedResult struct {
	BuildResult
	Original spec.Files `json:"original,omitempty"`
}

// Result exposes the durable result for the apps.get contract adapter.
func (s *Service) Result(ctx context.Context, scope svc.Scope) (BuildResult, error) {
	b, err := s.buildFor(ctx, scope.App)
	if err != nil {
		return BuildResult{}, err
	}
	return b.result().BuildResult, nil
}

func (b *build) result() savedResult {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := b.saved
	result.LeftOut = append([]LeftOutAutomation{}, result.LeftOut...)
	return result
}

func (s *Service) saveResult(b *build, result savedResult) error {
	if s.root != "" {
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		dir := filepath.Join(s.root, b.app)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(dir, ".build-result-*")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if _, err = tmp.Write(data); err != nil {
			tmp.Close()
			return err
		}
		if err = tmp.Close(); err != nil {
			return err
		}
		if err = os.Rename(tmp.Name(), filepath.Join(dir, "build-result.json")); err != nil {
			return err
		}
	}
	b.mu.Lock()
	b.saved = result
	b.mu.Unlock()
	return nil
}

func (s *Service) loadResult(b *build) error {
	if s.root == "" {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(s.root, b.app, "build-result.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, &b.saved)
}

// ResultDetail is the apps.get envelope with the Builder's durable result.
type ResultDetail struct {
	bridge.AppDetail
	BuildResult
}

// AppDetail is the optional apps.get adapter for the gateway lane. Returning
// any keeps the existing service interface intact while the wire grows a field.
func (s *Service) AppDetail(ctx context.Context, scope svc.Scope) (any, error) {
	detail, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return nil, err
	}
	if raw := detail.Files[spec.AppFile]; raw != "" {
		var app spec.App
		if err := json.Unmarshal([]byte(raw), &app); err != nil {
			return nil, err
		}
		detail.Needs = caps.Needs(&app)
	}
	result, err := s.Result(ctx, scope)
	if err != nil {
		return nil, err
	}
	return ResultDetail{AppDetail: detail, BuildResult: result}, nil
}
