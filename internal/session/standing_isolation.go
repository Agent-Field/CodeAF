package session

import "time"

// rememberStandingIsolation gives the copy its durable owner before opening a
// worker. A failed worker constructor must not leave a branch nobody can find.
// New reads this same metadata, so successful startup needs no second record.
func rememberStandingIsolation(cfg Config, folder string, tree taskTree) error {
	return withMetaLock(cfg.Place.Dir, func() error {
		meta, err := LoadMeta(cfg.Place.Dir)
		if err != nil {
			return err
		}
		if meta.ID == "" {
			meta.ID = cfg.Place.ID()
		}
		meta.Workspace = cfg.Workspace
		meta.Trees = append(meta.Trees, StandingTree{
			Folder: folder, Dir: tree.dir, Branch: tree.branch, Root: tree.root,
			Home: tree.home, HomeSha: tree.homeSha, Mode: TaskModeWorktree,
			Cut: time.Now(),
		})
		return SaveMeta(cfg.Place.Dir, meta)
	})
}
