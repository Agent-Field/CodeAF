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

// PipelineLoader keeps parsed files by blob SHA and repository reads until the
// next intake tick, or [PipelineFresh] when nothing ticks. Local checkouts are
// only used when the forge is offline. mu guards the maps and is never held
// while the forge is asked; at most one ask per repository is out at a time.
type PipelineLoader struct {
	mu     sync.Mutex
	api    func(context.Context) PipelineAPI
	repos  map[string]pipelineRead
	blobs  map[string]pipelineRead
	asking behind
	absent absences
	now    func() time.Time
}

func NewPipelineLoader(api func(context.Context) PipelineAPI) *PipelineLoader {
	return &PipelineLoader{api: api, repos: map[string]pipelineRead{}, blobs: map[string]pipelineRead{}, now: time.Now}
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
	l.repos[repoKey(repo)] = seen
}

// repoKey is the name a read is kept under: GitHub's names ignore case, and a
// job's spelling may differ from the watched repository's.
func repoKey(repo string) string { return strings.ToLower(repo) }

// Load returns independent file data so personal limits cannot alter the cache.
// It waits for the forge when the read is missing or stale, so a start, an edit
// or a merge decides by the file main has now; under [ShowOnly] it waits at
// most [FirstWait] and answers with the last file, else the checkout's, while
// the forge's answer lands for the next read.
func (l *PipelineLoader) Load(ctx context.Context, repo, dir, personal string) (pipeline.File, error) {
	var bound time.Duration
	if showOnly(ctx) {
		bound = FirstWait
	}
	return l.load(ctx, repo, dir, personal, bound)
}

func (l *PipelineLoader) load(ctx context.Context, repo, dir, personal string, bound time.Duration) (pipeline.File, error) {
	key := repoKey(repo)
	l.mu.Lock()
	seen, ok := l.repos[key]
	// A checkout used offline can change between reads without an intake tick.
	// Its git read already chooses the main branch, so keep those edits visible.
	if ok && seen.local && strings.TrimSpace(dir) != "" {
		at := seen.at
		seen = l.local(dir, nil)
		seen.at = at
		l.repos[key] = seen
	}
	// A READ THAT HAD NO GITHUB IS ASKED AGAIN: once it is old, and at once by
	// an action, which must never decide on a failure GitHub never gave.
	stale := !ok || l.now().Sub(seen.at) > PipelineFresh || (bound == 0 && seen.local && seen.err != nil)
	l.mu.Unlock()
	switch {
	case stale && bound == 0:
		// An action reads under its own context, never behind another's ask.
		seen, ok = l.read(ctx, repo, dir), true
		l.mu.Lock()
		seen.at = l.now()
		l.repos[key] = seen
		l.mu.Unlock()
	case stale:
		l.ask(repo, dir).wait(ctx, bound)
		l.mu.Lock()
		if now, known := l.repos[key]; known {
			seen, ok = now, true
		}
		l.mu.Unlock()
	}
	if !ok {
		// Not heard yet: the checkout's file, else the defaults, with no problem
		// to show, since nothing is wrong yet.
		seen = l.local(dir, nil)
	}
	return pipeline.MergePersonal(seen.file, personal, seen.err)
}

// ask asks the forge for repo's file behind the reads, one ask at a time. An
// ask that times out keeps the last file and waits [PipelineFresh] before the
// next; the forge is never asked while mu is held.
func (l *PipelineLoader) ask(repo, dir string) *ask {
	return l.asking.start(repo, "factory/pipeline read", func(ctx context.Context) {
		seen := l.read(ctx, repo, dir)
		if errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		if previous, known := l.repos[repoKey(repo)]; known && ctx.Err() != nil {
			seen = previous
		}
		seen.at = l.now()
		l.repos[repoKey(repo)] = seen
	})
}

// Close cancels any background pipeline read and waits up to [closeWait] for it
// to finish.
func (l *PipelineLoader) Close() {
	if l == nil {
		return
	}
	l.asking.close(closeWait)
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
	previous := l.repos[repoKey(repo)]
	l.mu.Unlock()
	tag := ""
	if previous.path == pipeline.RepoFile {
		tag = previous.tag
	}
	text, sha, tag, err := l.absent.contents(ctx, api, l.now(), owner, name, pipeline.RepoFile, tag)
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
		text, sha, tag, err = l.absent.contents(ctx, api, l.now(), owner, name, factory.RecipeFile, tag)
		if errors.Is(err, forge.ErrNotModified) {
			return previous
		}
		if missing(err) {
			if err := l.absent.repoAnswers(ctx, api, l.now(), owner, name); err != nil {
				return l.failure(dir, err)
			}
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
	return l.problem(refusal(err))
}

// refusal gives a forge's HTTP answer the words a person sees.
func refusal(err error) error {
	var apiErr *forge.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	why := fmt.Sprintf("GitHub answered %d when asked for it; check this computer's GitHub connection", apiErr.StatusCode)
	if apiErr.StatusCode == http.StatusNotFound {
		why = "GitHub can't show this repository with this computer's connection"
	}
	return &pipeline.ReadRefusal{Why: why, Err: err}
}

func (l *PipelineLoader) local(dir string, offline error) pipelineRead {
	if strings.TrimSpace(dir) != "" {
		f, err := pipeline.Load(dir)
		return pipelineRead{file: f, err: err, local: true}
	}
	if offline != nil {
		seen := l.problem(fmt.Errorf("%w: %v", pipeline.ErrNotHereYet, offline))
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
