package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// PipelineAPI is the forge's default-branch file door, shared by intake and the seam.
type PipelineAPI interface {
	Contents(context.Context, string, string, string, string, string) (string, string, string, error)
}

type pipelineRead struct {
	file  pipeline.File
	err   error
	path  string
	tag   string
	local bool
}

// PipelineLoader keeps parsed files by blob SHA and repository reads until the
// next intake tick. Local checkouts are only used when the forge is offline.
type PipelineLoader struct {
	mu    sync.Mutex
	api   func(context.Context) PipelineAPI
	repos map[string]pipelineRead
	blobs map[string]pipelineRead
}

func NewPipelineLoader(api func(context.Context) PipelineAPI) *PipelineLoader {
	return &PipelineLoader{api: api, repos: map[string]pipelineRead{}, blobs: map[string]pipelineRead{}}
}

// Refresh replaces the repository read on each intake tick, including failures
// and missing files, so fixing or adding the file needs no process restart.
func (l *PipelineLoader) Refresh(ctx context.Context, repo, dir string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.repos[repo] = l.read(ctx, repo, dir)
}

// Load returns independent file data so personal limits cannot alter the cache.
func (l *PipelineLoader) Load(ctx context.Context, repo, dir, personal string) (pipeline.File, error) {
	l.mu.Lock()
	seen, ok := l.repos[repo]
	// A checkout used offline can change between reads without an intake tick.
	// Its git read already chooses the main branch, so keep those edits visible.
	if ok && seen.local && strings.TrimSpace(dir) != "" {
		seen = l.local(dir, nil)
		l.repos[repo] = seen
	}
	if !ok {
		seen = l.read(ctx, repo, dir)
		l.repos[repo] = seen
	}
	l.mu.Unlock()
	return pipeline.MergePersonal(seen.file, personal, seen.err)
}

func (l *PipelineLoader) read(ctx context.Context, repo, dir string) pipelineRead {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return l.local(dir, nil)
	}
	var api PipelineAPI
	if l.api != nil {
		api = l.api(ctx)
	}
	if api == nil {
		return l.local(dir, errors.New("github is not connected"))
	}
	previous := l.repos[repo]
	tag := ""
	if previous.path == pipeline.RepoFile {
		tag = previous.tag
	}
	text, sha, tag, err := api.Contents(ctx, owner, name, pipeline.RepoFile, "", tag)
	if errors.Is(err, forge.ErrNotModified) {
		return previous
	}
	if missing(err) {
		// Markdown remains a supported predecessor, but an authoritative absence
		// on the forge never reads a file from the local job branch.
		tag = ""
		if previous.path == factory.RecipeFile {
			tag = previous.tag
		}
		text, sha, tag, err = api.Contents(ctx, owner, name, factory.RecipeFile, "", tag)
		if errors.Is(err, forge.ErrNotModified) {
			return previous
		}
		if missing(err) {
			f := pipeline.Defaults()
			f.Source = pipeline.SourceDefaults
			f.From = factory.FromDefault
			return pipelineRead{file: f}
		}
		if err != nil {
			return l.failure(dir, err)
		}
		if err := pipeline.ValidateText(text); err != nil {
			return l.problem(err)
		}
		key := "markdown:" + sha
		if seen, ok := l.blobs[key]; ok && sha != "" {
			seen.tag, seen.path = tag, factory.RecipeFile
			return seen
		}
		f := pipeline.FromMarkdown(text)
		f.Source = pipeline.SourceMarkdown
		f.From = factory.FromMainBranch
		seen := pipelineRead{file: f, path: factory.RecipeFile, tag: tag}
		if sha != "" {
			l.blobs[key] = seen
		}
		return seen
	}
	if err != nil {
		return l.failure(dir, err)
	}
	if err := pipeline.ValidateText(text); err != nil {
		return l.problem(err)
	}
	key := "yaml:" + sha
	if seen, ok := l.blobs[key]; ok && sha != "" {
		seen.tag, seen.path = tag, pipeline.RepoFile
		return seen
	}
	f, err := pipeline.Parse(text)
	f.Source = pipeline.SourceFile
	f.From = factory.FromMainBranch
	seen := pipelineRead{file: f, err: err, path: pipeline.RepoFile, tag: tag}
	if sha != "" {
		l.blobs[key] = seen
	}
	return seen
}

func missing(err error) bool {
	var apiErr *forge.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func (l *PipelineLoader) failure(dir string, err error) pipelineRead {
	// HTTP refusals and unreadable contents are visible failures. Only a
	// transport outage can substitute the local default-branch copy.
	if forge.IsTransportError(err) {
		return l.local(dir, err)
	}
	return l.problem(err)
}

func (l *PipelineLoader) local(dir string, offline error) pipelineRead {
	if strings.TrimSpace(dir) != "" {
		f, err := pipeline.Load(dir)
		return pipelineRead{file: f, err: err, local: true}
	}
	if offline != nil {
		seen := l.problem(offline)
		seen.local = true
		return seen
	}
	f := pipeline.Defaults()
	f.Source = pipeline.SourceDefaults
	return pipelineRead{file: f, local: true}
}

func (l *PipelineLoader) problem(err error) pipelineRead {
	f := pipeline.Defaults()
	f.Source = pipeline.SourceFile
	f.From = factory.FromMainBranch
	f.Problems = []pipeline.Problem{{Where: pipeline.RepoFile, Why: err.Error()}}
	return pipelineRead{file: f, err: fmt.Errorf("%s: %w", pipeline.RepoFile, err)}
}
