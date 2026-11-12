package appwire

import (
	"path/filepath"
	"strings"
)

// FeatureMediaResolve advertises recorded reference resolution without a path grant.
const FeatureMediaResolve = "media.resolve"

// MediaReferenceConflict describes a stale expected file version.
type MediaReferenceConflict struct {
	Reason string `json:"reason" enum:"changed"`
}

// MediaReferenceResolver is optional. It resolves a recorded owner's canonical
// reference; it neither discovers files nor grants a renderer a path lease.
type MediaReferenceResolver interface {
	ResolveMediaReference(agent, path, expectVersion string) (MediaSource, error)
}

func init() {
	methods["media.resolve"] = typed(func(e Engine, p struct {
		Agent         string `json:"agent"`
		Path          string `json:"path"`
		ExpectVersion string `json:"expect_version"`
	}) (*MediaResult, error) {
		r, ok := e.(MediaReferenceResolver)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve media.resolve")
		}
		if strings.TrimSpace(p.Agent) == "" || len(p.Agent) > 128 || p.Path == "" || !filepath.IsAbs(p.Path) || filepath.Clean(p.Path) != p.Path || len(p.Path) > 4096 || len(p.ExpectVersion) > 256 || strings.ContainsRune(p.Path, 0) {
			return nil, Errorf(CodeBadParams, "media.resolve needs an agent and a bounded canonical path")
		}
		source, err := r.ResolveMediaReference(p.Agent, p.Path, p.ExpectVersion)
		if err != nil {
			return nil, err
		}
		if size, err := MediaSourceSize(source); err != nil || size+64 > MediaReplyBudget {
			return nil, mediaReplyError()
		}
		return &MediaResult{Source: source}, nil
	})
}
