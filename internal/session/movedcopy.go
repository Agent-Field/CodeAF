package session

import "path/filepath"

// RebaseOwnedCopy updates only the derived workspace after filing a moved
// copy. Its truth already spells paths against the workspace base; resolving
// those paths with the old base and writing them back would strand references
// under the staging folder instead of letting them follow the copy.
func RebaseOwnedCopy(dir string) error {
	return withMetaLock(dir, func() error {
		meta, err := readMeta(dir)
		if err != nil {
			return err
		}
		meta.Workspace = (Place{Dir: dir, Owned: true}).Work()
		return writeJSONAtomic(filepath.Join(dir, placeMeta), meta)
	})
}
