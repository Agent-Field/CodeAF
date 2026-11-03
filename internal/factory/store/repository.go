package store

import (
	"context"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// repositoryIDs separates a machine's checkout aliases from GitHub identities.
// An ambiguous short name never selects a repository belonging to another owner.
type repositoryIDs struct {
	aliases map[string]string
	keys    map[string]string
	dirs    map[string]string
}

func (ids repositoryIDs) alias(key, full string) {
	key = strings.ToLower(strings.TrimSpace(key))
	if old, ok := ids.aliases[key]; ok && old != full {
		ids.aliases[key] = ""
	} else if !ok {
		ids.aliases[key] = full
	}
}

func (ids repositoryIDs) resolve(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if RepoName(key) {
		return key
	}
	if full := ids.keys[key]; full != "" {
		return full
	}
	return ids.aliases[key]
}

func (st *Store) repositoryIdentities() (repositoryIDs, error) {
	ids := repositoryIDs{aliases: map[string]string{}, keys: map[string]string{}, dirs: map[string]string{}}
	watched, err := st.Repos()
	if err != nil {
		return ids, err
	}
	for _, full := range watched {
		full = strings.ToLower(full)
		_, name, _ := strings.Cut(full, "/")
		ids.alias(name, full)
	}
	records, err := st.checkoutRecords()
	if err != nil {
		return ids, err
	}
	// Full keys win over an older alias for the same repository, independent
	// of map iteration order. Reading a remote is local and bounded.
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		full := strings.ToLower(strings.TrimSpace(key))
		if !RepoName(full) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			out, err := exec.CommandContext(ctx, "git", "-C", records[key], "remote", "get-url", "origin").Output()
			cancel()
			full = ""
			if err == nil {
				full = githubRemoteName(strings.TrimSpace(string(out)))
			}
		}
		if full == "" {
			continue
		}
		// An actual checkout key has its own origin. That identity takes
		// precedence over a coincidentally equal watched repository basename.
		ids.keys[strings.ToLower(strings.TrimSpace(key))] = full
		_, name, _ := strings.Cut(full, "/")
		ids.alias(name, full)
		if ids.dirs[full] == "" || RepoName(key) {
			ids.dirs[full] = records[key]
		}
	}
	// Infer aliases only after every origin and full key has named its owner.
	// Otherwise an early basename could select an owner before a later clash.
	for _, key := range keys {
		alias := strings.ToLower(strings.TrimSpace(key))
		if ids.keys[alias] != "" {
			continue
		}
		if full := ids.resolve(key); full != "" {
			ids.keys[alias] = full
			if ids.dirs[full] == "" {
				ids.dirs[full] = records[key]
			}
		}
	}
	return ids, nil
}

func githubRemoteName(remote string) string {
	if strings.HasPrefix(remote, "git@github.com:") {
		remote = "ssh://git@github.com/" + strings.TrimPrefix(remote, "git@github.com:")
	}
	u, err := url.Parse(remote)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return ""
	}
	full := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), "/"))
	if !RepoName(full) {
		return ""
	}
	return full
}

// canonicalItem preserves unknown local jobs until their identity can be
// resolved. Product is the owner on older forge jobs, independent of checkout.
func (ids repositoryIDs) canonicalItem(it *factory.Item) bool {
	full := ids.resolve(it.Repo)
	if !RepoName(it.Repo) && RepoName(it.Product+"/"+it.Repo) {
		full = strings.ToLower(it.Product + "/" + it.Repo)
	}
	if full == "" {
		return false
	}
	old := it.Repo
	changed := full != old
	it.Repo = full
	it.Places = append([]string(nil), it.Places...)
	for i, place := range it.Places {
		next := place
		if strings.EqualFold(place, old) {
			next = full
		} else if resolved := ids.resolve(place); resolved != "" {
			next = resolved
		}
		changed = changed || next != place
		it.Places[i] = next
	}
	return changed
}

func needsRepositoryLookup(it factory.Item) bool {
	return it.Repo != "" && !RepoName(it.Repo) && !RepoName(it.Product+"/"+it.Repo)
}

func (st *Store) itemIdentities(it factory.Item) (repositoryIDs, error) {
	if needsRepositoryLookup(it) {
		return st.repositoryIdentities()
	}
	return repositoryIDs{}, nil
}

func (st *Store) normalizeRepository(it *factory.Item) error {
	ids, err := st.itemIdentities(*it)
	if err == nil {
		ids.canonicalItem(it)
	}
	return err
}

// migrateRepository re-reads under the item lock so loading a legacy job
// cannot overwrite a worker's newer state. Identity repair preserves its age.
func (st *Store) migrateRepository(d doc, ids repositoryIDs) (doc, error) {
	probe := d.Item
	if !ids.canonicalItem(&probe) {
		return d, nil
	}
	err := st.underItemLock(d.Item.ID, func() error {
		current, err := st.read(st.ItemPath(d.Item.ID))
		if err != nil {
			return err
		}
		d = current
		if !ids.canonicalItem(&d.Item) {
			return nil
		}
		d.Revision++
		data, err := marshalDoc(d)
		if err != nil {
			return err
		}
		return writeAtomic(st.ItemPath(d.Item.ID), data)
	})
	return d, err
}
