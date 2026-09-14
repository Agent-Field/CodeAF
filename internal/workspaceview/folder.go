package workspaceview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// ── A FOLDER, READ THE WAY A PERSON BROWSES IT ──────────────────────────────
//
// [Resolver.Members] answers what a folder REFERENCES. A person standing in a
// folder asks a wider question — what is in here — and the answer has two
// sources that must never be mistaken for one another: what is FILED here (a
// membership, which organizes and grants nothing) and what is PLACED here (a
// governing placement, which is how this folder's rules reach work). Both doors
// that already answer the question print them as two lists (`aforge collections
// show`, the chat's `collections show`). A browsing surface needs ONE list with
// a cursor on it, so this file is the one place the two are joined — and the
// join is a presentation of two facts, not a third fact:
//
//   - A ROW IS ONE RECORD, AND ITS TWO FLAGS ARE COPIED FROM THEIR OWN TABLES.
//     Something both filed and placed is drawn once, at the position the person
//     filed it in, carrying Filed and Placed. Nothing is inferred: a membership
//     never makes a row Placed, a placement never makes it Filed, and nothing
//     here writes.
//
//   - THE PERSON'S ARRANGEMENT COMES FIRST. Filed rows keep the membership's own
//     insertion order ([workspace.Store.Members]); what is only placed follows,
//     in the placements key's order, because nobody arranged it.
//
//   - ONE READING OF THE MACHINE PER PAGE, as [Resolver.Resolve] promises. The
//     whole page is resolved in one call however many conversations it names.

// FolderLimit is the most rows one folder page carries. A folder holding more
// says so ([FolderPage.More]) rather than growing a reply without bound.
const FolderLimit = 500

// FolderRow is one record in a folder and how it is bound there.
type FolderRow struct {
	workspace.ResolvedRef
	// Filed says a membership files this record in the folder being read.
	Filed bool `json:"filed,omitempty"`
	// Placed says a governing placement puts it here, so this folder's rules reach
	// it. It is the only flag that says anything about authority.
	Placed bool `json:"placed,omitempty"`
}

// FolderPage is one folder's contents, or the top level when Folder is zero.
type FolderPage struct {
	Folder workspace.Collection `json:"folder"`
	Rows   []FolderRow          `json:"rows"`
	// More says the folder holds rows past [FolderLimit] that this page left out.
	More bool `json:"more,omitempty"`
}

// Folder reads one folder's page. An empty id is the top level: the collections
// no collection files as a member ([workspace.Store.TopLevel]).
//
// A FOLDER THAT IS NOT THERE IS [workspace.ErrNotFound], NEVER AN EMPTY PAGE —
// the store's own answer, carried through, for the reason [Resolver.Members]
// gives.
func (r Resolver) Folder(ctx context.Context, s *workspace.Store, id string, limit int) (FolderPage, error) {
	if s == nil {
		return FolderPage{}, ErrNoCollections
	}
	if limit <= 0 || limit > FolderLimit {
		limit = FolderLimit
	}
	if id == "" {
		return r.topLevel(ctx, s, limit)
	}
	if err := (workspace.Ref{Kind: workspace.CollectionKind, ID: id}).Validate(); err != nil {
		return FolderPage{}, err
	}
	members, err := s.Members(ctx, id)
	if err != nil {
		return FolderPage{}, err
	}
	// ENOUGH PLACEMENTS TO KNOW WHETHER THERE IS MORE AFTER DUPLICATES GO: at
	// most every member is also placed, so limit+1 unfiled placements are
	// always inside this window when they exist.
	placed, _, err := placedIn(ctx, s, id, limit+1+len(members))
	if err != nil {
		return FolderPage{}, err
	}
	page := FolderPage{Rows: make([]FolderRow, 0, min(len(members)+len(placed), limit))}
	if page.Folder, err = collectionNamed(ctx, s, id); err != nil {
		return FolderPage{}, err
	}
	isPlaced := make(map[workspace.Ref]bool, len(placed))
	for _, ref := range placed {
		isPlaced[ref] = true
	}
	isFiled := make(map[workspace.Ref]bool, len(members))
	for _, ref := range members {
		isFiled[ref] = true
		page.Rows = append(page.Rows, FolderRow{ResolvedRef: workspace.ResolvedRef{Ref: ref}, Filed: true, Placed: isPlaced[ref]})
	}
	for _, ref := range placed {
		if !isFiled[ref] {
			page.Rows = append(page.Rows, FolderRow{ResolvedRef: workspace.ResolvedRef{Ref: ref}, Placed: true})
		}
	}
	if len(page.Rows) > limit {
		page.Rows, page.More = page.Rows[:limit], true
	}
	refs := make([]workspace.Ref, len(page.Rows))
	for i, row := range page.Rows {
		refs[i] = row.Ref
	}
	resolved, err := r.Resolve(ctx, s, refs)
	if err != nil {
		return FolderPage{}, err
	}
	for i := range page.Rows {
		page.Rows[i].ResolvedRef = resolved[i]
	}
	return page, nil
}

// topLevel is where browsing starts. Its rows are neither filed nor placed —
// they are simply the folders nothing contains.
func (r Resolver) topLevel(ctx context.Context, s *workspace.Store, limit int) (FolderPage, error) {
	folders, err := s.TopLevel(ctx)
	if err != nil {
		return FolderPage{}, err
	}
	page := FolderPage{Rows: make([]FolderRow, 0, min(len(folders), limit))}
	for _, folder := range folders {
		if len(page.Rows) == limit {
			page.More = true
			break
		}
		page.Rows = append(page.Rows, FolderRow{ResolvedRef: workspace.ResolvedRef{
			Ref: workspace.Ref{Kind: workspace.CollectionKind, ID: folder.ID}, Title: folder.Name, Available: true,
		}})
	}
	return page, nil
}

// placedIn pages through a folder's placements by the cursor the store keeps
// ([workspace.PlacedWindow]), stopping once `want` rows are in hand.
func placedIn(ctx context.Context, s *workspace.Store, id string, want int) ([]workspace.Ref, bool, error) {
	var placed []workspace.Ref
	window := workspace.PlacedWindow{Limit: want}
	for {
		page, more, err := s.Placed(ctx, id, window)
		if err != nil {
			return nil, false, err
		}
		placed = append(placed, page...)
		if len(placed) >= want {
			return placed[:want], true, nil
		}
		if !more || len(page) == 0 {
			return placed, false, nil
		}
		window.After = page[len(page)-1]
	}
}

func collectionNamed(ctx context.Context, s *workspace.Store, id string) (workspace.Collection, error) {
	all, err := s.Collections(ctx)
	if err != nil {
		return workspace.Collection{}, err
	}
	for _, c := range all {
		if c.ID == id {
			return c, nil
		}
	}
	return workspace.Collection{}, workspace.ErrNotFound
}

// ── ONE SELECTED RECORD, READ DEEPER ────────────────────────────────────────

// ItemContextLimit is how many shared-context records one selection reads.
const ItemContextLimit = 6

// itemContextRunes bounds each record's text on the way to a surface: an
// inspector shows a record's title and its opening, and the whole text is the
// shared_context tool's to read by identity.
const itemContextRunes = 400

// FolderItem is what a surface shows beside one selected row. Every section is
// read independently, and a section that could not be read says so in Errors
// under its own name — A READ FAILURE IS NOT AN ABSENCE.
type FolderItem struct {
	Ref workspace.Ref `json:"ref"`
	// FiledIn is every folder that files this record, in creation order.
	FiledIn []workspace.Collection `json:"filed_in,omitempty"`
	// GovernedBy is every folder whose rules reach it, nearest first. Only
	// placements make this list; a membership never does.
	GovernedBy []workspace.GoverningCollection `json:"governed_by,omitempty"`
	// Context is the shared context that currently reaches the record, by the
	// same scope a conversation or a piece of work reads for itself. It is
	// information, never instructions or permission.
	Context     []workspace.ContextRecord `json:"context,omitempty"`
	ContextMore bool                      `json:"context_more,omitempty"`
	// Standing is the ongoing work itself, for a standing reference.
	Standing *standing.Item `json:"standing,omitempty"`
	// Rules are the rules that reach this record: for ongoing work, the reading
	// its own run takes ([RulesReaching]); for a folder, the rules scoped to it.
	Rules []standing.Item `json:"rules,omitempty"`
	// Work is what the owner of ongoing work says about its runs and its report,
	// for a standing reference.
	Work *WorkFacts `json:"work,omitempty"`
	// Errors names each section that could not be read, with the reason.
	Errors map[string]string `json:"errors,omitempty"`
}

// ItemRunLimit is how many of ongoing work's latest runs one selection reads.
const ItemRunLimit = 3

// WorkFacts is ongoing work as its owner records it, beside the item itself:
// the latest runs, the receipt at its report path, whether a pass has it in
// hand this instant, and how checks happen on the machine that keeps it.
//
// EVERY FIELD IS THE OWNER'S OWN RECORD, carried whole. Nothing here is inferred
// from a folder, and nothing a surface does with it writes anything back.
type WorkFacts struct {
	// Runs are the latest occurrences, newest first ([standing.Store.Occurrences]).
	Runs []standing.Occurrence `json:"runs,omitempty"`
	// ReportPath is the stored destination resolved as the publisher resolves it
	// ([standing.ReportPath]), and Receipt what aforge last put there, if anything.
	ReportPath string            `json:"report_path,omitempty"`
	Receipt    *standing.Receipt `json:"receipt,omitempty"`
	// Named are files the item's words or instructions name that could be a
	// report but are not the stored one ([session.StandingNamedFiles]).
	Named []string `json:"named,omitempty"`
	// Running is the pass holding the item right now, if one is.
	Running *standing.RunningMark `json:"running,omitempty"`
	// Checks is how checks happen here; nil when the reader could not say.
	Checks *CheckWays `json:"checks,omitempty"`
}

// CheckWays is how ongoing work gets checked on one machine, as that machine
// can state it. It is a claim about the machine and never about the item.
type CheckWays struct {
	// Window is whether this engine process is running the pass itself, every
	// [standing.Interval], while it is open.
	Window bool `json:"window"`
	// Timer is whether an operating-system timer is installed FOR THIS HOME
	// ([standing.WatchStatus.Installed] reads the definition's own home), and
	// TimerKnown whether that could be read at all; unknown claims nothing.
	Timer      bool `json:"timer,omitempty"`
	TimerKnown bool `json:"timer_known,omitempty"`
}

// Item reads one selected record's detail. Nothing is written.
func (r Resolver) Item(ctx context.Context, s *workspace.Store, ref workspace.Ref) (FolderItem, error) {
	if err := ref.Validate(); err != nil {
		return FolderItem{}, err
	}
	if s == nil {
		return FolderItem{}, ErrNoCollections
	}
	item := FolderItem{Ref: ref}
	fail := func(section string, err error) {
		if item.Errors == nil {
			item.Errors = map[string]string{}
		}
		item.Errors[section] = err.Error()
	}
	var err error
	if item.FiledIn, err = s.CollectionsFor(ctx, ref); err != nil {
		fail("filed_in", err)
	}
	if item.GovernedBy, err = s.GoverningCollections(ctx, ref); err != nil {
		fail("governed_by", err)
	}
	page, err := s.ContextPage(ctx, session.OrganizationScope(ref), true, 0, ItemContextLimit)
	if err != nil {
		fail("context", err)
	} else {
		item.Context, item.ContextMore = page.Records, page.More
		for i := range item.Context {
			if text := []rune(item.Context[i].Text); len(text) > itemContextRunes {
				item.Context[i].Text = string(text[:itemContextRunes])
			}
		}
	}
	switch ref.Kind {
	case workspace.StandingKind:
		if r.Standing == nil {
			fail("standing", ErrNoStandingStore)
			break
		}
		got, err := r.Standing.Get(ref.ID)
		if err != nil {
			fail("standing", err)
			break
		}
		item.Standing = &got
		if _, rules, err := RulesReaching(ctx, s, r.Standing, got); err != nil {
			fail("rules", err)
		} else {
			item.Rules = rules
		}
		item.Work = r.work(got, fail)
	case workspace.CollectionKind:
		if r.Standing == nil {
			break
		}
		if rules, err := rulesScopedTo(r.Standing, ref.ID); err != nil {
			fail("rules", err)
		} else {
			item.Rules = rules
		}
	}
	return item, ctx.Err()
}

// RulesReaching is THE RUN'S OWN READING of which rules reach one piece of
// ongoing work: its folders, from placements alone, then the store's resolver
// over its workspace, the conversation it was set up in, and those folders —
// less itself, and only the holds with words among what applies. It is the one
// spelling of that reading: `aforge standing show` and a browsing surface both
// call it, so the two cannot come to list different rules.
//
// org may be nil, which is a machine with no folders: no placement can exist,
// so only the rules its workspace and conversation reach apply.
func RulesReaching(ctx context.Context, org *workspace.Store, store *standing.Store, item standing.Item) ([]workspace.GoverningCollection, []standing.Item, error) {
	collections := map[string]int{}
	var places []workspace.GoverningCollection
	if org != nil {
		var err error
		places, err = org.GoverningCollections(ctx, workspace.Ref{Kind: workspace.StandingKind, ID: item.ID})
		if err != nil {
			return nil, nil, err
		}
		for _, place := range places {
			collections[place.ID] = place.Depth
		}
	}
	applicable, err := store.ApplicableScope(item.Workspace, item.Origin.SessionID, collections)
	if err != nil {
		return places, nil, err
	}
	var rules []standing.Item
	for _, rule := range session.GoverningRules(applicable) {
		if rule.ID != item.ID {
			rules = append(rules, rule)
		}
	}
	return places, rules, nil
}

// rulesScopedTo is the rules written for one folder: the store's own resolver
// asked as if something were placed directly in it, keeping only the rules that
// name folders. A machine-wide rule applies to everything and is not this
// folder's to list.
func rulesScopedTo(store *standing.Store, id string) ([]standing.Item, error) {
	applicable, err := store.ApplicableScope("", "", map[string]int{id: 0})
	if err != nil {
		return nil, err
	}
	var rules []standing.Item
	for _, rule := range session.GoverningRules(applicable) {
		if rule.Scope != nil {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

// ── A FILED FILE, READ BY THE MACHINE THAT HOLDS IT ─────────────────────────

// ArtifactPreviewBytes is the most of a file one preview carries.
const ArtifactPreviewBytes = 64 << 10

// ErrNotFiled refuses a preview of a path no folder refers to. The door reads
// what the person's folders name and nothing else, so it cannot become a way to
// read any file on the machine that serves it.
var ErrNotFiled = errors.New("that file is not in any folder")

// ArtifactPreview is the opening of one file, as the machine holding it read it.
type ArtifactPreview struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	// Text is the opening of the file when it reads as text, and Binary says it
	// did not — a preview never draws bytes a terminal would take as commands.
	Text      string `json:"text,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Artifact reads the opening of a file a folder refers to, filed or placed.
func (r Resolver) Artifact(ctx context.Context, s *workspace.Store, path string) (ArtifactPreview, error) {
	if s == nil {
		return ArtifactPreview{}, ErrNoCollections
	}
	ref := workspace.Ref{Kind: workspace.ArtifactKind, ID: path}
	if err := ref.Validate(); err != nil {
		return ArtifactPreview{}, err
	}
	filed, err := s.CollectionsFor(ctx, ref)
	if err != nil {
		return ArtifactPreview{}, err
	}
	if len(filed) == 0 {
		governing, err := s.GoverningCollections(ctx, ref)
		if err != nil {
			return ArtifactPreview{}, err
		}
		if len(governing) == 0 {
			return ArtifactPreview{}, ErrNotFiled
		}
	}
	// ONLY A REGULAR FILE IS OPENED, AND IT IS ASKED BEFORE THE OPEN AS WELL AS
	// AFTER. A folder can name any clean absolute path, and opening a named pipe
	// waits for a writer forever, while /dev/stdin or /proc/self/fd/0 on an
	// engine is the wire itself — so a path that is not a plain file is refused
	// by its mode, the open cannot block (O_NONBLOCK), and the handle is checked
	// again in case the path was swapped between the two looks.
	if info, err := os.Stat(path); err != nil {
		return ArtifactPreview{}, err
	} else if err := previewable(path, info); err != nil {
		return ArtifactPreview{}, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ArtifactPreview{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ArtifactPreview{}, err
	}
	if err := previewable(path, info); err != nil {
		return ArtifactPreview{}, err
	}
	preview := ArtifactPreview{Path: path, Size: info.Size(), Modified: info.ModTime()}
	head := make([]byte, ArtifactPreviewBytes)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return ArtifactPreview{}, err
	}
	head = head[:n]
	preview.Truncated = info.Size() > int64(n)
	if preview.Truncated {
		// A CUT MAY LAND INSIDE A CHARACTER, and a torn rune is not binary.
		for cut := 0; cut < utf8.UTFMax-1 && len(head) > 0 && !utf8.Valid(head); cut++ {
			head = head[:len(head)-1]
		}
	}
	if bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(head) {
		preview.Binary = true
		return preview, nil
	}
	preview.Text = strings.ToValidUTF8(string(head), "")
	return preview, nil
}

// previewable refuses what a preview may not read: a folder, and anything that
// is not a plain file (a pipe, a socket, a device).
func previewable(path string, info os.FileInfo) error {
	switch {
	case info.IsDir():
		return fmt.Errorf("%s is a folder on disk, not a file", filepath.Base(path))
	case !info.Mode().IsRegular():
		return fmt.Errorf("%s is not a plain file, so it is not read", filepath.Base(path))
	}
	return nil
}

// work reads ongoing work's runs, report receipt and running mark from its
// owner. Each part that fails is named in the item's errors and the rest stand.
func (r Resolver) work(got standing.Item, fail func(string, error)) *WorkFacts {
	facts := &WorkFacts{ReportPath: standing.ReportPath(got), Named: session.StandingNamedFiles(got)}
	runs, err := r.Standing.Occurrences(got.ID, ItemRunLimit)
	if err != nil {
		fail("runs", err)
	}
	facts.Runs = runs
	if facts.ReportPath != "" {
		if facts.Receipt, err = r.Standing.Receipt(facts.ReportPath); err != nil {
			fail("receipt", err)
		}
	}
	if mark, ok := r.Standing.Running(got.ID); ok {
		facts.Running = &mark
	}
	if r.Checks != nil {
		ways := r.Checks()
		facts.Checks = &ways
	}
	return facts
}
