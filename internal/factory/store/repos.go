package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// reposDoc is <root>/repos.json: the forge repositories this floor watches,
// each `owner/name`. A hand-written bare array of the same strings is read
// too, because until the picker exists that file is how a person says which
// repositories to watch.
type reposDoc struct {
	Schema int      `json:"schema"`
	Repos  []string `json:"repos"`
	// OnSince is when each repository on the list was last turned on, keyed
	// by its lowercased `owner/name`. A repository on the list with no entry
	// was turned on before this was kept, and holds nothing back
	// ([Store.OnSince]).
	OnSince map[string]time.Time `json:"on_since,omitempty"`
}

// ReposPath is <root>/repos.json.
func (st *Store) ReposPath() string { return filepath.Join(st.root, "repos.json") }

// Repos answers the watched repositories, cleaned, deduplicated and sorted.
// NO FILE IS NO REPOSITORIES and no error: a floor nobody connected to a forge
// is the ordinary floor.
func (st *Store) Repos() ([]string, error) {
	repos, _, err := st.readRepos()
	return repos, err
}

// OnSince is when repo was last turned on, and false when the list does not
// say: it is off, or it was turned on before the time was kept.
//
// TURNING A REPOSITORY ON NEVER STARTS WHAT WAS ALREADY OPEN ON IT. The poll
// holds every issue opened before this time in Backlog until a person starts
// it ([factory.Item.SetupHeld]), exactly as an organization's first import
// does, so its start labels apply only to issues opened after it was turned
// on. Turning on a repository with a backlog of labelled issues once queued
// every one of them in the same minute, while the page said nothing runs
// until someone starts it.
func (st *Store) OnSince(repo string) (time.Time, bool) {
	d, _, err := st.readReposDoc()
	if err != nil {
		return time.Time{}, false
	}
	at, ok := d.OnSince[strings.ToLower(strings.TrimSpace(repo))]
	return at, ok && !at.IsZero()
}

// RepoSwitch is the watched list as the factory's power switch per
// repository ([factory.RepoSwitch]): on the list is on, off it is off. The
// switch exists once the file does, so a floor that never kept a list has
// nothing off, and a list saved empty has everything off.
func (st *Store) RepoSwitch() (factory.RepoSwitch, error) {
	repos, kept, err := st.readRepos()
	if err != nil {
		return factory.RepoSwitch{}, err
	}
	return factory.NewRepoSwitch(repos, kept), nil
}

// readRepos is the one read of repos.json: the cleaned list, and whether the
// file is there at all.
func (st *Store) readRepos() ([]string, bool, error) {
	d, kept, err := st.readReposDoc()
	if err != nil || !kept {
		return nil, false, err
	}
	return CleanRepos(d.Repos), true, nil
}

// readReposDoc is repos.json as written, and whether it is there at all.
func (st *Store) readReposDoc() (reposDoc, bool, error) {
	if st == nil {
		return reposDoc{}, false, nil
	}
	data, err := os.ReadFile(st.ReposPath())
	if err != nil {
		if os.IsNotExist(err) {
			return reposDoc{}, false, nil
		}
		return reposDoc{}, false, err
	}
	var d reposDoc
	if err := json.Unmarshal(data, &d); err != nil {
		var bare []string
		if json.Unmarshal(data, &bare) != nil {
			return reposDoc{}, false, fmt.Errorf("factory store: cannot read repos.json: %w", err)
		}
		d = reposDoc{Repos: bare}
	}
	if d.Schema > Schema {
		return reposDoc{}, false, fmt.Errorf("%w: repos.json", ErrNewer)
	}
	return d, true, nil
}

// SetRepos writes the watched repositories, temp+rename under the file's own
// flock. An entry that is not `owner/name` is refused, so a typo never becomes
// a repository the poll asks GitHub about every minute.
func (st *Store) SetRepos(repos []string) error {
	if st == nil {
		return errors.New("factory store: no store")
	}
	for _, r := range repos {
		if !RepoName(r) {
			return fmt.Errorf("factory store: %q is not owner/name", strings.TrimSpace(r))
		}
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, "repos.lock"), func() error {
		// Each repository keeps the time it was turned on ([Store.OnSince]);
		// one newly on the list is stamped now. A list kept before times were
		// kept stamps none of what it already watched, so nothing it already
		// had is held back by an upgrade.
		was, _, err := st.readReposDoc()
		if err != nil {
			return err
		}
		on := map[string]bool{}
		for _, r := range CleanRepos(was.Repos) {
			on[strings.ToLower(r)] = true
		}
		next := reposDoc{Schema: Schema, Repos: CleanRepos(repos)}
		now := time.Now().UTC()
		for _, r := range next.Repos {
			key := strings.ToLower(r)
			at, had := was.OnSince[key]
			if on[key] && !had {
				continue
			}
			if !on[key] {
				at = now
			}
			if next.OnSince == nil {
				next.OnSince = map[string]time.Time{}
			}
			next.OnSince[key] = at
		}
		data, err := json.MarshalIndent(next, "", "  ")
		if err != nil {
			return err
		}
		return writeAtomic(st.ReposPath(), append(data, '\n'))
	})
}

// RepoName says whether s is `owner/name` ([factory.RepoName], which the
// router shares without depending on the store).
func RepoName(s string) bool { return factory.RepoName(s) }

// CleanRepos trims, drops what is not `owner/name`, deduplicates without
// regard to case (GitHub's own rule) and sorts.
func CleanRepos(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range in {
		r = strings.TrimSpace(r)
		if !RepoName(r) || seen[strings.ToLower(r)] {
			continue
		}
		seen[strings.ToLower(r)] = true
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}
