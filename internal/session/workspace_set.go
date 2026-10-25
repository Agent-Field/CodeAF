package session

import (
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// WorkspaceAccess describes intended use, not an operating-system sandbox.
type WorkspaceAccess string

const workspaceSetRootLimit = 64

const (
	WorkspaceRead  WorkspaceAccess = "read"
	WorkspaceWrite WorkspaceAccess = "write"
)

// WorkspaceRootRequest explicitly selects one source and its relative layout.
// Slots preserve supported ASCII spelling; case-fold aliases, Unicode and
// reserved filesystem names refuse rather than being renamed.
// Landing is empty to retain work privately, or the explicitly admitted source
// folder. Matching names or Git remotes never supply landing permission.
type WorkspaceRootRequest struct {
	// SourceURI is authoritative once admitted; Source is a local path hint.
	SourceURI  string          `json:"source_uri,omitempty"`
	LandingURI string          `json:"landing_uri,omitempty"`
	Source     string          `json:"source"`
	Slot       string          `json:"slot"`
	Access     WorkspaceAccess `json:"access"`
	Landing    string          `json:"landing,omitempty"`
}

// WorkspaceSetRequest belongs to one execution, not to the conversation's
// lifetime. Slots describe actual sibling directories, never a common ancestor.
type WorkspaceSetRequest struct {
	Primary string                 `json:"primary"`
	Roots   []WorkspaceRootRequest `json:"roots"`
}

// WorkspaceRootUse names one admitted copy. Copy.URI retains its existing
// one-copy meaning even when several copies belong to the same execution.
type WorkspaceRootUse struct {
	SourceURI string          `json:"source_uri,omitempty"`
	Slot      string          `json:"slot"`
	Access    WorkspaceAccess `json:"access"`
	Landing   string          `json:"landing,omitempty"`
	Copy      *TaskCopyRecord `json:"copy"`
	Outcome   string          `json:"outcome,omitempty"`
}

// WorkspaceSet is the durable execution layout. History-only roots remain in
// their earlier result records; they are not required to start this execution.
type WorkspaceSet struct {
	Version int                `json:"version"`
	Primary string             `json:"primary"`
	Roots   []WorkspaceRootUse `json:"roots"`
}

// StartTaskInWorkspaceSet admits an explicitly declared private task layout.
func (a *Agent) StartTaskInWorkspaceSet(ctx context.Context, brief string, request WorkspaceSetRequest) (uint64, string, string, error) {
	if !WorkspaceOwnershipEnabled(a.config.Place) || !bashBeltAsked() {
		return 0, "", "", errors.New("workspace sets require private task placement and the Bash task engine")
	}

	if strings.TrimSpace(brief) == "" {
		return 0, "", "", errors.New("task brief is empty")
	}
	g := a.graph()
	if chatRunEngine == nil || g == nil || g.planPath() == "" {
		return 0, "", "", errRunRoadUnavailable
	}
	if err := admitWorkspaceSetRequest(ctx, a.config.Place, &request); err != nil {
		return 0, "", "", err
	}
	id := g.reserve()
	title := taskPersonTitle(brief)
	primary := request.Roots[0]
	for _, root := range request.Roots {
		if root.Slot == request.Primary {
			primary = root
		}
	}
	stand := taskStand{dir: primary.Source, mode: TaskModeMirror, workspaceSet: &request}
	if err := a.startKnownTaskRun(ctx, id, title, brief, nil, stand, ""); err != nil {
		return 0, "", "", err
	}
	return id, title, "", nil
}

func cloneWorkspaceSet(set *WorkspaceSet) *WorkspaceSet {
	if set == nil {
		return nil
	}
	out := *set
	out.Roots = append([]WorkspaceRootUse(nil), set.Roots...)
	for i := range out.Roots {
		if copy := out.Roots[i].Copy; copy != nil {
			v := *copy
			v.Untracked = append([]string(nil), copy.Untracked...)
			out.Roots[i].Copy = &v
		}
	}
	return &out
}
func sameWorkspaceSetRequest(a, b *WorkspaceSetRequest) bool { return reflect.DeepEqual(a, b) }

// Clone returns worker-owned metadata; callers cannot change seated routes.
func (s *WorkspaceSet) Clone() *WorkspaceSet { return cloneWorkspaceSet(s) }

// WorkerInstructions routes every delegated worker, independently of the
// planner's subtask prose. Instruction files stay scoped to their own copies.
func (s *WorkspaceSet) WorkerInstructions() string {
	if s == nil {
		return ""
	}
	var text strings.Builder
	primaryDir := "unavailable"
	for _, root := range s.Roots {
		if root.Slot == s.Primary && root.Copy != nil {
			primaryDir = root.Copy.Dir
		}
	}
	text.WriteString("The harness prepared these private working roots. Use these copies for all task work; do not write the source folders. Primary current directory (" + s.Primary + "): " + primaryDir + ".\n")
	for _, root := range s.Roots {
		if root.Copy != nil {
			fmt.Fprintf(&text, "- %s: %s (intended %s; landing %s)\n", root.Slot, root.Copy.Dir, root.Access, root.Landing)
		}
	}
	text.WriteString("These routes are not an operating-system sandbox. Read roots must not be changed. No landing destination means keep output in the private copy. Rules from one root, including the primary, apply only within that root; do not blend conflicting rules across sibling roots. Read more specific nested instructions before editing nested files.\n")
	spent := 0
	for _, root := range s.Roots {
		if root.Copy == nil {
			continue
		}
		for _, name := range []string{agentsFileName, claudeFileName} {
			file := filepath.Join(root.Copy.Dir, name)
			if spent >= attachedFilesBudget {
				fmt.Fprintf(&text, "Read %s for instructions if work enters this root; the quoted instruction budget is exhausted.\n", file)
				continue
			}
			rules, truncated := readInstructionFileWithin(root.Copy.Dir, name, min(attachedFileLimit, attachedFilesBudget-spent))
			if rules == "" && !truncated {
				continue
			}
			spent += len(rules)
			fmt.Fprintf(&text, "\n## Instructions for %s\nThese hold for work UNDER %s AND NOWHERE ELSE.\n", root.Copy.Dir, root.Copy.Dir)
			fence := fenceFor(rules)
			text.WriteString(fence + "text\n" + rules + "\n" + fence + "\n")
			if truncated {
				fmt.Fprintf(&text, "Instructions were truncated; read %s for the remainder before relevant work.\n", file)
			}
		}
	}
	return text.String()
}

// reopenWorkspaceSet adopts every required copy before execution is allowed.
// Old rows retain their exact singleton adoption road.
func reopenWorkspaceSet(copy *TaskCopyRecord, set *WorkspaceSet, place Place) (taskTree, error) {
	if set != nil {
		if err := workspaceFinalizationPending(place); err != nil {
			return taskTree{}, err
		}
		if err := taskcopy.WorkspaceReady(cell.Cell{Root: place.Dir}); err != nil {
			return taskTree{}, err
		}
	}

	if set == nil {
		return runCopyTree(copy, place)
	}
	if !WorkspaceOwnershipEnabled(place) || set.Version != 1 || len(set.Roots) == 0 || len(set.Roots) > workspaceSetRootLimit {
		return taskTree{}, errors.New("unsupported workspace set record")
	}
	set = cloneWorkspaceSet(set)
	seen := map[string]bool{}
	folded := map[string]bool{}
	primary := false
	layoutBase := ""
	var grounds []string
	for _, use := range set.Roots {
		if use.Copy == nil || use.Copy.URI == "" || !validWorkspaceSlot(use.Slot) || seen[use.Slot] || folded[strings.ToLower(use.Slot)] || use.Access != WorkspaceRead && use.Access != WorkspaceWrite {
			return taskTree{}, errors.New("invalid workspace root record")
		}
		seen[use.Slot] = true
		folded[strings.ToLower(use.Slot)] = true
		if use.Landing != "" && (use.Access != WorkspaceWrite || use.Landing != use.Copy.GroundURI) {
			return taskTree{}, errors.New("workspace root landing does not match admitted source identity")
		}
		resolved, err := resolveOwnedWorkspace(use.Copy, place)
		if err != nil {
			return taskTree{}, err
		}
		if filepath.Base(resolved) != use.Slot || layoutBase != "" && filepath.Dir(resolved) != layoutBase {
			return taskTree{}, errors.New("workspace copy bindings no longer preserve the declared sibling layout")
		}
		layoutBase = filepath.Dir(resolved)
		_, ground, err := runCopyLandingPaths(use.Copy)
		if err != nil {
			return taskTree{}, err
		}
		for _, prior := range grounds {
			if withinDir(prior, ground) || withinDir(ground, prior) {
				return taskTree{}, errors.New("workspace source bindings now overlap; explicit ownership mapping is required")
			}
		}
		grounds = append(grounds, ground)
		if use.Slot == set.Primary {
			primary = true
			if copy == nil || copy.URI != use.Copy.URI {
				return taskTree{}, errors.New("workspace primary does not match its legacy copy projection")
			}
		}
	}
	if !primary {
		return taskTree{}, errors.New("workspace primary copy is absent")
	}
	var tree taskTree
	for i, use := range set.Roots {
		adopted, err := runCopyTree(use.Copy, place)
		if err != nil {
			return taskTree{}, err
		}
		set.Roots[i].Copy = runCopyOf(adopted)
		if use.Slot == set.Primary {
			tree = adopted
		}
	}
	tree.workspaceSet = set
	return tree, nil
}

func workspaceSetRequestOf(set *WorkspaceSet) *WorkspaceSetRequest {
	if set == nil {
		return nil
	}
	request := &WorkspaceSetRequest{Primary: set.Primary}
	for _, use := range set.Roots {
		landing := ""
		if use.Landing != "" {
			landing = use.Copy.Ground
		}
		request.Roots = append(request.Roots, WorkspaceRootRequest{Source: use.Copy.Ground, Slot: use.Slot, Access: use.Access, Landing: landing})
	}
	return request
}

func cloneWorkspaceSetRequest(request *WorkspaceSetRequest) *WorkspaceSetRequest {
	if request == nil {
		return nil
	}
	copied := *request
	copied.Roots = append([]WorkspaceRootRequest(nil), request.Roots...)
	return &copied
}

// workspaceTransferProjection is derived from the authoritative run records in
// the same checkpoint. It never discovers membership by scanning directories.
func workspaceTransferProjection(document taskDocument, place Place) (*taskcopy.WorkspacePlan, error) {
	hasSet := false
	for _, row := range document.Runs {
		if row.WorkspaceSet != nil || row.PendingRun != nil && row.PendingRun.WorkspaceSet != nil {
			hasSet = true
			break
		}
	}
	if !hasSet {
		return nil, nil
	}

	if err := validateWorkspaceTransferCoverage(document); err != nil {
		return nil, err
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return nil, err
	}
	chat, err := registry.Register(workspaceidentity.Chat, canonicalPath(place.Dir))
	if err != nil {
		return nil, err
	}
	return workspaceTransferProjectionFor(document, place, chat.URI)
}

// workspaceTransferProjectionFor is pure: receiving code supplies authenticated
// chat authority and resolved layout paths before comparing the projection.
func workspaceTransferProjectionFor(document taskDocument, place Place, chatURI string) (*taskcopy.WorkspacePlan, error) {
	if err := validateWorkspaceTransferCoverage(document); err != nil {
		return nil, err
	}
	var plan *taskcopy.WorkspacePlan
	for _, row := range document.Runs {
		if row.WorkspaceSet == nil && (row.PendingRun == nil || row.PendingRun.WorkspaceSet == nil) {
			continue
		}
		if plan == nil {
			plan = &taskcopy.WorkspacePlan{Version: 2, ChatURI: chatURI}
		}

		group := taskcopy.WorkspaceGroup{TaskID: row.ID, Required: row.State == TaskQueued || row.State == TaskRunning}
		if row.WorkspaceSet != nil {
			group.Primary = row.WorkspaceSet.Primary
			for _, use := range row.WorkspaceSet.Roots {
				entry := taskcopy.WorkspaceEntry{ChatURI: plan.ChatURI, RootURI: use.SourceURI, Layout: strconv.FormatUint(row.ID, 10) + "/" + use.Slot, Access: string(use.Access)}
				if use.Access == WorkspaceWrite {
					entry.Access = string(WorkspaceWrite)
				}
				if use.Copy == nil {
					group.Incomplete = true
				} else {
					copy := use.Copy
					entry.CopyURI, entry.ChatURI, entry.ProjectURI, entry.RootURI, entry.GroundURI = copy.URI, copy.ChatURI, copy.ProjectURI, copy.RootURI, copy.GroundURI
					if entry.RootURI == "" {
						entry.RootURI = use.SourceURI
					}
					if entry.RootURI == "" {
						entry.RootURI = copy.GroundURI
					}

					layout, err := filepath.Rel(place.Trees(), copy.Dir)
					if err != nil || filepath.IsAbs(layout) || strings.HasPrefix(layout, ".."+string(filepath.Separator)) || layout == ".." {
						return nil, errors.New("workspace transfer copy lies outside owned trees")
					}
					entry.Layout = filepath.ToSlash(layout)
				}
				group.Entries = append(group.Entries, entry)
			}
		} else {
			request := row.PendingRun.WorkspaceSet
			group.Primary, group.Incomplete = request.Primary, true
			for _, root := range request.Roots {
				access := string(WorkspaceRead)
				if root.Access == WorkspaceWrite {
					access = string(WorkspaceWrite)
				}
				group.Entries = append(group.Entries, taskcopy.WorkspaceEntry{ChatURI: plan.ChatURI, RootURI: root.SourceURI, Layout: strconv.FormatUint(row.ID, 10) + "/" + root.Slot, Access: access})
			}
		}
		plan.Groups = append(plan.Groups, group)
	}
	return plan, nil
}

func validateWorkspaceTransferCoverage(document taskDocument) error {
	hasSet := false
	for _, row := range document.Runs {
		if row.WorkspaceSet != nil || row.PendingRun != nil && row.PendingRun.WorkspaceSet != nil {
			hasSet = true
			break
		}
	}
	if !hasSet {
		return nil
	}
	covered := map[string]bool{}
	for _, row := range document.Runs {
		if row.WorkspaceSet != nil {
			for _, root := range row.WorkspaceSet.Roots {
				if root.Copy != nil {
					covered[root.Copy.URI] = true
				}
			}
		}
	}
	for _, row := range document.Runs {
		if row.WorkspaceSet == nil && (row.Copy != nil && row.Copy.URI != "" && !covered[row.Copy.URI] || row.PendingRun != nil && row.PendingRun.WorkspaceSet == nil) {
			return errors.New("workspace-set prototype cannot preserve mixed legacy task copies; keep this conversation local and start the multi-root prototype in a new conversation")
		}
	}
	for _, node := range document.Nodes {
		if node.WorkspaceURI != "" && !covered[node.WorkspaceURI] {
			return errors.New("workspace-set prototype cannot preserve mixed legacy node copies; original work is retained locally")
		}
	}

	return nil
}
