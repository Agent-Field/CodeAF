package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// Run only while owning serve.lock, before exposing the floor or starting its
// workers. Standalone PR jobs are removed, including worker mirrors; an issue's
// attached PR, local branch and publication/CI history are untouched.
func forgetFactoryPRJobs(ctx context.Context, st *store.Store, profile string) error {
	sharedHistory := map[int]bool{}
	roots := []string{st.Root()}
	mirrors, err := filepath.Glob(filepath.Join(st.Root(), "workers", "*", "*"))
	if err != nil {
		return err
	}
	roots = append(roots, mirrors...)
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			continue
		}
		floor, err := store.Open(root)
		if err != nil {
			return err
		}
		items, err := floor.List()
		if err != nil {
			return err
		}
		ids := map[int]bool{}
		var prs []factory.Item
		for _, it := range items {
			if it.Kind == factory.KindPR {
				ids[it.ID] = true
				prs = append(prs, it)
				// Workers record events on the main floor. Do not erase an
				// unrelated local issue that has the same ID as a remote job.
				primary, e := st.Get(it.ID)
				if e != nil && !errors.Is(e, store.ErrNotFound) {
					return e
				}
				if errors.Is(e, store.ErrNotFound) || primary.Kind == factory.KindPR {
					sharedHistory[it.ID] = true
				}
			}
		}
		if len(prs) == 0 {
			continue
		}
		desk, err := lease.Open(floor, lease.Options{})
		if err != nil {
			return err
		}
		if err := desk.ForgetJobs(ids); err != nil {
			return err
		}
		// Worker mirrors contain snapshots; their worktrees and checkouts live
		// on this machine's main floor, just like buildFactoryRunner.
		folders := newFactoryJobFolders(st)
		for _, it := range prs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := forgetPRJobChats(profile, floor, it); err != nil {
				return err
			}
			// Every branch removed here was minted for this factory job, never the
			// PR author's branch. Workdirs validates the recorded branch shape.
			local := it
			local.Kind = factory.KindIssue
			if err := folders.Discard(local); err != nil {
				return err
			}
			for _, workRoot := range []string{st.Root(), root} {
				work := factoryrun.NewWorkdirs(filepath.Join(workRoot, "work"), factoryrun.ExecGit{})
				if err := os.RemoveAll(work.Dir(it)); err != nil {
					return err
				}
			}
			if err := os.RemoveAll(filepath.Join(root, "chats", strconv.Itoa(it.ID))); err != nil {
				return err
			}
			if err := floor.SetBusy(it.ID, ""); err != nil {
				return err
			}
			if err := floor.SetMarks([]int{it.ID}, false); err != nil {
				return err
			}
		}
		history, err := record.Open(floor, record.Options{})
		if err != nil {
			return err
		}
		if err := history.ForgetJobs(ctx, ids); err != nil {
			return err
		}
		if err := live.ForgetJobs(root, ids); err != nil {
			return err
		}
		for _, it := range prs {
			if err := floor.ForgetPRJob(it.ID); err != nil {
				return err
			}
		}
	}
	if len(sharedHistory) != 0 {
		history, err := record.Open(st, record.Options{})
		if err != nil {
			return err
		}
		if err := history.ForgetJobs(ctx, sharedHistory); err != nil {
			return err
		}
		if err := live.ForgetJobs(st.Root(), sharedHistory); err != nil {
			return err
		}
	}
	return nil
}

// Normal deletion retains a tombstone and metadata to prevent stale owners
// reopening a chat. These deprecated jobs have no recovery path or reusable ID;
// after owners have closed, erase those final local files too.
func forgetPRJobChats(profile string, floor *store.Store, it factory.Item) error {
	profile = factoryProfile(profile)
	f, err := teams.Snapshot(profile)
	if err != nil {
		return err
	}
	root := session.PlacesRoot()
	files := jobChatsIn(f, it, jobRoomIn(f, it))
	files = append(files, ownedJobChats(root, it, []string{floor.Root()})...)
	if err := factoryDeleteJobChats(profile, floor)(it); err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for _, file := range files {
		if filepath.Base(file) != "transcript.jsonl" {
			return fmt.Errorf("unexpected factory chat file")
		}
		dir, err := filepath.EvalSymlinks(filepath.Dir(file))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, dir)
		if err != nil || strings.HasPrefix(rel, "..") || len(strings.Split(rel, string(filepath.Separator))) != 2 {
			return fmt.Errorf("factory chat directory is outside the conversation store")
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		_ = os.Remove(filepath.Dir(dir)) // Remove only an empty workspace bucket.
	}
	return nil
}
