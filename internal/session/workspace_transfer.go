package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// TaskWorkspaceTransferPlan validates a complete incoming checkpoint and its
// carrier without registering identities, preparing files or changing sources.
func TaskWorkspaceTransferPlan(place Place) (*taskcopy.WorkspacePlan, error) {
	_, plan, err := readTaskWorkspaceTransfer(place)
	if err != nil || plan == nil {
		return plan, err
	}
	if err := taskcopy.ValidateWorkspace(cell.Cell{Root: place.Dir}, *plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func readTaskWorkspaceTransfer(place Place) (taskDocument, *taskcopy.WorkspacePlan, error) {
	raw, err := readWorkspaceCheckpointBytes(place.Tasks())
	if errors.Is(err, os.ErrNotExist) {
		return taskDocument{}, nil, nil
	}
	if err != nil {
		return taskDocument{}, nil, err
	}
	if len(raw) > taskcopy.WorkspaceCheckpointLimit {
		return taskDocument{}, nil, errors.New("workspace checkpoint exceeds transfer bound")
	}
	document, err := decodeTasks(raw)
	if err != nil {
		return taskDocument{}, nil, err
	}
	plan, digest, err := taskcopy.ReadWorkspacePlan(cell.Cell{Root: place.Dir})
	if err != nil {
		return taskDocument{}, nil, err
	}
	if plan == nil {
		return document, nil, nil
	}
	sum := sha256.Sum256(raw)
	if digest != hex.EncodeToString(sum[:]) {
		return taskDocument{}, nil, errors.New("workspace checkpoint changed during transfer validation")
	}
	resolved := document.mapPaths(codecFor(place.Dir, place.Workspace).resolve)
	expected, err := workspaceTransferProjectionFor(resolved, place, plan.ChatURI)
	if err != nil {
		return taskDocument{}, nil, err
	}
	if !reflect.DeepEqual(expected, plan) {
		return taskDocument{}, nil, errors.New("workspace transfer projection disagrees with authoritative task records")
	}
	return resolved, plan, nil
}

// ValidateTaskWorkspaceSetBindings checks every root before any receiver file or
// identity changes. A supplied path is explicit receiver authority; source paths
// from the portable checkpoint never supply a binding.
func ValidateTaskWorkspaceSetBindings(place Place, bindings map[string]string) error {
	return ValidateTaskWorkspaceSetBindingsForDestination(place, place.Dir, bindings)
}

// ValidateTaskWorkspaceSetBindingsForDestination validates a staged checkpoint
// against the private storage it will replace before takeover acquires a lease.
func ValidateTaskWorkspaceSetBindingsForDestination(place Place, destination string, bindings map[string]string) error {
	plan, err := TaskWorkspaceTransferPlan(place)
	if err != nil || plan == nil {
		return err
	}
	_, err = workspaceSetTransferTargets(place, *plan, bindings, destination)
	return err
}

// WorkspaceRootBinding is a source identity requiring the person's local folder
// choice. Label describes its declared layout, never a suggested local path.
type WorkspaceRootBinding struct {
	URI   string
	Label string
}

// WorkspaceBindingsRequired leaves the incoming layout unadmitted until the
// person selects every missing source. Mapping grants no landing permission.
type WorkspaceBindingsRequired struct{ Roots []WorkspaceRootBinding }

func (e *WorkspaceBindingsRequired) Error() string {
	return "choose local folders for this chat's working roots"
}

func workspaceSetTransferTargets(place Place, plan taskcopy.WorkspacePlan, bindings map[string]string, destination string) (map[string]string, error) {
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		return nil, err
	}
	if meta.ChatURI != "" && meta.ChatURI != plan.ChatURI {
		return nil, errors.New("incoming workspace chat identity conflicts with receiver metadata")
	}
	type resolvedIdentity struct {
		record workspaceidentity.Record
		err    error
	}
	knownURIs := map[string]resolvedIdentity{}
	resolve := func(uri string) (workspaceidentity.Record, error) {
		if cached, ok := knownURIs[uri]; ok {
			return cached.record, cached.err
		}
		record, err := workspaceidentity.ResolveExisting(home.Join("workspace-identities"), uri)
		knownURIs[uri] = resolvedIdentity{record, err}
		return record, err
	}
	chat, chatErr := resolve(plan.ChatURI)
	if chatErr == nil && chat.Role != workspaceidentity.Chat {
		return nil, errors.New("incoming chat identity has an incompatible local role")
	}
	if chatErr != nil && !errors.Is(chatErr, workspaceidentity.ErrUnknown) {
		return nil, chatErr
	}
	targets := map[string]string{}
	copyURIs := map[string]bool{}
	external := map[string]bool{}
	for _, group := range plan.Groups {
		if group.Required && group.Incomplete {
			return nil, errors.New("required workspace set is incomplete and cannot resume on another machine")
		}
		for _, entry := range group.Entries {
			if entry.CopyURI != "" {
				copyURIs[entry.CopyURI] = true
				targets[entry.CopyURI] = filepath.Join(place.Trees(), filepath.FromSlash(entry.Layout))
			}
		}
	}
	for _, group := range plan.Groups {
		for _, entry := range group.Entries {
			if entry.CopyURI == "" {
				continue
			}
			if copyURIs[entry.ProjectURI] {
				return nil, errors.New("workspace project link names a run copy")
			}
			existing, err := resolve(entry.CopyURI)
			if err == nil && (existing.Role != workspaceidentity.Run || existing.OwnerURI != plan.ChatURI || existing.ProjectURI != entry.ProjectURI) {
				return nil, errors.New("incoming copy identity conflicts with existing role or ownership")
			}
			if err != nil && !errors.Is(err, workspaceidentity.ErrUnknown) {
				return nil, err
			}
		}
	}
	for _, group := range plan.Groups {
		for _, entry := range group.Entries {
			for _, uri := range []string{entry.ProjectURI, entry.RootURI, entry.GroundURI} {
				if uri != "" && !copyURIs[uri] {
					external[uri] = true
				}
			}
		}
	}
	for uri := range bindings {
		if !external[uri] {
			return nil, errors.New("workspace binding does not name an incoming external root")
		}
	}
	missing := &WorkspaceBindingsRequired{}
	for uri := range external {
		path := bindings[uri]
		known, lookupErr := resolve(uri)
		if lookupErr == nil {
			if known.Role != workspaceidentity.Project {
				return nil, errors.New("workspace root binding has an incompatible role")
			}
			if path != "" && canonicalPath(path) != canonicalPath(known.Path) {
				return nil, errors.New("workspace root already has a different admitted local binding")
			}
			path = known.Path
		} else if !errors.Is(lookupErr, workspaceidentity.ErrUnknown) {
			return nil, lookupErr
		}
		if path == "" {
			label := uri
			for _, group := range plan.Groups {
				for _, entry := range group.Entries {
					if entry.ProjectURI == uri || entry.RootURI == uri || entry.GroundURI == uri {
						label = entry.Layout
						break
					}
				}
			}
			missing.Roots = append(missing.Roots, WorkspaceRootBinding{URI: uri, Label: label})
			continue
		}
		if !filepath.IsAbs(path) || !noSymlinkAncestors(path) {
			return nil, errors.New("workspace binding must be an existing absolute folder without symlink ancestors")
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return nil, errors.New("workspace root binding is unavailable")
		}
		path = canonicalPath(path)
		if withinDir(place.Dir, path) || withinDir(path, place.Dir) || withinDir(destination, path) || withinDir(path, destination) {
			return nil, errors.New("workspace binding overlaps private receiver storage")
		}
		if refusal := programHoldRefusal(path); refusal != "" {
			return nil, errors.New(refusal)
		}
		targets[uri] = path
	}
	if len(missing.Roots) > 0 {
		sort.Slice(missing.Roots, func(i, j int) bool {
			if missing.Roots[i].Label == missing.Roots[j].Label {
				return missing.Roots[i].URI < missing.Roots[j].URI
			}
			return missing.Roots[i].Label < missing.Roots[j].Label
		})
		return nil, missing
	}
	return targets, nil
}

// RestoreTaskWorkspaceSets keeps the native whole-layout receipt fenced until
// all root identities are admitted. Landing permissions never travel with data.
func RestoreTaskWorkspaceSets(ctx context.Context, place Place, bindings map[string]string) ([]string, error) {
	release, err := taskcopy.LockWorkspaceCheckpoint(ctx, cell.Cell{Root: place.Dir})
	if err != nil {
		return nil, err
	}
	defer release()
	if recovered, present, err := recoverWorkspaceFinalization(place); present || err != nil {
		return recovered, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	document, plan, err := readTaskWorkspaceTransfer(place)
	if err != nil || plan == nil {
		return nil, err
	}
	if err := taskcopy.ValidateWorkspace(cell.Cell{Root: place.Dir}, *plan); err != nil {
		if readyErr := validateAdmittedWorkspaceTransfer(place); readyErr != nil {
			return nil, err
		}
		if _, bindingErr := workspaceSetTransferTargets(place, *plan, bindings, place.Dir); bindingErr != nil {
			return nil, bindingErr
		}
		seen := map[string]bool{}
		var layouts []string
		for _, group := range plan.Groups {
			for _, entry := range group.Entries {
				if entry.CopyURI != "" && !seen[entry.Layout] {
					seen[entry.Layout] = true
					layouts = append(layouts, entry.Layout)
				}
			}
		}
		sort.Strings(layouts)
		return layouts, nil
	}
	targets, err := workspaceSetTransferTargets(place, *plan, bindings, place.Dir)
	if err != nil {
		return nil, err
	}
	restore, err := (taskcopy.Carry{}).PrepareWorkspace(cell.Cell{Root: place.Dir}, *plan)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	staged, err := restore.Promote()
	if err != nil {
		return nil, err
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return nil, err
	}
	if _, err = registry.Adopt(workspaceidentity.Record{URI: plan.ChatURI, Role: workspaceidentity.Chat}, canonicalPath(place.Dir), workspaceidentity.Links{}); err != nil {
		return nil, err
	}
	copies := map[string]taskcopy.WorkspaceEntry{}
	for _, binding := range staged {
		copies[binding.Entry.CopyURI] = binding.Entry
		targets[binding.Entry.CopyURI] = binding.Path
	}
	external := map[string]bool{}
	for _, group := range plan.Groups {
		for _, entry := range group.Entries {
			for _, uri := range []string{entry.ProjectURI, entry.RootURI, entry.GroundURI} {
				if uri != "" {
					if _, copy := copies[uri]; !copy {
						external[uri] = true
					}
				}
			}
		}
	}
	for uri := range external {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err = registry.Adopt(workspaceidentity.Record{URI: uri, Role: workspaceidentity.Project}, targets[uri], workspaceidentity.Links{}); err != nil {
			return nil, err
		}
	}
	admitted := map[string]bool{}
	for uri := range external {
		admitted[uri] = true
	}
	for len(admitted) < len(external)+len(copies) {
		progress := false
		for uri, entry := range copies {
			if admitted[uri] || !admitted[entry.ProjectURI] || !admitted[entry.RootURI] || !admitted[entry.GroundURI] {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if _, err = registry.Adopt(workspaceidentity.Record{URI: uri, Role: workspaceidentity.Run, OwnerURI: plan.ChatURI, ProjectURI: entry.ProjectURI}, targets[uri], workspaceidentity.Links{OwnerURI: plan.ChatURI, ProjectURI: entry.ProjectURI}); err != nil {
				return nil, err
			}
			admitted[uri] = true
			progress = true
		}
		if !progress {
			return nil, errors.New("workspace parent identity closure cannot be admitted")
		}
	}
	for i := range document.Runs {
		row := &document.Runs[i]
		if row.WorkspaceSet == nil {
			continue
		}
		for j := range row.WorkspaceSet.Roots {
			use := &row.WorkspaceSet.Roots[j]
			use.Landing = ""
			copy := use.Copy
			if copy == nil {
				continue
			}
			entry, ok := copies[copy.URI]
			if !ok {
				return nil, errors.New("workspace copy was omitted from restored closure")
			}
			copy.Dir, copy.Ground = targets[copy.URI], targets[entry.GroundURI]
			if copy.Root != "" {
				copy.Root = targets[entry.RootURI]
			}
			if use.Slot == row.WorkspaceSet.Primary {
				primary := *copy
				row.Copy = &primary
			}
		}
	}
	encoded, err := encodeTaskCheckpoint(document, place.Tasks())
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	original, err := readWorkspaceCheckpointBytes(place.Tasks())
	if err != nil {
		return nil, err
	}
	var layouts []string
	for _, entry := range copies {
		layouts = append(layouts, entry.Layout)
	}
	sort.Strings(layouts)
	receipt := workspaceFinalization{Version: 1, ChatURI: plan.ChatURI, IncomingSHA: workspaceBytesSHA(original), ExpectedSHA: workspaceBytesSHA(encoded), Checkpoint: encoded, Layouts: layouts}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	if err := writeWorkspaceDurable(workspaceFinalizationPath(place), raw, 0600); err != nil {
		return nil, err
	}
	if err = restore.Acknowledge(); err != nil {
		return nil, err
	}
	if err = finishWorkspaceFinalization(place, receipt); err != nil {
		return nil, err
	}
	return layouts, nil
}

func noSymlinkAncestors(path string) bool {
	for {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		next := filepath.Dir(path)
		if next == path {
			return true
		}
		path = next
	}
}

// TaskWorkspaceForkResults selects only settled result copies. It does not
// duplicate active work, task stores or authority into an independent chat.
func TaskWorkspaceForkResults(place Place) ([]taskcopy.WorkspaceResultSelection, error) {
	document, plan, err := readTaskWorkspaceTransfer(place)
	if err != nil || plan == nil {
		return nil, err
	}
	if err := taskcopy.ValidateWorkspace(cell.Cell{Root: place.Dir}, *plan); err != nil {
		return nil, err
	}
	selected := map[string]string{}
	for _, row := range document.Runs {
		if !row.State.settled() || row.WorkspaceSet == nil {
			continue
		}
		for _, use := range row.WorkspaceSet.Roots {
			if use.Copy == nil {
				continue
			}
			copy := use.Copy
			if prior, ok := selected[copy.URI]; ok && prior != copy.CheckBase {
				return nil, errors.New("settled workspace copy has conflicting result baselines")
			}
			selected[copy.URI] = copy.CheckBase
		}
	}
	var result []taskcopy.WorkspaceResultSelection
	for uri, base := range selected {
		result = append(result, taskcopy.WorkspaceResultSelection{CopyURI: uri, BaseCommit: base})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CopyURI < result[j].CopyURI })
	return result, nil
}

func readWorkspaceCheckpointBytes(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > taskcopy.WorkspaceCheckpointLimit {
		return nil, errors.New("workspace checkpoint is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errors.New("workspace checkpoint changed before opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, taskcopy.WorkspaceCheckpointLimit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > taskcopy.WorkspaceCheckpointLimit {
		return nil, errors.New("workspace checkpoint exceeds transfer bound")
	}
	return raw, nil
}

func validateAdmittedWorkspaceTransfer(place Place) error {
	if err := workspaceFinalizationPending(place); err != nil {
		return err
	}
	_, plan, err := readTaskWorkspaceTransfer(place)
	if err != nil {
		return err
	}
	if plan == nil {
		return errors.New("workspace transfer projection is missing")
	}
	if err := taskcopy.WorkspaceReady(cell.Cell{Root: place.Dir}); err != nil {
		return err
	}
	if _, err := workspaceSetTransferTargets(place, *plan, nil, place.Dir); err != nil {
		return err
	}
	for _, group := range plan.Groups {
		for _, entry := range group.Entries {
			if entry.CopyURI == "" {
				continue
			}
			actual, err := workspaceidentity.ResolveExisting(home.Join("workspace-identities"), entry.CopyURI)
			if err != nil {
				return err
			}
			expected := canonicalPath(filepath.Join(place.Trees(), filepath.FromSlash(entry.Layout)))
			if actual.Role != workspaceidentity.Run || actual.OwnerURI != plan.ChatURI || actual.ProjectURI != entry.ProjectURI || canonicalPath(actual.Path) != expected {
				return errors.New("workspace copy has not been admitted at its declared receiver layout")
			}
		}
	}
	return nil
}

// A finalization receipt is private receiver machinery, never part of the
// portable checkpoint. Its exact hashes fence interruption across acknowledgement.
type workspaceFinalization struct {
	ChatURI     string   `json:"chat_uri"`
	Version     int      `json:"version"`
	IncomingSHA string   `json:"incoming_sha256"`
	ExpectedSHA string   `json:"expected_sha256"`
	Checkpoint  []byte   `json:"checkpoint"`
	Layouts     []string `json:"layouts"`
}

func workspaceFinalizationPath(place Place) string {
	return filepath.Join(place.Dir, ".codeaf", "workspace", "session-finalization.json")
}
func workspaceBytesSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func workspaceFinalizationPending(place Place) error {
	if !WorkspaceOwnershipEnabled(place) {
		return nil
	}
	_, err := os.Lstat(workspaceFinalizationPath(place))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("workspace transfer finalization is incomplete; retain its private recovery receipt and retry the transfer before starting work")
}
func recoverWorkspaceFinalization(place Place) ([]string, bool, error) {
	path := workspaceFinalizationPath(place)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	if !info.Mode().IsRegular() || info.Size() > 24<<20 {
		return nil, true, errors.New("workspace finalization receipt is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, true, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, (24<<20)+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, true, readErr
	}
	if closeErr != nil {
		return nil, true, closeErr
	}
	if len(raw) > 24<<20 {
		return nil, true, errors.New("workspace finalization receipt exceeds its bound")
	}
	var receipt workspaceFinalization
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return nil, true, err
	}
	if receipt.Version != 1 || len(receipt.IncomingSHA) != 64 || receipt.ExpectedSHA != workspaceBytesSHA(receipt.Checkpoint) || len(receipt.Checkpoint) > taskcopy.WorkspaceCheckpointLimit {
		return nil, true, errors.New("workspace finalization receipt checksum is invalid")
	}
	intended, err := decodeTasks(receipt.Checkpoint)
	if err != nil {
		return nil, true, err
	}
	if intended.WorkspaceTransfer == nil || intended.WorkspaceTransfer.ChatURI != receipt.ChatURI {
		return nil, true, errors.New("workspace finalization chat identity does not match its checkpoint")
	}
	current, err := readWorkspaceCheckpointBytes(place.Tasks())
	if err != nil {
		return nil, true, err
	}
	digest := workspaceBytesSHA(current)
	if digest != receipt.IncomingSHA && digest != receipt.ExpectedSHA {
		return nil, true, errors.New("workspace checkpoint changed outside its retained transfer receipt; preserved for review")
	}
	if digest == receipt.IncomingSHA {
		_, plan, err := readTaskWorkspaceTransfer(place)
		if err != nil || plan == nil {
			return nil, true, errors.New("retained workspace transfer no longer has its original checkpoint")
		}
		restore, err := (taskcopy.Carry{}).PrepareWorkspace(cell.Cell{Root: place.Dir}, *plan)
		if err != nil {
			return nil, true, err
		}
		if _, err = restore.Promote(); err != nil {
			return nil, true, err
		}
		if err = restore.Acknowledge(); err != nil {
			return nil, true, err
		}
	} else if err := taskcopy.WorkspaceReady(cell.Cell{Root: place.Dir}); err != nil {
		return nil, true, err
	}
	if err := finishWorkspaceFinalization(place, receipt); err != nil {
		return nil, true, err
	}
	return receipt.Layouts, true, nil
}
func finishWorkspaceFinalization(place Place, receipt workspaceFinalization) error {
	if err := writeWorkspaceDurable(place.Tasks(), receipt.Checkpoint, 0600); err != nil {
		return err
	}
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		return err
	}
	if meta.ChatURI != "" && meta.ChatURI != receipt.ChatURI {
		return errors.New("workspace finalization cannot replace an existing conversation identity")
	}
	meta.ChatURI = receipt.ChatURI
	if meta.ID == "" {
		meta.ID = place.ID()
	}
	if meta.Workspace == "" {
		meta.Workspace = place.Workspace
	}
	if err := SaveMeta(place.Dir, meta); err != nil {
		return err
	}
	for _, path := range []string{filepath.Join(place.Dir, placeMeta), layoutOf(place.Dir).metaTruth(place.Dir)} {
		if path == "" {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		syncErr := file.Sync()
		closeErr := file.Close()
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := syncWorkspaceDirectory(place.Dir); err != nil {
		return err
	}
	if err := syncWorkspaceDirectory(filepath.Dir(place.Tasks())); err != nil {
		return err
	}
	if err := os.Remove(workspaceFinalizationPath(place)); err != nil {
		return err
	}
	return syncWorkspaceDirectory(filepath.Dir(workspaceFinalizationPath(place)))
}
func writeWorkspaceDurable(path string, raw []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".workspace-finalize-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(raw)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return syncWorkspaceDirectory(directory)
}
func syncWorkspaceDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// TaskWorkspaceAdmissionNeeded inspects only authenticated incoming metadata.
// Carriers still receive their complete validation before the lease is taken.
func TaskWorkspaceAdmissionNeeded(place Place) (bool, error) {
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		return false, err
	}
	if err := validateWorkspacePolicy(meta.WorkspacePolicy); err != nil {
		return false, err
	}
	if meta.WorkspacePolicy != nil || WorkspaceOwnershipPrototypeEnabled() {
		return true, nil
	}
	raw, err := readWorkspaceCheckpointBytes(place.Tasks())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	document, err := decodeTasks(raw)
	if err != nil {
		return false, err
	}
	if document.WorkspaceTransfer != nil {
		return true, nil
	}
	_, err = os.Lstat(filepath.Join(place.Dir, taskcopy.WorkspaceCarrierPath, "manifest.json"))
	return !errors.Is(err, os.ErrNotExist), nil
}
