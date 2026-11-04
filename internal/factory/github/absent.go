package github

import (
	"context"
	"net/http"
	"sync"
	"time"

	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// MissingFresh is how long the forge's answer that a file does not exist
// stands before it is asked again. A 404 carries no ETag, so unlike a file
// that exists, a missing one cost a counted call on every intake tick: an idle
// factory asked for its two pipeline names and three CODEOWNERS places about
// four times a minute. A file added on GitHub is read within this long.
const MissingFresh = 5 * time.Minute

// errMissing is the remembered answer: the forge's own 404.
var errMissing = &forge.APIError{StatusCode: http.StatusNotFound, Body: "missing when last asked"}

// absences remembers which files of which repositories the forge said are
// missing, and when.
type absences struct {
	mu sync.Mutex
	at map[string]time.Time
	// answered is when each repository last answered: only then does a 404 for
	// a file in it mean there is no file.
	answered map[string]time.Time
}

// repoReader is a forge that can say whether a repository itself answers.
type repoReader interface {
	DefaultBranch(ctx context.Context, owner, repo string) (string, error)
}

// repoAnswers is nil when the repository itself answers. GitHub says 404 for a
// private repository a connection can't see as it does for a file that is not
// there, and a read taken for "no file" would run the job on the default rules.
// An answer stands for [MissingFresh], as the absence it confirms does.
func (a *absences) repoAnswers(ctx context.Context, api PipelineAPI, now time.Time, owner, name string) error {
	reader, ok := api.(repoReader)
	if !ok {
		return nil
	}
	key := owner + "/" + name
	a.mu.Lock()
	said, known := a.answered[key]
	a.mu.Unlock()
	if known && now.Sub(said) < MissingFresh {
		return nil
	}
	if _, err := reader.DefaultBranch(ctx, owner, name); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.answered == nil {
		a.answered = map[string]time.Time{}
	}
	a.answered[key] = now
	return nil
}

// contents reads one file through api unless the forge said it is missing
// less than [MissingFresh] before now, in which case it answers that again.
func (a *absences) contents(ctx context.Context, api PipelineAPI, now time.Time, owner, name, path, tag string) (text, sha, newTag string, err error) {
	key := owner + "/" + name + "\x00" + path
	a.mu.Lock()
	said, known := a.at[key]
	a.mu.Unlock()
	if known && now.Sub(said) < MissingFresh {
		return "", "", "", errMissing
	}
	text, sha, newTag, err = api.Contents(ctx, owner, name, path, "", tag)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.at == nil {
		a.at = map[string]time.Time{}
	}
	if missing(err) {
		a.at[key] = now
	} else {
		delete(a.at, key)
	}
	return text, sha, newTag, err
}
