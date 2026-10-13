package custom

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct {
	all   *svc.Services
	mu    sync.Mutex
	cache map[string]*versionCache
}

type versionCache struct {
	version int
	files   map[string]bridge.CustomBundleResult
}

var _ svc.Custom = (*Service)(nil)

func New(_ svc.Config, all *svc.Services) *Service {
	return &Service{all: all, cache: make(map[string]*versionCache)}
}

func (s *Service) Bundle(ctx context.Context, scope svc.Scope, p bridge.CustomBundleParams) (bridge.CustomBundleResult, error) {
	if err := ctx.Err(); err != nil {
		return bridge.CustomBundleResult{}, err
	}
	if !IsFile(p.File) {
		return bridge.CustomBundleResult{}, bridge.Errorf(bridge.CodeInvalid, "file must be custom/<name>.tsx")
	}
	if s.all == nil || s.all.Store == nil {
		return bridge.CustomBundleResult{}, bridge.NotBuilt("store")
	}
	// Get supplies files and version in one snapshot so a concurrent Apply
	// cannot put a new file under an old version's cache key.
	detail, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return bridge.CustomBundleResult{}, err
	}
	return s.bundleVersion(ctx, scope.App, detail, p.File)
}

func (s *Service) bundleVersion(ctx context.Context, app string, detail bridge.AppDetail, file string) (bridge.CustomBundleResult, error) {
	source, exists := detail.Files[file]
	if !exists {
		return bridge.CustomBundleResult{}, bridge.Errorf(bridge.CodeNotFound, "no custom file "+file)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return bridge.CustomBundleResult{}, err
	}
	cache := s.cache[app]
	if cache == nil || cache.version != detail.Version {
		cache = &versionCache{version: detail.Version, files: make(map[string]bridge.CustomBundleResult)}
		s.cache[app] = cache
	}
	if result, ok := cache.files[file]; ok && result.Hash == hash {
		return result, nil
	}
	js, issues := compile(file, []byte(source))
	if len(issues) != 0 {
		return bridge.CustomBundleResult{}, &bridge.Error{Code: bridge.CodeCheckFailed, Message: "custom file failed kit/custom", Details: map[string]any{"issues": issues}}
	}
	if err := ctx.Err(); err != nil {
		return bridge.CustomBundleResult{}, err
	}
	result := bridge.CustomBundleResult{JS: js, Hash: hash}
	// A changed file replaces its entry, and Apply replaces the whole version.
	// Old Builder revisions therefore cannot accumulate compiled modules.
	cache.files[file] = result
	return result, nil
}
