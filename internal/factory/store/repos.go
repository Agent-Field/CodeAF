package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// reposDoc is <root>/repos.json: the forge repositories this floor watches,
// each `owner/name`. A hand-written bare array of the same strings is read
// too, because until the picker exists that file is how a person says which
// repositories to watch.
type reposDoc struct {
	Schema int      `json:"schema"`
	Repos  []string `json:"repos"`
}

// ReposPath is <root>/repos.json.
func (st *Store) ReposPath() string { return filepath.Join(st.root, "repos.json") }

// Repos answers the watched repositories, cleaned, deduplicated and sorted.
// NO FILE IS NO REPOSITORIES and no error: a floor nobody connected to a forge
// is the ordinary floor.
func (st *Store) Repos() ([]string, error) {
	if st == nil {
		return nil, nil
	}
	data, err := os.ReadFile(st.ReposPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var d reposDoc
	if err := json.Unmarshal(data, &d); err != nil {
		var bare []string
		if json.Unmarshal(data, &bare) != nil {
			return nil, fmt.Errorf("factory store: cannot read repos.json: %w", err)
		}
		d.Repos = bare
	}
	if d.Schema > Schema {
		return nil, fmt.Errorf("%w: repos.json", ErrNewer)
	}
	return CleanRepos(d.Repos), nil
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
	data, err := json.MarshalIndent(reposDoc{Schema: Schema, Repos: CleanRepos(repos)}, "", "  ")
	if err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, "repos.lock"), func() error {
		return writeAtomic(st.ReposPath(), append(data, '\n'))
	})
}

// RepoName says whether s is `owner/name`: two non-empty parts, no spaces,
// nothing that could walk out of an API path.
func RepoName(s string) bool {
	owner, name, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return false
	}
	for _, part := range []string{owner, name} {
		if part == "." || part == ".." || strings.ContainsAny(part, " \t\n?#%\\") {
			return false
		}
	}
	return true
}

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
