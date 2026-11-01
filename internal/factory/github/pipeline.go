package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/guard"
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
	// at is when the forge was last asked, so a read older than
	// [PipelineFresh] is asked again.
	at time.Time
}

// PipelineFresh is how long a repository's read answers without asking the
// forge again. A factory whose intake is off never refreshes its reads, and a
// pipeline change merged on GitHub must reach the next start: past this, a
// read asks again, by its ETag, so an unchanged file costs one 304.
const PipelineFresh = 15 * time.Second

// PipelineFirstWait is how long a read waits for the forge before it answers
// with what it has: the last file, else the checkout's. A READ NEVER WAITS ON
// GITHUB LONGER: a forge that did not answer once held every queue read of the
// factory until the app called it "not answering". The forge's answer lands
// for the next read.
const PipelineFirstWait = 300 * time.Millisecond

// pipelineAskWait bounds one ask of the forge behind the reads.
const pipelineAskWait = 30 * time.Second

// PipelineLoader keeps parsed files by blob SHA and repository reads until the
// next intake tick, or [PipelineFresh] when nothing ticks. Local checkouts are
// only used when the forge is offline. mu guards the maps and is never held
// while the forge is asked; at most one ask per repository is out at a time.
type PipelineLoader struct {
	mu     sync.Mutex
	api    func(context.Context) PipelineAPI
	repos  map[string]pipelineRead
	blobs  map[string]pipelineRead
	asking map[string]*pipelineAsk
	now    func() time.Time
}

// pipelineAsk is one ask of the forge that is out, and when it began.
type pipelineAsk struct {
	done  chan struct{}
	began time.Time
}

func NewPipelineLoader(api func(context.Context) PipelineAPI) *PipelineLoader {
	return &PipelineLoader{api: api, repos: map[string]pipelineRead{}, blobs: map[string]pipelineRead{}, asking: map[string]*pipelineAsk{}, now: time.Now}
}

// WithClock is the loader telling time by now, for a test of [PipelineFresh].
func (l *PipelineLoader) WithClock(now func() time.Time) *PipelineLoader {
	l.now = now
	return l
}

// Refresh replaces the repository read on each intake tick, including failures
// and missing files, so fixing or adding the file needs no process restart.
func (l *PipelineLoader) Refresh(ctx context.Context, repo, dir string) {
	seen := l.read(ctx, repo, dir)
	l.mu.Lock()
	defer l.mu.Unlock()
	seen.at = l.now()
	l.repos[repo] = seen
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
	var ask *pipelineAsk
	if !ok || (!seen.local && l.now().Sub(seen.at) > PipelineFresh) {
		ask = l.askLocked(repo, dir)
	}
	l.mu.Unlock()
	if ask == nil {
		return pipeline.MergePersonal(seen.file, personal, seen.err)
	}
	select {
	case <-ask.done:
	case <-time.After(time.Until(ask.began.Add(PipelineFirstWait))):
	case <-ctx.Done():
	}
	l.mu.Lock()
	if now, known := l.repos[repo]; known {
		seen, ok = now, true
	}
	l.mu.Unlock()
	if !ok {
		seen = l.local(dir, errors.New("github has not answered yet"))
	}
	return pipeline.MergePersonal(seen.file, personal, seen.err)
}

// askLocked asks the forge for repo's file behind the reads, unless an ask is
// out already, and answers the one that is out. An ask that times out keeps
// the last file and waits [PipelineFresh] before the next.
func (l *PipelineLoader) askLocked(repo, dir string) *pipelineAsk {
	if ask := l.asking[repo]; ask != nil {
		return ask
	}
	ask := &pipelineAsk{done: make(chan struct{}), began: time.Now()}
	l.asking[repo] = ask
	guard.Go("factory/pipeline read", func() {
		defer close(ask.done)
		ctx, cancel := context.WithTimeout(context.Background(), pipelineAskWait)
		defer cancel()
		seen := l.read(ctx, repo, dir)
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.asking, repo)
		if previous, known := l.repos[repo]; known && ctx.Err() != nil {
			seen = previous
		}
		seen.at = l.now()
		l.repos[repo] = seen
	})
	return ask
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
	l.mu.Lock()
	previous := l.repos[repo]
	l.mu.Unlock()
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
		if seen, ok := l.blob(key); ok && sha != "" {
			seen.tag, seen.path = tag, factory.RecipeFile
			return seen
		}
		f := pipeline.FromMarkdown(text)
		f.Source = pipeline.SourceMarkdown
		f.From = factory.FromMainBranch
		seen := pipelineRead{file: f, path: factory.RecipeFile, tag: tag}
		if sha != "" {
			l.keepBlob(key, seen)
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
	if seen, ok := l.blob(key); ok && sha != "" {
		seen.tag, seen.path = tag, pipeline.RepoFile
		return seen
	}
	f, err := pipeline.Parse(text)
	f.Source = pipeline.SourceFile
	f.From = factory.FromMainBranch
	seen := pipelineRead{file: f, err: err, path: pipeline.RepoFile, tag: tag}
	if sha != "" {
		l.keepBlob(key, seen)
	}
	return seen
}

func (l *PipelineLoader) blob(key string) (pipelineRead, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	seen, ok := l.blobs[key]
	return seen, ok
}

func (l *PipelineLoader) keepBlob(key string, seen pipelineRead) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.blobs[key] = seen
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
