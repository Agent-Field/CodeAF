package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func validateWorkspaceSetRequest(request *WorkspaceSetRequest) error {
	if len(request.Roots) == 0 || len(request.Roots) > workspaceSetRootLimit {
		return fmt.Errorf("workspace set requires between 1 and %d explicit roots", workspaceSetRootLimit)
	}
	request.Roots = append([]WorkspaceRootRequest(nil), request.Roots...)
	slots := map[string]bool{}
	folded := map[string]bool{}
	for i := range request.Roots {
		root := &request.Roots[i]
		if !validWorkspaceSlot(root.Slot) || folded[strings.ToLower(root.Slot)] {
			return errors.New("workspace slots require distinct portable ASCII directory names, including case-insensitive uniqueness; unsupported layouts are not renamed")
		}
		slots[root.Slot] = true
		folded[strings.ToLower(root.Slot)] = true
		if root.Access != WorkspaceRead && root.Access != WorkspaceWrite {
			return errors.New("workspace root requires explicit read or write access")
		}
		if root.SourceURI != "" {
			source, err := workspaceidentity.ResolveExisting(home.Join("workspace-identities"), root.SourceURI)
			if err != nil {
				return fmt.Errorf("resolve declared workspace source: %w", err)
			}
			if source.Role != workspaceidentity.Project && source.Role != workspaceidentity.Run {
				return errors.New("workspace source identity does not name a root or owned copy")
			}
			root.Source = source.Path
		}
		if root.LandingURI != "" {
			if root.SourceURI == "" || root.LandingURI != root.SourceURI || root.Access != WorkspaceWrite {
				return errors.New("workspace landing identity must name its admitted writable source")
			}
			root.Landing = root.Source
		}
		if !filepath.IsAbs(root.Source) {
			return errors.New("workspace source must be an explicit absolute folder")
		}
		root.Source = filepath.Clean(root.Source)
		if err := workspacePathWithoutSymlinks(root.Source); err != nil {
			return err
		}
		if root.Landing != "" {
			if root.Access != WorkspaceWrite || filepath.Clean(root.Landing) != root.Source {
				return errors.New("workspace landing must explicitly name its writable source folder")
			}
			root.Landing = root.Source
		}
		for j := 0; j < i; j++ {
			other := request.Roots[j].Source
			if withinDir(other, root.Source) || withinDir(root.Source, other) {
				return errors.New("overlapping workspace sources require an explicit ownership layout")
			}
		}
	}
	if !slots[request.Primary] {
		return errors.New("workspace primary must name one declared root slot")
	}
	sort.Slice(request.Roots, func(i, j int) bool { return request.Roots[i].Slot < request.Roots[j].Slot })
	return nil
}

func workspacePathWithoutSymlinks(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("workspace source unavailable: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("workspace source uses a symlink; an explicit supported layout is required")
		}
		if current == path && !info.IsDir() {
			return errors.New("workspace source is not a folder")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}

func prepareWorkspaceSet(ctx context.Context, place Place, id uint64, title string, request WorkspaceSetRequest) (primary taskTree, err error) {
	if !WorkspaceOwnershipEnabled(place) || place.Trees() == "" {
		return taskTree{}, errors.New("workspace sets require an owned conversation tree root")
	}
	if err := validateWorkspaceSetRequest(&request); err != nil {
		return taskTree{}, err
	}
	base := filepath.Join(place.Trees(), strconv.FormatUint(id, 10))
	if _, err := validateWorkspaceSetSources(ctx, place, request, base); err != nil {
		return taskTree{}, err
	}

	// A partially prepared set is preserved for diagnosis; never recarve it.
	if _, err := os.Lstat(base); err == nil {
		if receipt, readErr := readWorkspaceSetPreparation(place, id); readErr == nil {
			return taskTree{workspaceSet: receipt.Set}, errors.New("workspace preparation already has retained copies; its incomplete record requires review before retry")
		}
		return taskTree{}, errors.New("workspace set already exists; its retained preparation needs review")
	} else if !os.IsNotExist(err) {
		return taskTree{}, err
	}
	set := &WorkspaceSet{Version: 1, Primary: request.Primary}
	for _, use := range request.Roots {
		set.Roots = append(set.Roots, WorkspaceRootUse{SourceURI: use.SourceURI, Slot: use.Slot, Access: use.Access, Landing: use.LandingURI})
	}
	defer func() {
		primary.workspaceSet = set
		if receiptErr := writeWorkspaceSetPreparation(place, id, title, request, set, err == nil); receiptErr != nil {
			err = errors.Join(err, fmt.Errorf("retain workspace preparation receipt: %w", receiptErr))
		}
	}()
	if err := writeWorkspaceSetPreparation(place, id, title, request, set, false); err != nil {
		return primary, err
	}
	for i, use := range request.Roots {
		if err := ctx.Err(); err != nil {
			return taskTree{}, err
		}
		dir := filepath.Join(base, use.Slot)
		order := groundOrder{place: place, ground: use.Source, dir: dir, mode: 0700, title: title, promise: TaskModeMirror}
		if root, ok := repositoryRoot(use.Source); ok && canonicalPath(root) == canonicalPath(use.Source) && hasCommit(root) {
			order = worktreeOrder(place, root, dir, 0700, taskBranchName(title+"-"+use.Slot), title, "")
		}
		inputBaseline, err := taskcopy.InspectPortableState(use.Source)
		if order.promise == TaskModeMirror {
			inputBaseline, err = taskcopy.InspectPortableFamilyInput(use.Source)
		}
		if err != nil {
			return primary, fmt.Errorf("inspect workspace root %s source metadata: %w", use.Slot, err)
		}
		tree, err := carveGround(ctx, order)
		if err != nil {
			return taskTree{}, fmt.Errorf("prepare workspace root %s: %w", use.Slot, err)
		}
		if order.promise == TaskModeMirror {
			rememberGroundBaseline(tree.dir)
			tree, err = openPortableWorkspaceFamilyTree(tree, inputBaseline)
			if err != nil {
				return primary, preserveFailedWorkspaceCreation(tree, err)
			}
			// The native mirror receives its repository only after the carve. Record
			// its immutable admission commit before any worker can change this copy.
			baseline, baseErr := git(tree.dir, "rev-parse", "--verify", "HEAD")
			if baseErr != nil {
				return taskTree{}, fmt.Errorf("record workspace root %s baseline before starting work: %w", use.Slot, baseErr)
			}
			tree.checkBase = strings.TrimSpace(baseline)
		}
		tree, err = registerOwnedWorkspace(tree, place)
		if err != nil {
			return taskTree{}, err
		}
		tree, err = finishWorkspaceRootInput(use.Source, tree, inputBaseline)
		if err != nil {
			return primary, preserveFailedWorkspaceCreation(tree, err)
		}
		tree.bashBelt = true
		landingURI := ""
		if use.Landing != "" {
			landingURI = tree.groundURI
		}
		set.Roots[i] = WorkspaceRootUse{SourceURI: use.SourceURI, Slot: use.Slot, Access: use.Access, Landing: landingURI, Copy: runCopyOf(tree)}
		if err := writeWorkspaceSetPreparation(place, id, title, request, set, false); err != nil {
			return primary, err
		}
		if use.Slot == request.Primary {
			primary = tree
		}
	}
	primary.workspaceSet = set
	return primary, nil
}

func (a *Agent) landWorkspaceSet(ctx context.Context, engine RunEngine, run *beltRun) RunLanding {
	result := RunLanding{Home: mergeKept}
	if !a.beginBeltHomecoming(run) {
		return result
	}
	set := run.tree.workspaceSet
	for i := range set.Roots {
		use := &set.Roots[i]
		tree, err := runCopyTree(use.Copy, a.config.Place)
		if err != nil {
			use.Outcome = mergeAborted
			result.Refused += use.Slot + ": " + err.Error() + "; "
			continue
		}
		receipt, err := engine.Land(ctx, run.store, tree.dir, tree.checkBase, run.root)
		if err != nil {
			use.Outcome = mergeAborted
			result.Refused += use.Slot + ": " + err.Error() + "; "
			continue
		}
		if receipt.Refused == runNothingToLand && len(receipt.Changed) == 0 {
			use.Outcome = mergeKept
			_, _ = run.store.AddNote(run.root, run.root, "workspace "+use.Slot+": unchanged, retained privately")
			continue
		}
		if receipt.Refused != "" {
			use.Outcome = mergeAborted
			result.Refused += use.Slot + ": " + receipt.Refused + "; "
			_, _ = run.store.AddNote(run.root, run.root, "workspace "+use.Slot+": kept privately; "+receipt.Refused)
			continue
		}
		use.Outcome = mergeKept
		if use.Access == WorkspaceWrite && use.Landing != "" {
			merged, said, _, _ := tree.comeHome(run.title, receipt.Changed, a.signsGitWork())
			use.Outcome = merged
			if merged != mergeMerged && merged != mergeInPlace {
				result.Refused += use.Slot + ": " + said + "; "
			}
		}
		_, _ = run.store.AddNote(run.root, run.root, "workspace "+use.Slot+": "+use.Outcome)
		if use.Slot == set.Primary {
			result.Branch = receipt.Branch
			result.Changed = receipt.Changed
		}
	}
	if result.Refused != "" {
		result.Home = mergeAborted
	}
	if g := a.graph(); g != nil {
		if notice, ok := runRowOf(g, run.row); ok {
			notice.WorkspaceSet = cloneWorkspaceSet(set)
			a.publishRunRow(g, notice)
		}
	}
	return result
}

// Stopping never lands unfinished work. Keep each member locally, including
// native forks: the singleton keptWork road may fetch into its source repo.
func (a *Agent) keepStoppedWorkspaceSet(run *beltRun) (merge, branch string, changed []string, report string) {
	merge = mergeKept
	for i := range run.tree.workspaceSet.Roots {
		use := &run.tree.workspaceSet.Roots[i]
		tree, err := runCopyTree(use.Copy, a.config.Place)
		problem := ""
		var rootChanged []string
		if err != nil {
			problem = err.Error()
		} else {
			if tree.checkBase != "" {
				rootChanged, _, err = runCommittedPaths(tree.dir, tree.checkBase)
				if err != nil {
					problem = err.Error()
				}
			}
			saved, saveProblem, _ := commitTaskWork(tree.dir, run.title, rootChanged, a.signsGitWork(), true)
			rootChanged = alsoChanged(rootChanged, saved)
			if saveProblem != "" {
				problem = withReport(problem, saveProblem)
			}
		}
		use.Outcome = mergeKept
		if problem != "" {
			use.Outcome = mergeAborted
			merge = mergeAborted
		}
		note := "workspace " + use.Slot + ": " + use.Outcome + " in its private copy"
		if problem != "" {
			note = withReport(note, problem)
		}
		_, _ = run.store.AddNote(run.root, run.root, note)
		report += " · " + note
		if use.Slot == run.tree.workspaceSet.Primary {
			changed = rootChanged
			if len(rootChanged) > 0 {
				branch = currentBranch(tree.dir)
			}
		}
	}
	return
}

// Keep declared spelling while refusing layouts that alias on other supported
// filesystems. Unicode normalization and hidden metadata-root layouts need a
// separately tested mapping; they are never silently renamed.
func validWorkspaceSlot(slot string) bool { return taskcopy.ValidWorkspaceSlot(slot) }

func validateWorkspaceSetSources(ctx context.Context, place Place, request WorkspaceSetRequest, base string) (*workspaceidentity.Registry, error) {
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return nil, err
	}
	chat, chatErr := registry.LookupRecorded(workspaceidentity.Chat, canonicalPath(place.Dir))
	if chatErr != nil && !errors.Is(chatErr, workspaceidentity.ErrUnknown) {
		return nil, chatErr
	}
	// Validate every source before carving even the primary: a secondary may
	// still be owned by an external program, or contain the destination itself.
	for _, use := range request.Roots {
		for path := use.Source; ; path = filepath.Dir(path) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			owned, err := registry.LookupRecorded(workspaceidentity.Run, path)
			if err == nil {
				if chatErr != nil || owned.OwnerURI != chat.URI || path != use.Source || !ownedTreePath(path, place) {
					return nil, errors.New("workspace source belongs to another execution; cooperate in its admitted workspace instead")
				}
				break
			}
			if !errors.Is(err, workspaceidentity.ErrUnknown) {
				return nil, err
			}
			if filepath.Dir(path) == path {
				break
			}
		}
		if refusal := programHoldRefusal(use.Source); refusal != "" {
			return nil, errors.New(refusal)
		}
		if withinDir(use.Source, base) || withinDir(base, use.Source) {
			return nil, errors.New("workspace source overlaps its owned destination")
		}
		if ownershipPlainMode(use.Source) == TaskModeReference {
			return nil, errors.New("a personal home cannot be copied as a workspace root; select an explicit working folder")
		}
	}

	return registry, nil
}

// Admit roots while work is queued, without carving any execution copies.
// Sealed checkpoints can then omit machine-local paths and still reopen every
// selected root through its authenticated local identity.
func admitWorkspaceSetRequest(ctx context.Context, place Place, request *WorkspaceSetRequest) error {
	if place.Trees() == "" {
		return errors.New("workspace sets require an owned conversation tree root")
	}
	if err := validateWorkspaceSetRequest(request); err != nil {
		return err
	}
	registry, err := validateWorkspaceSetSources(ctx, place, *request, place.Trees())
	if err != nil {
		return err
	}
	for i := range request.Roots {
		root := &request.Roots[i]
		if root.SourceURI == "" {
			record, err := registry.LookupRecorded(workspaceidentity.Run, root.Source)
			if errors.Is(err, workspaceidentity.ErrUnknown) {
				record, err = registry.Register(workspaceidentity.Project, root.Source)
			}
			if err != nil {
				return err
			}
			root.SourceURI = record.URI
		}
		if root.Landing != "" {
			root.LandingURI = root.SourceURI
		}
	}
	return nil
}

const workspaceSetPreparationFile = ".workspace-set.json"

type workspaceSetPreparation struct {
	Version  int                 `json:"version"`
	Run      uint64              `json:"run"`
	ChatURI  string              `json:"chat_uri"`
	Title    string              `json:"title"`
	Request  WorkspaceSetRequest `json:"request"`
	Set      *WorkspaceSet       `json:"set"`
	Complete bool                `json:"complete"`
}

func writeWorkspaceSetPreparation(place Place, id uint64, title string, request WorkspaceSetRequest, set *WorkspaceSet, complete bool) error {
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return err
	}
	chat, err := registry.Register(workspaceidentity.Chat, canonicalPath(place.Dir))
	if err != nil {
		return err
	}
	request.Roots = append([]WorkspaceRootRequest(nil), request.Roots...)
	for i := range request.Roots {
		request.Roots[i].Source = ""
		request.Roots[i].Landing = ""
	}
	row := runRecord{WorkspaceSet: set}
	codec := codecFor(place.Dir, place.Workspace)
	for _, field := range row.pathFields() {
		if *field != "" {
			*field, _ = codec.encode(*field)
		}
	}
	receipt := workspaceSetPreparation{Version: 1, Run: id, ChatURI: chat.URI, Title: title, Request: request, Set: row.WorkspaceSet, Complete: complete}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	base := filepath.Join(place.Trees(), strconv.FormatUint(id, 10))
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(base, ".workspace-set-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(base, workspaceSetPreparationFile)); err != nil {
		return err
	}
	directory, err := os.Open(base)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func readWorkspaceSetPreparation(place Place, id uint64) (workspaceSetPreparation, error) {
	var receipt workspaceSetPreparation
	base := filepath.Join(place.Trees(), strconv.FormatUint(id, 10))
	path := filepath.Join(base, workspaceSetPreparationFile)
	info, err := os.Lstat(path)
	if err != nil {
		return receipt, err
	}
	if !info.Mode().IsRegular() || info.Size() > taskcopy.WorkspaceCheckpointLimit {
		return receipt, errors.New("invalid workspace preparation receipt")
	}
	file, err := os.Open(path)
	if err != nil {
		return receipt, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, taskcopy.WorkspaceCheckpointLimit+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return receipt, errors.New("invalid workspace preparation receipt")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return receipt, errors.New("invalid workspace preparation receipt trailer")
	}
	if receipt.Version != 1 || receipt.Run != id || receipt.Set == nil || receipt.Set.Version != 1 || len(receipt.Set.Roots) == 0 || len(receipt.Set.Roots) > workspaceSetRootLimit {
		return receipt, errors.New("unsupported workspace preparation receipt")
	}
	chat, err := workspaceidentity.ResolveExisting(home.Join("workspace-identities"), receipt.ChatURI)
	if err != nil || chat.Role != workspaceidentity.Chat || chat.Path != canonicalPath(place.Dir) {
		return receipt, errors.New("workspace preparation belongs to another conversation")
	}
	row := runRecord{WorkspaceSet: receipt.Set}
	codec := codecFor(place.Dir, place.Workspace)
	for _, field := range row.pathFields() {
		if *field != "" {
			*field, _ = codec.resolve(*field)
		}
	}
	receipt.Set = row.WorkspaceSet
	for _, use := range receipt.Set.Roots {
		if !validWorkspaceSlot(use.Slot) {
			return receipt, errors.New("invalid workspace preparation slot")
		}
		if use.Copy == nil {
			continue
		}
		resolved, err := resolveOwnedWorkspace(use.Copy, place)
		if err != nil || filepath.Dir(resolved) != base || filepath.Base(resolved) != use.Slot {
			return receipt, errors.New("workspace preparation copy ownership does not match its layout")
		}
	}
	return receipt, nil
}

func recoverWorkspaceSetPreparations(place Place, document taskDocument) (taskDocument, bool) {
	if !WorkspaceOwnershipEnabled(place) || place.Trees() == "" {
		return document, false
	}
	directory, err := os.Open(place.Trees())
	if err != nil {
		return document, false
	}
	defer directory.Close()
	known := map[uint64]bool{}
	for _, row := range document.Runs {
		if row.PendingRun == nil {
			known[row.ID] = true
		}
	}
	for _, node := range document.Nodes {
		known[node.ID] = true
	}
	changed := false
	for {
		entries, readErr := directory.ReadDir(128)
		for _, entry := range entries {
			id, err := strconv.ParseUint(entry.Name(), 10, 64)
			if err != nil || id == 0 || !entry.IsDir() || known[id] {
				continue
			}
			receipt, err := readWorkspaceSetPreparation(place, id)
			if err != nil {
				continue
			}
			record := runRecord{ID: id, Title: receipt.Title, State: TaskFailed, WorkspaceSet: receipt.Set, Report: "workspace preparation was interrupted; its private copies and ownership receipt are retained for review"}
			for _, use := range receipt.Set.Roots {
				if use.Slot == receipt.Set.Primary {
					record.Copy = use.Copy
				}
			}
			replaced := false
			for i := range document.Runs {
				if document.Runs[i].ID == id {
					record.CrewState = document.Runs[i].CrewState
					record = keepPendingRemotePreparation(receipt, record, document.Runs[i])
					document.Runs[i] = record
					replaced = true
					break
				}
			}
			if !replaced {
				document.Runs = append(document.Runs, record)
			}
			document.Seq = max(document.Seq, id)
			changed = true
		}
		if readErr != nil {
			break
		}
	}
	if changed {
		document.Type = taskDocumentType
		document.Version = taskWorkspaceSetFileVersion
		document = filterDeletedDocument(document, taskDeletions(place.Transcript()))
	}
	return document, changed
}

// A completed preparation receipt belongs to its saved remote intent, not a new local run.
func keepPendingRemotePreparation(receipt workspaceSetPreparation, recovered, saved runRecord) runRecord {
	if receipt.Complete && saved.Remote != nil && !saved.Remote.Abandoned && saved.PendingRun != nil {
		saved.Copy, saved.WorkspaceSet = recovered.Copy, recovered.WorkspaceSet
		return saved
	}
	return recovered
}
