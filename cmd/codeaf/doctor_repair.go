package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

const doctorSyncTimeout = 3 * time.Second

// inspectAndRepairDoctor collects engine-store and cross-computer sync health
// diagnostics and, when fix is true, repairs only what cannot touch another
// computer's live hold or discard a unique turn.
func inspectAndRepairDoctor(fix bool) (engineStore []string, syncIssues []string, fixes []string) {
	storeLines, storeFixes := inspectDoctorStores(fix)
	engineStore = append(engineStore, storeLines...)
	fixes = append(fixes, storeFixes...)

	syncLines, syncFixes := inspectDoctorSync(fix)
	syncIssues = append(syncIssues, syncLines...)
	fixes = append(fixes, syncFixes...)
	return engineStore, syncIssues, fixes
}

func doctorStoreDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(dir string) {
		clean := filepath.Clean(strings.TrimSpace(dir))
		if clean == "" || clean == "." || strings.HasSuffix(clean, ".lock") || seen[clean] {
			return
		}
		seen[clean] = true
		out = append(out, clean)
	}
	add(cellstore.SharedStoreDir())
	matches, _ := filepath.Glob(filepath.Join(home.Join("v3", "stores"), "*"))
	slices.Sort(matches)
	for _, m := range matches {
		info, err := os.Stat(m)
		if err == nil && info.IsDir() {
			add(m)
		}
	}
	return out
}

func inspectDoctorStores(fix bool) (issues []string, fixes []string) {
	dirs := doctorStoreDirs()
	seenBusy := map[string]bool{}
	for _, dir := range dirs {
		if holder, busy := cellstore.InspectStoreBusy(dir, ""); busy {
			key := filepath.Clean(holder.Lock)
			if key == "." || key == "" {
				key = dir
			}
			if !seenBusy[key] {
				seenBusy[key] = true
				if who := holder.Describe(); who != "" {
					issues = append(issues, "busy · held by "+who)
				} else {
					issues = append(issues, "busy")
				}
			}
		}
	}
	return issues, fixes
}

type localChatInventory struct {
	foldersByID   map[string][]string
	inUseByID     map[string]bool
	orphanStaging map[string]string
}

func scanLocalChatInventory() localChatInventory {
	inv := localChatInventory{
		foldersByID:   map[string][]string{},
		inUseByID:     map[string]bool{},
		orphanStaging: map[string]string{},
	}
	seenFolder := map[string]bool{}
	recordFolder := func(id, dir string) {
		id = strings.TrimSpace(id)
		dir = filepath.Clean(strings.TrimSpace(dir))
		if id == "" || dir == "" || dir == "." {
			return
		}
		transcript := session.Place{Dir: dir}.Transcript()
		if session.InUse(transcript) || handoff.Arriving(dir) {
			inv.inUseByID[id] = true
		}
		key := id + "\x00" + dir
		if seenFolder[key] {
			return
		}
		seenFolder[key] = true
		inv.foldersByID[id] = append(inv.foldersByID[id], dir)
	}

	for _, row := range session.ReadHome().Sessions() {
		id := strings.TrimSpace(row.ID)
		if id == "" {
			if dir, ok := session.FolderOf(row.Transcript); ok {
				id = filepath.Base(dir)
			}
		}
		dir := session.DirOf(row.Transcript)
		if row.Open {
			inv.inUseByID[id] = true
		}
		recordFolder(id, dir)
	}

	var candidates []string
	for _, pattern := range []string{
		filepath.Join(home.Join("v3", "projects"), "*", "*"),
		filepath.Join(home.Join("v3", "projects"), "*", ".moving", "*"),
		filepath.Join(cell.Dir(home.Dir()), "*"),
	} {
		matches, _ := filepath.Glob(pattern)
		candidates = append(candidates, matches...)
	}
	slices.Sort(candidates)
	for _, entry := range candidates {
		base := filepath.Base(entry)
		if base == ".moving" || strings.HasSuffix(base, ".arriving") || strings.HasSuffix(base, ".lock") {
			continue
		}
		info, err := os.Stat(entry)
		if err != nil || !info.IsDir() {
			continue
		}
		if strings.HasSuffix(base, handoff.StagingSuffix) {
			baseRoot := strings.TrimSuffix(entry, handoff.StagingSuffix)
			id := strings.TrimSpace(filepath.Base(baseRoot))
			if id == "" {
				continue
			}
			if handoff.Arriving(baseRoot) || session.InUse(session.Place{Dir: baseRoot}.Transcript()) {
				inv.inUseByID[id] = true
				continue
			}
			if _, exists := inv.orphanStaging[id]; !exists {
				inv.orphanStaging[id] = filepath.Clean(entry)
			}
			continue
		}
		if strings.HasSuffix(base, ".replaced") {
			continue
		}
		hasTranscript := fileExists(session.Place{Dir: entry}.Transcript())
		hasCell := fileExists(filepath.Join(entry, filepath.FromSlash(cell.MetaPath)))
		if hasTranscript || hasCell {
			id := base
			if meta, err := session.LoadMeta(entry); err == nil && strings.TrimSpace(meta.ID) != "" {
				id = strings.TrimSpace(meta.ID)
			}
			recordFolder(id, entry)
		}
	}
	return inv
}

func inspectDoctorSync(fix bool) (issues []string, fixes []string) {
	inv := scanLocalChatInventory()
	handledStaging := map[string]bool{}

	if syncsetup.MayTalk(home.Dir()) {
		if s, ok, err := syncsetup.Open(home.Dir()); err == nil && ok {
			ctx, cancel := context.WithTimeout(context.Background(), doctorSyncTimeout)
			defer cancel()
			if l, listErr := s.Dir.List(ctx); listErr == nil {
				self := s.Device.ID()
				ids := slices.Sorted(maps.Keys(l.Cells))
				reportedUnfinished := map[string]bool{}
				releasedTake := map[string]bool{}

				// 1. Unfinished continues started by this computer.
				for _, id := range ids {
					rec := l.Cells[id]
					if rec.Archived || inv.inUseByID[id] || handoff.Arriving(takeRootFor(id)) {
						continue
					}
					takingDir := inv.orphanStaging[id]
					heldLiveBySelf := rec.Lease.Device == self && rec.Lease.Expires > l.Now
					heldLiveByOther := rec.Lease.Device != "" && rec.Lease.Device != self && rec.Lease.Expires > l.Now
					if heldLiveByOther {
						// Never touch another computer's live hold.
						continue
					}
					_, hasLocal := openCellByID(id)
					unfinished := false
					if heldLiveBySelf && (takingDir != "" || hasLocal != nil || !sendLeaseMatches(s, id, rec.Lease.Fence)) {
						unfinished = true
					} else if takingDir != "" {
						unfinished = true
					}
					if !unfinished {
						continue
					}
					handledStaging[id] = true

					if fix {
						safeStaging := takingDir == "" || !stagingHasUniqueTurns(takingDir, id, rec.Head)
						released := false
						if heldLiveBySelf {
							relErr := directory.ReleaseQuiet(ctx, s.Dir, id, rec.Lease.Fence)
							if errors.Is(relErr, directory.ErrUnauthorized) {
								relErr = s.Dir.Release(ctx, id, rec.Lease.Fence)
							}
							if relErr == nil {
								released = true
								rec.Lease.Expires, rec.Lease.Pending = 0, 0
								l.Cells[id] = rec
							}
						}
						removedStaging := false
						if (!heldLiveBySelf || released) && takingDir != "" && safeStaging {
							if rmErr := os.RemoveAll(takingDir); rmErr == nil {
								removedStaging = true
								delete(inv.orphanStaging, id)
							}
						}
						if (heldLiveBySelf && released) || (!heldLiveBySelf && removedStaging) {
							fixes = append(fixes, fmt.Sprintf("fixed: released unfinished continue for chat %s", id))
							releasedTake[id] = true
							if takingDir == "" || removedStaging {
								continue
							}
						}
					}
					issues = append(issues, fmt.Sprintf("unfinished continue · chat %s", id))
					reportedUnfinished[id] = true
				}

				// 2. Duplicate Home rows for one chat id: local duplicate folders and sync branch copies.
				localIDs := slices.Sorted(maps.Keys(inv.foldersByID))
				for _, id := range localIDs {
					if len(inv.foldersByID[id]) > 1 {
						issues = append(issues, fmt.Sprintf("duplicate rows · chat %s (%d local folders)", id, len(inv.foldersByID[id])))
					}
				}
				for _, branchID := range ids {
					branchRec := l.Cells[branchID]
					if branchRec.Archived || branchRec.ParentCell == "" || branchRec.OrphanTurns == 0 {
						continue
					}
					mainID := resolveRootCellID(l.Cells, branchRec.ParentCell)
					mainRec, hasMain := l.Cells[mainID]
					allInMain := hasMain && !mainRec.Archived && branchTurnsAllInMain(mainID, mainRec, branchID, branchRec)
					heldLiveByOther := branchRec.Lease.Expires > l.Now && branchRec.Lease.Device != "" && branchRec.Lease.Device != self
					if fix && allInMain && !heldLiveByOther {
						if discErr := s.Discard(ctx, branchID); discErr == nil {
							fixes = append(fixes, fmt.Sprintf("fixed: retired duplicate branch copy %s of chat %s", branchID, mainID))
							branchRec.Archived = true
							l.Cells[branchID] = branchRec
							continue
						}
					}
					if allInMain {
						issues = append(issues, fmt.Sprintf("duplicate rows · chat %s (branch copy %s, all turns in main chat)", mainID, branchID))
					} else {
						turnsWord := fmt.Sprintf("%d turns", branchRec.OrphanTurns)
						if branchRec.OrphanTurns == 1 {
							turnsWord = "1 turn"
						}
						issues = append(issues, fmt.Sprintf("duplicate rows · chat %s (branch copy %s, %s not in main chat)", mainID, branchID, turnsWord))
					}
				}

				// 3. This computer's own lapsed holds that would ask a question on open.
				for _, id := range ids {
					rec := l.Cells[id]
					if rec.Archived || rec.OrphanTurns > 0 || reportedUnfinished[id] || releasedTake[id] || inv.inUseByID[id] {
						continue
					}
					if rec.Lease.Device == self && rec.Lease.Expires > 0 && rec.Lease.Expires <= l.Now {
						if lapsedHoldWouldAskOnOpen(s, id, rec) {
							issues = append(issues, fmt.Sprintf("lapsed hold on this computer · chat %s", id))
						}
					}
				}

				// Any local orphan .taking folder for a chat id absent from the directory listing.
				issues, fixes = finishOrphanStaging(inv.orphanStaging, handledStaging, fix, issues, fixes)
				return issues, fixes
			}
		}
	}

	// Sync is off or unreachable: still report local duplicate Home rows and orphaned .taking folders.
	localIDs := slices.Sorted(maps.Keys(inv.foldersByID))
	for _, id := range localIDs {
		if len(inv.foldersByID[id]) > 1 {
			issues = append(issues, fmt.Sprintf("duplicate rows · chat %s (%d local folders)", id, len(inv.foldersByID[id])))
		}
	}
	issues, fixes = finishOrphanStaging(inv.orphanStaging, handledStaging, fix, issues, fixes)
	return issues, fixes
}

func finishOrphanStaging(orphanStaging map[string]string, handled map[string]bool, fix bool, issues, fixes []string) ([]string, []string) {
	ids := slices.Sorted(maps.Keys(orphanStaging))
	for _, id := range ids {
		if handled[id] {
			continue
		}
		takingDir := orphanStaging[id]
		if fix && !stagingHasUniqueTurns(takingDir, id, "") {
			if err := os.RemoveAll(takingDir); err == nil {
				fixes = append(fixes, fmt.Sprintf("fixed: released unfinished continue for chat %s", id))
				continue
			}
		}
		issues = append(issues, fmt.Sprintf("unfinished continue · chat %s", id))
	}
	return issues, fixes
}

func sendLeaseMatches(s *syncsetup.Sync, id string, fence uint64) bool {
	heldCell, heldFence, err := s.SendLease(id)
	return err == nil && heldCell == id && heldFence == fence && fence != 0
}

func resolveRootCellID(cells map[string]directory.Cell, parent string) string {
	cur := strings.TrimSpace(parent)
	for hops := 0; cur != "" && hops < 16; hops++ {
		rec, ok := cells[cur]
		if !ok || rec.OrphanTurns == 0 || strings.TrimSpace(rec.ParentCell) == "" || rec.ParentCell == cur {
			break
		}
		cur = strings.TrimSpace(rec.ParentCell)
	}
	return cur
}

func branchTurnsAllInMain(mainID string, mainRec directory.Cell, branchID string, branchRec directory.Cell) bool {
	if strings.TrimSpace(branchRec.Head) == "" {
		return false
	}
	if mainRec.Head != "" && branchRec.Head == mainRec.Head {
		return true
	}
	mainLocal, err := openCellByID(mainID)
	if err != nil {
		return false
	}
	if mainRec.Head != "" {
		if mainKnown, err := cellstore.KnowsHead(mainLocal, mainRec.Head); err != nil || !mainKnown {
			return false
		}
	}
	if branchLocal, err := openCellByID(branchID); err == nil {
		branchTurns, err := cellstore.Turns(branchLocal)
		if err != nil || len(branchTurns) == 0 || branchTurns[len(branchTurns)-1].ID != branchRec.Head {
			return false
		}
		for _, turn := range branchTurns {
			if known, err := cellstore.KnowsHead(mainLocal, turn.ID); err != nil || !known {
				return false
			}
		}
		return true
	}
	known, err := cellstore.KnowsHead(mainLocal, branchRec.Head)
	return err == nil && known
}

func lapsedHoldWouldAskOnOpen(s *syncsetup.Sync, id string, rec directory.Cell) bool {
	localCell, err := openCellByID(id)
	if err != nil {
		return true
	}
	if known, err := cellstore.KnowsHead(localCell, rec.Head); err != nil || !known {
		return true
	}
	if extends, err := cellstore.Extends(localCell, rec.Head); err != nil || !extends {
		return true
	}
	if head, err := cellstore.Head(localCell); err != nil || head == nil || head.Turn.ID != rec.Head {
		return true
	}
	if rec.Lease.Pending > 0 {
		return true
	}
	if heldCell, heldFence, err := s.SendLease(id); err == nil && heldFence != 0 && (heldCell != id || heldFence != rec.Lease.Fence) {
		return true
	}
	return false
}

// stagingHasUniqueTurns fails closed whenever a .taking scratch folder holds any
// sealed turn or unsealed transcript content that does not already exist in the
// local main chat or the directory's durable head.
func stagingHasUniqueTurns(takingDir, id, durableHead string) bool {
	entries, err := os.ReadDir(takingDir)
	if err != nil {
		return !os.IsNotExist(err)
	}
	if len(entries) == 0 {
		return false
	}

	mainCell, mainErr := openCellByID(id)
	stagedCell := cell.Cell{ID: id, Root: takingDir}
	hasStagedCell := fileExists(filepath.Join(takingDir, filepath.FromSlash(cell.MetaPath)))
	if hasStagedCell {
		if _, busy := cellstore.InspectStoreBusy(cellstore.SharedStoreDir(), ""); busy {
			return true
		}
		stagedTurns, turnsErr := cellstore.Turns(stagedCell)
		if turnsErr != nil && !os.IsNotExist(turnsErr) {
			return true
		}
		for _, turn := range stagedTurns {
			if durableHead != "" && len(stagedTurns) > 0 && stagedTurns[len(stagedTurns)-1].ID == durableHead {
				break
			}
			if mainErr != nil {
				return true
			}
			if known, err := cellstore.KnowsHead(mainCell, turn.ID); err != nil || !known {
				return true
			}
		}
	}

	stagedTranscript := session.Place{Dir: takingDir}.Transcript()
	stagedRaw, readErr := os.ReadFile(stagedTranscript)
	if readErr != nil {
		return !os.IsNotExist(readErr)
	}
	if len(bytes.TrimSpace(stagedRaw)) == 0 {
		return false
	}
	if hasStagedCell {
		if head, err := cellstore.Head(stagedCell); err == nil && head != nil && int64(len(stagedRaw)) <= head.Receipt.Transcript.End {
			if durableHead != "" && head.Turn.ID == durableHead {
				return false
			}
			if mainErr == nil {
				if known, err := cellstore.KnowsHead(mainCell, head.Turn.ID); err == nil && known {
					return false
				}
			}
		}
	}
	if mainErr != nil {
		return true
	}
	mainRaw, err := os.ReadFile(session.Place{Dir: mainCell.Root}.Transcript())
	if err != nil {
		return true
	}
	return !bytes.HasPrefix(mainRaw, stagedRaw)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
