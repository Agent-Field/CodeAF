package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/workstream/pagedoc"
)

const (
	SuggestionsMax     = 5_000
	SuggestionEditsMax = 50
	SuggestionTextMax  = 4_000
	SuggestionWhyMax   = 200
)

var ErrNotPersonPage = errors.New("That page is yours; change it with write_agent_page. Suggest edits only on the person's pages.")

type Suggestion struct {
	ID, WorkstreamID, PageID string
	BaseVersion              int
	Why, By                  string
	Source                   *Source
	CreatedAt                time.Time
	Edits                    []SuggestionEdit
}

type SuggestionEdit struct {
	ID                                    string
	Block                                 []int
	Offset                                int
	Delete, Insert, BlockText, Why, State string
	Applies                               *bool
	DecidedAt                             time.Time
}

type SuggestionFind struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
	Why     string `json:"why"`
}

type SuggestionCreate struct {
	WorkstreamID, PageID string
	BaseVersion          int
	Why                  string
	Edits                []SuggestionFind
	Attribution
}

// ValidateSuggestion checks byte bounds independently of any provider schema.
func ValidateSuggestion(pageID string, base int, why string, edits []SuggestionFind) error {
	if strings.TrimSpace(pageID) == "" || base < 1 {
		return invalid("page_id and a positive base_version are required")
	}
	if len(why) < 1 || !lineWithin(why, SuggestionWhyMax) {
		return invalid("why must be one line of 1–200 bytes")
	}
	if len(edits) < 1 || len(edits) > SuggestionEditsMax {
		return invalid("edits must contain 1–50 edits")
	}
	for _, edit := range edits {
		if len(edit.Find) < 1 || len(edit.Find) > SuggestionTextMax || !utf8.ValidString(edit.Find) || len(edit.Replace) > SuggestionTextMax || !utf8.ValidString(edit.Replace) {
			return invalid("find must be 1–4000 bytes and replace 0–4000 bytes")
		}
		if len(edit.Why) < 1 || !lineWithin(edit.Why, SuggestionWhyMax) {
			return invalid("edit why must be one line of 1–200 bytes")
		}
	}
	return nil
}

// SuggestionTextError is a normal tool refusal, with the caller's edit numbers.
type SuggestionTextError struct{ Message string }

func (e *SuggestionTextError) Error() string { return e.Message }

func suggestionTextError(n int, message string) error {
	return &SuggestionTextError{Message: fmt.Sprintf("Edit %d: %s", n, message)}
}

func resolveSuggestionEdit(blocks []pagedoc.TextBlock, ask SuggestionFind, n int) (SuggestionEdit, error) {
	if ask.Find == ask.Replace {
		return SuggestionEdit{}, suggestionTextError(n, "replace is the same as find.")
	}
	var match pagedoc.TextBlock
	at, count := 0, 0
	for _, block := range blocks {
		for from := 0; from <= len(block.Text)-len(ask.Find); {
			index := strings.Index(block.Text[from:], ask.Find)
			if index < 0 {
				break
			}
			at, match = from+index, block
			count++
			// Count overlapping occurrences too; an ambiguous quote is never guessed.
			_, width := utf8.DecodeRuneInString(block.Text[at:])
			from = at + width
		}
	}
	if count == 0 {
		return SuggestionEdit{}, suggestionTextError(n, "that text is not on the page. Quote plain text from one paragraph, heading, list item or code block exactly as it reads, without markdown marks.")
	}
	if count != 1 {
		return SuggestionEdit{}, suggestionTextError(n, "that text appears more than once. Quote more of it so it is unique.")
	}
	if match.Kind != "codeBlock" && strings.ContainsAny(ask.Replace, "\r\n") {
		return SuggestionEdit{}, suggestionTextError(n, "only a code block's text may contain a line break.")
	}
	offset, del, ins := pagedoc.TrimEdit(ask.Find, ask.Replace)
	return SuggestionEdit{Block: match.Path, Offset: utf8.RuneCountInString(match.Text[:at]) + offset, Delete: del, Insert: ins, BlockText: match.Text, Why: ask.Why, State: "proposed"}, nil
}

func resolveSuggestion(doc json.RawMessage, asks []SuggestionFind) ([]SuggestionEdit, error) {
	root, err := pagedoc.Decode(doc)
	if err != nil {
		return nil, err
	}
	blocks := pagedoc.TextBlocks(root)
	edits := make([]SuggestionEdit, 0, len(asks))
	for i, ask := range asks {
		edit, err := resolveSuggestionEdit(blocks, ask, i+1)
		if err != nil {
			return nil, err
		}
		for j, other := range edits {
			if slices.Equal(edit.Block, other.Block) && edit.Offset <= other.Offset+utf8.RuneCountInString(other.Delete) && other.Offset <= edit.Offset+utf8.RuneCountInString(edit.Delete) {
				return nil, &SuggestionTextError{Message: fmt.Sprintf("Edits %d and %d touch the same text. Merge them into one edit.", j+1, i+1)}
			}
		}
		edits = append(edits, edit)
	}
	sort.SliceStable(edits, func(i, j int) bool { return suggestionEditLess(edits[i], edits[j]) })
	for i := range edits {
		edits[i].ID = fmt.Sprintf("e%d", i+1)
	}
	return edits, nil
}

func suggestionEditLess(a, b SuggestionEdit) bool {
	if order := slices.Compare(a.Block, b.Block); order != 0 {
		return order < 0
	}
	return a.Offset < b.Offset
}

func (s *Store) CreateSuggestion(ctx context.Context, p SuggestionCreate) (Suggestion, Change, error) {
	var out Suggestion
	if p.By != "agent" {
		return out, Change{}, invalid("only the agent suggests edits")
	}
	if err := ValidateSuggestion(p.PageID, p.BaseVersion, p.Why, p.Edits); err != nil {
		return out, Change{}, err
	}
	source, err := sourceJSON(p.From)
	if err != nil {
		return out, Change{}, err
	}
	err = s.workspace.Write(ctx, func(tx *sql.Tx) error {
		page, err := suggestionCreatePage(ctx, tx, p)
		if err != nil {
			return err
		}
		edits, err := resolveSuggestion(page.Doc, p.Edits)
		if err != nil {
			return err
		}
		id, err := s.newID()
		if err != nil {
			return err
		}
		out = Suggestion{ID: "sg_" + id, WorkstreamID: page.WorkstreamID, PageID: page.ID, BaseVersion: p.BaseVersion, Why: p.Why, By: "agent", Source: p.From, CreatedAt: instant(s.now().UnixMilli()), Edits: edits}
		if err := insertSuggestion(ctx, tx, out, source); err != nil {
			return err
		}
		if err := touch(ctx, tx, out.WorkstreamID, out.CreatedAt.UnixMilli()); err != nil {
			return err
		}
		return locateSuggestion(&out, page.Doc)
	})
	if err != nil {
		return Suggestion{}, Change{}, err
	}
	return out, suggestionChanged(out.WorkstreamID), nil
}

func suggestionCreatePage(ctx context.Context, tx *sql.Tx, p SuggestionCreate) (Page, error) {
	page, _, err := livePageIn(ctx, tx, p.PageID)
	if err != nil {
		return page, err
	}
	if page.WorkstreamID != p.WorkstreamID {
		return page, ErrNotFound
	}
	if err := workstreamWriteAllowed(ctx, tx, page.WorkstreamID, "agent"); err != nil {
		return page, err
	}
	if page.Owner != "you" {
		return page, ErrNotPersonPage
	}
	if page.Version != p.BaseVersion {
		return page, suggestionVersionConflict(page)
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM workstream_suggestions WHERE workstream_id=?", page.WorkstreamID).Scan(&count); err != nil {
		return page, err
	}
	if count >= SuggestionsMax {
		return page, ErrTooLarge
	}
	return page, nil
}

func insertSuggestion(ctx context.Context, tx *sql.Tx, suggestion Suggestion, source any) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO workstream_suggestions(id,workstream_id,page_id,base_version,why,actor,source,created_at) VALUES (?,?,?,?,?,?,?,?)`, suggestion.ID, suggestion.WorkstreamID, suggestion.PageID, suggestion.BaseVersion, suggestion.Why, suggestion.By, source, suggestion.CreatedAt.UnixMilli()); err != nil {
		return err
	}
	for i, edit := range suggestion.Edits {
		block, err := json.Marshal(edit.Block)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workstream_suggestion_edits(suggestion_id,id,position,block,at_offset,del,ins,block_text,why,state) VALUES (?,?,?,?,?,?,?,?,?,'proposed')`, suggestion.ID, edit.ID, i, string(block), edit.Offset, edit.Delete, edit.Insert, edit.BlockText, edit.Why); err != nil {
			return err
		}
	}
	return nil
}

func suggestionChanged(id string) Change { return Change{ID: id, Parts: []ChangedPart{PartPages}} }
func suggestionVersionConflict(p Page) error {
	return &VersionConflict{ID: p.ID, Version: p.Version, UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy}
}

func suggestionIn(ctx context.Context, tx *sql.Tx, id string) (Suggestion, error) {
	var out Suggestion
	var source sql.NullString
	var at int64
	err := tx.QueryRowContext(ctx, `SELECT id,workstream_id,page_id,base_version,why,actor,source,created_at FROM workstream_suggestions WHERE id=?`, id).Scan(&out.ID, &out.WorkstreamID, &out.PageID, &out.BaseVersion, &out.Why, &out.By, &source, &at)
	if err != nil {
		return out, found(err)
	}
	out.CreatedAt = instant(at)
	out.Source, err = readSource(source)
	if err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,block,at_offset,del,ins,block_text,why,state,decided_at FROM workstream_suggestion_edits WHERE suggestion_id=? ORDER BY position`, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Edits = []SuggestionEdit{}
	for rows.Next() {
		var edit SuggestionEdit
		var block string
		var decided sql.NullInt64
		if err := rows.Scan(&edit.ID, &block, &edit.Offset, &edit.Delete, &edit.Insert, &edit.BlockText, &edit.Why, &edit.State, &decided); err != nil {
			return out, err
		}
		if err := json.Unmarshal([]byte(block), &edit.Block); err != nil {
			return out, err
		}
		edit.DecidedAt = optionalInstant(decided)
		out.Edits = append(out.Edits, edit)
	}
	return out, rows.Err()
}

func locateSuggestion(suggestion *Suggestion, doc json.RawMessage) error {
	root, err := pagedoc.Decode(doc)
	if err != nil {
		return err
	}
	for i := range suggestion.Edits {
		edit := &suggestion.Edits[i]
		edit.Applies = nil
		if edit.State != "proposed" {
			continue
		}
		block, ok := pagedoc.Locate(root, edit.Block, edit.BlockText)
		edit.Applies = &ok
		if ok {
			edit.Block = block.Path
		}
	}
	return nil
}

func (s *Store) Suggestions(ctx context.Context, pageID string) ([]Suggestion, error) {
	out := []Suggestion{}
	err := s.workspace.Read(ctx, func(tx *sql.Tx, version int) error {
		if version < 3 {
			return ErrNotFound
		}
		page, _, err := livePageIn(ctx, tx, pageID)
		if err != nil || version < workspace.WorkstreamSuggestionSchemaVersion {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT s.id FROM workstream_suggestions s WHERE s.page_id=? AND EXISTS(SELECT 1 FROM workstream_suggestion_edits e WHERE e.suggestion_id=s.id AND e.state='proposed') ORDER BY s.created_at,s.rowid`, pageID)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		for _, id := range ids {
			suggestion, err := suggestionIn(ctx, tx, id)
			if err != nil {
				return err
			}
			if err := locateSuggestion(&suggestion, page.Doc); err != nil {
				return err
			}
			out = append(out, suggestion)
		}
		return nil
	})
	return out, err
}

func suggestionTargets(suggestion Suggestion, editID string) ([]int, error) {
	targets := []int{}
	found := editID == ""
	for i, edit := range suggestion.Edits {
		if editID != "" && edit.ID != editID {
			continue
		}
		found = true
		if edit.State == "proposed" {
			targets = append(targets, i)
		} else if editID != "" {
			return nil, ErrConflict
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	if len(targets) == 0 {
		return nil, ErrConflict
	}
	return targets, nil
}

func suggestionDecisionIn(ctx context.Context, tx *sql.Tx, id string) (Suggestion, Page, error) {
	suggestion, err := suggestionIn(ctx, tx, id)
	if err != nil {
		return suggestion, Page{}, err
	}
	page, _, err := livePageIn(ctx, tx, suggestion.PageID)
	if err != nil {
		return suggestion, page, err
	}
	w, err := getIn(ctx, tx, suggestion.WorkstreamID)
	if err == nil && !w.DoneAt.IsZero() {
		err = ErrConflict
	}
	return suggestion, page, err
}

// keepSuggestionDoc checks every selected anchor before applying any edit.
func keepSuggestionDoc(suggestion *Suggestion, page Page, targets []int, at time.Time) (json.RawMessage, error) {
	if err := locateSuggestion(suggestion, page.Doc); err != nil {
		return nil, err
	}
	for _, i := range targets {
		if !*suggestion.Edits[i].Applies {
			return nil, suggestionVersionConflict(page)
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		return suggestionEditLess(suggestion.Edits[targets[i]], suggestion.Edits[targets[j]])
	})
	root, err := pagedoc.Decode(page.Doc)
	if err != nil {
		return nil, err
	}
	for _, i := range targets {
		edit := &suggestion.Edits[i]
		block, ok := pagedoc.Locate(root, edit.Block, edit.BlockText)
		if !ok {
			return nil, suggestionVersionConflict(page)
		}
		if err := pagedoc.Apply(block, edit.Offset, edit.Delete, edit.Insert); err != nil {
			return nil, suggestionVersionConflict(page)
		}
		edit.Block, edit.State, edit.DecidedAt, edit.Applies = block.Path, "kept", at, nil
		rebaseSuggestion(suggestion, *edit, block.Path)
	}
	doc, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	if err := checkDoc(doc); err != nil {
		return nil, err
	}
	return doc, locateSuggestion(suggestion, doc)
}

func rebaseSuggestion(suggestion *Suggestion, kept SuggestionEdit, path []int) {
	text := []rune(kept.BlockText)
	end := kept.Offset + utf8.RuneCountInString(kept.Delete)
	newText := string(text[:kept.Offset]) + kept.Insert + string(text[end:])
	delta := utf8.RuneCountInString(kept.Insert) - utf8.RuneCountInString(kept.Delete)
	for i := range suggestion.Edits {
		edit := &suggestion.Edits[i]
		if edit.State != "proposed" || !slices.Equal(edit.Block, path) || edit.BlockText != kept.BlockText {
			continue
		}
		edit.BlockText = newText
		if edit.Offset > kept.Offset {
			edit.Offset += delta
		}
	}
}

func saveSuggestionEdits(ctx context.Context, tx *sql.Tx, suggestion Suggestion) error {
	for _, edit := range suggestion.Edits {
		block, err := json.Marshal(edit.Block)
		if err != nil {
			return err
		}
		var decided any
		if !edit.DecidedAt.IsZero() {
			decided = edit.DecidedAt.UnixMilli()
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workstream_suggestion_edits SET block=?,at_offset=?,block_text=?,state=?,decided_at=? WHERE suggestion_id=? AND id=?`, string(block), edit.Offset, edit.BlockText, edit.State, decided, suggestion.ID, edit.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) KeepSuggestion(ctx context.Context, id, editID string) (Page, Suggestion, Change, error) {
	var page Page
	var suggestion Suggestion
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		var err error
		suggestion, page, err = suggestionDecisionIn(ctx, tx, id)
		if err != nil {
			return err
		}
		targets, err := suggestionTargets(suggestion, editID)
		if err != nil {
			return err
		}
		label := suggestion.Why
		if editID != "" {
			label = suggestion.Edits[targets[0]].Why
		}
		at := instant(s.now().UnixMilli())
		doc, err := keepSuggestionDoc(&suggestion, page, targets, at)
		if err != nil {
			return err
		}
		source, err := json.Marshal(Source{Kind: "suggestion", ID: id, Label: label})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workstream_pages SET doc=?,version=version+1,updated_at=?,updated_by='you',updated_from=? WHERE id=?`, string(doc), at.UnixMilli(), string(source), page.ID); err != nil {
			return err
		}
		if err := saveVersion(ctx, tx, page.ID); err != nil {
			return err
		}
		if err := saveSuggestionEdits(ctx, tx, suggestion); err != nil {
			return err
		}
		if err := touch(ctx, tx, suggestion.WorkstreamID, at.UnixMilli()); err != nil {
			return err
		}
		page, _, err = livePageIn(ctx, tx, page.ID)
		return err
	})
	if err != nil {
		return Page{}, Suggestion{}, Change{}, err
	}
	return page, suggestion, suggestionChanged(suggestion.WorkstreamID), nil
}

func (s *Store) DropSuggestion(ctx context.Context, id, editID string) (Suggestion, Change, error) {
	var suggestion Suggestion
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		var page Page
		var err error
		suggestion, page, err = suggestionDecisionIn(ctx, tx, id)
		if err != nil {
			return err
		}
		targets, err := suggestionTargets(suggestion, editID)
		if err != nil {
			return err
		}
		at := instant(s.now().UnixMilli())
		for _, i := range targets {
			suggestion.Edits[i].State, suggestion.Edits[i].DecidedAt = "dropped", at
		}
		if err := locateSuggestion(&suggestion, page.Doc); err != nil {
			return err
		}
		if err := saveSuggestionEdits(ctx, tx, suggestion); err != nil {
			return err
		}
		return touch(ctx, tx, suggestion.WorkstreamID, at.UnixMilli())
	})
	if err != nil {
		return Suggestion{}, Change{}, err
	}
	return suggestion, suggestionChanged(suggestion.WorkstreamID), nil
}
