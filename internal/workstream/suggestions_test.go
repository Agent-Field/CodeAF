package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workstream/pagedoc"
)

type workstreamSuggestionFixture struct {
	Before json.RawMessage  `json:"doc_before"`
	After  json.RawMessage  `json:"doc_after"`
	OnlyE3 json.RawMessage  `json:"doc_after_e3"`
	Edits  []SuggestionFind `json:"edits"`
}

func workstreamSuggestionSeed(t *testing.T, s *Store) (Page, SuggestionCreate, workstreamSuggestionFixture) {
	t.Helper()
	raw, err := os.ReadFile("pagedoc/testdata/suggestion-keep.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture workstreamSuggestionFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	w := createStream(t, s, "Suggestions")
	page, _, err := s.CreatePage(context.Background(), PageCreate{WorkstreamID: w.ID, Title: "Signing", Doc: fixture.Before})
	if err != nil {
		t.Fatal(err)
	}
	ask := SuggestionCreate{WorkstreamID: w.ID, PageID: page.ID, BaseVersion: page.Version, Why: "Explain signing and retries", Edits: fixture.Edits, Attribution: Attribution{By: "agent", From: &Source{Kind: "chat", ID: w.AgentID}}}
	return page, ask, fixture
}

func workstreamSuggestionDocEqual(t *testing.T, got, want json.RawMessage) {
	t.Helper()
	a, err := pagedoc.Decode(got)
	if err != nil {
		t.Fatal(err)
	}
	b, err := pagedoc.Decode(want)
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("document differs: %s; want %s; %v", got, want, err)
	}
}

func workstreamSuggestionCreate(t *testing.T, s *Store, ask SuggestionCreate) Suggestion {
	t.Helper()
	suggestion, change, err := s.CreateSuggestion(context.Background(), ask)
	if err != nil || !reflect.DeepEqual(change, suggestionChanged(ask.WorkstreamID)) {
		t.Fatalf("create: %+v %v", change, err)
	}
	return suggestion
}

// WP-6d (b): a single keep makes exactly one attributed version; drop leaves
// every page field and history untouched and decides only its selected edit.
func TestWorkstreamSuggestionKeepOneAndDropOne(t *testing.T) {
	s, _, _ := testStore(t)
	page, ask, fixture := workstreamSuggestionSeed(t, s)
	suggestion := workstreamSuggestionCreate(t, s, ask)
	ctx := context.Background()
	historyBefore, err := s.PageHistory(ctx, page.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	kept, after, change, err := s.KeepSuggestion(ctx, suggestion.ID, "e3")
	if err != nil || kept.Version != page.Version+1 || kept.UpdatedBy != "you" || !reflect.DeepEqual(change, suggestionChanged(page.WorkstreamID)) {
		t.Fatalf("keep: %+v %+v %v", kept, change, err)
	}
	workstreamSuggestionDocEqual(t, kept.Doc, fixture.OnlyE3)
	history, err := s.PageHistory(ctx, page.ID, 50)
	wantSource := &Source{Kind: "suggestion", ID: suggestion.ID, Label: ask.Edits[2].Why}
	if err != nil || len(history) != len(historyBefore)+1 || !reflect.DeepEqual(history[1:], historyBefore) || !reflect.DeepEqual(history[0].From, wantSource) || !reflect.DeepEqual(kept.UpdatedFrom, wantSource) || history[0].SavedBy != "you" {
		t.Fatalf("history: %+v %v", history, err)
	}
	for i, edit := range after.Edits {
		if i == 2 {
			if edit.State != "kept" || edit.DecidedAt.IsZero() || edit.Applies != nil {
				t.Fatalf("kept edit: %+v", edit)
			}
		} else if edit.State != "proposed" || edit.Applies == nil || !*edit.Applies || !edit.DecidedAt.IsZero() {
			t.Fatalf("proposed edit: %+v", edit)
		}
	}
	dropped, change, err := s.DropSuggestion(ctx, suggestion.ID, "e1")
	if err != nil || dropped.Edits[0].State != "dropped" || dropped.Edits[0].DecidedAt.IsZero() || dropped.Edits[0].Applies != nil || dropped.Edits[1].State != "proposed" || dropped.Edits[2].State != "kept" || !reflect.DeepEqual(change, suggestionChanged(page.WorkstreamID)) {
		t.Fatalf("drop: %+v %v", dropped, err)
	}
	unchanged, err := s.Page(ctx, page.ID, false)
	if err != nil || !reflect.DeepEqual(unchanged, kept) {
		t.Fatal("drop changed page", err)
	}
	historyAfter, err := s.PageHistory(ctx, page.ID, 50)
	if err != nil || !reflect.DeepEqual(historyAfter, history) {
		t.Fatal("drop changed history", err)
	}
}

// WP-6d (c): keep all saves one version and closes the suggestion; drop all
// closes it without touching the page, history or updated_at.
func TestWorkstreamSuggestionKeepAllAndDropAll(t *testing.T) {
	for _, keep := range []bool{true, false} {
		t.Run(map[bool]string{true: "keep", false: "drop"}[keep], func(t *testing.T) {
			s, _, _ := testStore(t)
			page, ask, fixture := workstreamSuggestionSeed(t, s)
			suggestion := workstreamSuggestionCreate(t, s, ask)
			ctx := context.Background()
			before, err := s.PageHistory(ctx, page.ID, 50)
			if err != nil {
				t.Fatal(err)
			}
			var decided Suggestion
			if keep {
				var saved Page
				saved, decided, _, err = s.KeepSuggestion(ctx, suggestion.ID, "")
				if err != nil {
					t.Fatal(err)
				}
				workstreamSuggestionDocEqual(t, saved.Doc, fixture.After)
				want := &Source{Kind: "suggestion", ID: suggestion.ID, Label: ask.Why}
				if saved.Version != page.Version+1 || saved.UpdatedBy != "you" || !reflect.DeepEqual(saved.UpdatedFrom, want) {
					t.Fatalf("keep all: %+v", saved)
				}
			} else {
				decided, _, err = s.DropSuggestion(ctx, suggestion.ID, "")
				if err != nil {
					t.Fatal(err)
				}
				saved, err := s.Page(ctx, page.ID, false)
				if err != nil || !reflect.DeepEqual(saved, page) {
					t.Fatal("drop all changed page", err)
				}
			}
			for _, edit := range decided.Edits {
				want := "dropped"
				if keep {
					want = "kept"
				}
				if edit.State != want || edit.DecidedAt.IsZero() || edit.Applies != nil {
					t.Fatalf("decision: %+v", edit)
				}
			}
			after, err := s.PageHistory(ctx, page.ID, 50)
			if err != nil {
				t.Fatal(err)
			}
			if keep {
				if len(after) != len(before)+1 || after[0].From.Label != ask.Why {
					t.Fatalf("keep all history: %+v", after)
				}
			} else if !reflect.DeepEqual(before, after) {
				t.Fatal("drop all changed history")
			}
			open, err := s.Suggestions(ctx, page.ID)
			if err != nil || open == nil || len(open) != 0 {
				t.Fatalf("closed still open: %+v %v", open, err)
			}
			for _, editID := range []string{"e1", ""} {
				if _, _, c, err := s.KeepSuggestion(ctx, suggestion.ID, editID); !errors.Is(err, ErrConflict) || len(c.Parts) != 0 {
					t.Fatalf("repeat keep: %v", err)
				}
				if _, c, err := s.DropSuggestion(ctx, suggestion.ID, editID); !errors.Is(err, ErrConflict) || len(c.Parts) != 0 {
					t.Fatalf("repeat drop: %v", err)
				}
			}
		})
	}
}

// WP-6d (c): keeping either end of one block rebases the remaining anchor.
func TestWorkstreamSuggestionEditsMoveWithText(t *testing.T) {
	for _, first := range []string{"e1", "e2", ""} {
		t.Run("first="+first, func(t *testing.T) {
			s, _, _ := testStore(t)
			page, ask, _ := workstreamSuggestionSeed(t, s)
			doc := pagedoc.FromMarkdown("Alpha old words and omega old words.")
			page, _, err := s.SavePage(context.Background(), page.ID, page.Version, doc, Attribution{})
			if err != nil {
				t.Fatal(err)
			}
			ask.BaseVersion = page.Version
			ask.Edits = []SuggestionFind{{Find: "Alpha old", Replace: "Alpha several new", Why: "Earlier"}, {Find: "omega old", Replace: "omega fresh", Why: "Later"}}
			suggestion := workstreamSuggestionCreate(t, s, ask)
			saved, after, _, err := s.KeepSuggestion(context.Background(), suggestion.ID, first)
			if err != nil {
				t.Fatal(err)
			}
			if first != "" {
				remaining := "e2"
				if first == "e2" {
					remaining = "e1"
				}
				for _, edit := range after.Edits {
					if edit.State == "proposed" && (edit.Applies == nil || !*edit.Applies) {
						t.Fatalf("lost anchor: %+v", edit)
					}
				}
				saved, _, _, err = s.KeepSuggestion(context.Background(), suggestion.ID, remaining)
				if err != nil {
					t.Fatal(err)
				}
			}
			workstreamSuggestionDocEqual(t, saved.Doc, pagedoc.FromMarkdown("Alpha several new words and omega fresh words."))
		})
	}
}

// WP-6d (d): stale single/all keeps are atomic, carry page details, and expose
// applies false. Moving an intact paragraph reports and persists its new path.
func TestWorkstreamSuggestionStaleAndMovedBlocks(t *testing.T) {
	for _, moved := range []bool{false, true} {
		t.Run(map[bool]string{true: "moved", false: "stale"}[moved], func(t *testing.T) {
			s, _, _ := testStore(t)
			page, ask, fixture := workstreamSuggestionSeed(t, s)
			suggestion := workstreamSuggestionCreate(t, s, ask)
			root, err := pagedoc.Decode(fixture.Before)
			if err != nil {
				t.Fatal(err)
			}
			content := root["content"].([]any)
			if moved {
				root["content"] = append([]any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Inserted above"}}}}, content...)
			} else {
				content[1].(map[string]any)["content"] = []any{map[string]any{"type": "text", "text": "Person changed the paragraph"}}
			}
			doc, err := json.Marshal(root)
			if err != nil {
				t.Fatal(err)
			}
			page, _, err = s.SavePage(context.Background(), page.ID, page.Version, doc, Attribution{})
			if err != nil {
				t.Fatal(err)
			}
			open, err := s.Suggestions(context.Background(), page.ID)
			if err != nil || len(open) != 1 {
				t.Fatal("list", err)
			}
			if moved {
				if !*open[0].Edits[0].Applies || !reflect.DeepEqual(open[0].Edits[0].Block, []int{1}) {
					t.Fatalf("path did not move: %+v", open)
				}
				_, after, _, err := s.KeepSuggestion(context.Background(), suggestion.ID, "e1")
				if err != nil || !reflect.DeepEqual(after.Edits[0].Block, []int{1}) || !reflect.DeepEqual(after.Edits[1].Block, []int{2}) {
					t.Fatalf("located paths not saved: %+v %v", after, err)
				}
				return
			}
			if !*open[0].Edits[0].Applies || *open[0].Edits[1].Applies {
				t.Fatalf("applies: %+v", open)
			}
			history, err := s.PageHistory(context.Background(), page.ID, 50)
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{"e2", ""} {
				_, _, change, err := s.KeepSuggestion(context.Background(), suggestion.ID, target)
				var stale *VersionConflict
				if !errors.As(err, &stale) || stale.ID != page.ID || stale.Version != page.Version || !stale.UpdatedAt.Equal(page.UpdatedAt) || stale.UpdatedBy != page.UpdatedBy || len(change.Parts) != 0 {
					t.Fatalf("stale: %+v %v", stale, err)
				}
				after, _ := s.Page(context.Background(), page.ID, false)
				afterHistory, _ := s.PageHistory(context.Background(), page.ID, 50)
				afterOpen, _ := s.Suggestions(context.Background(), page.ID)
				if !reflect.DeepEqual(after, page) || !reflect.DeepEqual(afterHistory, history) || !reflect.DeepEqual(afterOpen, open) {
					t.Fatal("refused keep changed state")
				}
			}
		})
	}
}

// WP-6d (a), (h), Tool: ownership, actor, version, done, missing targets,
// deletion and the all-states suggestion cap refuse without a receipt.
func TestWorkstreamSuggestionRefusals(t *testing.T) {
	s, workspace, _ := testStore(t)
	page, ask, _ := workstreamSuggestionSeed(t, s)
	ctx := context.Background()
	owner := "agent"
	if _, _, err := s.UpdatePage(ctx, page.ID, PagePatch{Owner: &owner}); err != nil {
		t.Fatal(err)
	}
	if _, c, err := s.CreateSuggestion(ctx, ask); !errors.Is(err, ErrNotPersonPage) || len(c.Parts) != 0 {
		t.Fatalf("own page: %v", err)
	}
	open, err := s.Suggestions(ctx, page.ID)
	if err != nil || len(open) != 0 {
		t.Fatal("ownership refusal stored suggestion", err)
	}
	owner = "you"
	if _, _, err := s.UpdatePage(ctx, page.ID, PagePatch{Owner: &owner}); err != nil {
		t.Fatal(err)
	}
	stale := ask
	stale.BaseVersion++
	if _, c, err := s.CreateSuggestion(ctx, stale); err == nil || len(c.Parts) != 0 {
		t.Fatal("accepted stale create")
	} else {
		var conflict *VersionConflict
		if !errors.As(err, &conflict) || conflict.ID != page.ID || conflict.Version != page.Version {
			t.Fatalf("create conflict: %v", err)
		}
	}
	person := ask
	person.By = "you"
	if _, _, err := s.CreateSuggestion(ctx, person); !errors.Is(err, ErrInvalid) {
		t.Fatal("person created suggestion", err)
	}
	suggestion := workstreamSuggestionCreate(t, s, ask)
	for _, ids := range [][2]string{{"missing", "e1"}, {suggestion.ID, "missing"}} {
		if _, _, _, err := s.KeepSuggestion(ctx, ids[0], ids[1]); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing keep: %v", err)
		}
		if _, _, err := s.DropSuggestion(ctx, ids[0], ids[1]); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing drop: %v", err)
		}
	}
	if err := workspace.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO workstream_suggestions(id,workstream_id,page_id,base_version,why,actor,created_at) SELECT 'cap-'||x,?,?,1,'Cap','agent',1 FROM n`, SuggestionsMax-1, page.WorkstreamID, page.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, c, err := s.CreateSuggestion(ctx, ask); !errors.Is(err, ErrTooLarge) || len(c.Parts) != 0 {
		t.Fatalf("cap: %v", err)
	}
	if _, _, err := s.MarkDone(ctx, page.WorkstreamID, ""); err != nil {
		t.Fatal(err)
	}
	if _, c, err := s.CreateSuggestion(ctx, ask); !errors.Is(err, ErrDone) || len(c.Parts) != 0 {
		t.Fatalf("done create: %v", err)
	}
	before, _ := s.Suggestions(ctx, page.ID)
	beforePage, _ := s.Page(ctx, page.ID, false)
	beforeHistory, _ := s.PageHistory(ctx, page.ID, 50)
	for _, target := range []string{"e1", ""} {
		if _, _, c, err := s.KeepSuggestion(ctx, suggestion.ID, target); !errors.Is(err, ErrConflict) || len(c.Parts) != 0 {
			t.Fatalf("done keep: %v", err)
		}
		if _, c, err := s.DropSuggestion(ctx, suggestion.ID, target); !errors.Is(err, ErrConflict) || len(c.Parts) != 0 {
			t.Fatalf("done drop: %v", err)
		}
	}
	after, err := s.Suggestions(ctx, page.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("done decisions changed state", err)
	}
	afterPage, _ := s.Page(ctx, page.ID, false)
	afterHistory, _ := s.PageHistory(ctx, page.ID, 50)
	if !reflect.DeepEqual(beforePage, afterPage) || !reflect.DeepEqual(beforeHistory, afterHistory) {
		t.Fatal("done decisions changed page or history")
	}
	if _, _, err := s.DeletePage(ctx, page.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Suggestions(ctx, page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("listed deleted page", err)
	}
	if _, _, _, err := s.KeepSuggestion(ctx, suggestion.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("kept deleted page", err)
	}
	if _, _, err := s.DropSuggestion(ctx, suggestion.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("dropped deleted page", err)
	}
}

// WP-6d Tool: exact resolution refusals, overlapping occurrences, closed
// touching ranges, cross-block quotes, and code-block line-break permission.
func TestWorkstreamSuggestionTextResolution(t *testing.T) {
	for _, test := range []struct {
		name, markdown string
		edits          []SuggestionFind
		refusal        string
	}{
		{"missing", "One paragraph.", []SuggestionFind{{Find: "absent", Replace: "new", Why: "Reason"}}, "Edit 1: that text is not on the page. Quote plain text from one paragraph, heading, list item or code block exactly as it reads, without markdown marks."},
		{"ambiguous", "Same Same", []SuggestionFind{{Find: "Same", Replace: "new", Why: "Reason"}}, "Edit 1: that text appears more than once. Quote more of it so it is unique."},
		{"overlap", "aaa", []SuggestionFind{{Find: "aa", Replace: "new", Why: "Reason"}}, "Edit 1: that text appears more than once. Quote more of it so it is unique."},
		{"unchanged", "Same", []SuggestionFind{{Find: "Same", Replace: "Same", Why: "Reason"}}, "Edit 1: replace is the same as find."},
		{"newline", "One", []SuggestionFind{{Find: "One", Replace: "Two\nThree", Why: "Reason"}}, "Edit 1: only a code block's text may contain a line break."},
		{"touch", "AB", []SuggestionFind{{Find: "A", Replace: "X", Why: "Reason"}, {Find: "B", Replace: "Y", Why: "Reason"}}, "Edits 1 and 2 touch the same text. Merge them into one edit."},
		{"spans blocks", "One\n\nTwo", []SuggestionFind{{Find: "OneTwo", Replace: "new", Why: "Reason"}}, "Edit 1: that text is not on the page. Quote plain text from one paragraph, heading, list item or code block exactly as it reads, without markdown marks."},
		{"code", "```\nOne\n```", []SuggestionFind{{Find: "One", Replace: "Two\nThree", Why: "Reason"}}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _, _ := testStore(t)
			page, ask, _ := workstreamSuggestionSeed(t, s)
			page, _, err := s.SavePage(context.Background(), page.ID, page.Version, pagedoc.FromMarkdown(test.markdown), Attribution{})
			if err != nil {
				t.Fatal(err)
			}
			ask.BaseVersion, ask.Edits = page.Version, test.edits
			_, c, err := s.CreateSuggestion(context.Background(), ask)
			if test.refusal == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || err.Error() != test.refusal || len(c.Parts) != 0 {
				t.Fatalf("refusal: %v", err)
			}
			open, err := s.Suggestions(context.Background(), page.ID)
			if err != nil || len(open) != 0 {
				t.Fatal("refused text stored edits", err)
			}
		})
	}
}

// WP-6d (b), (c): a keep exceeding 1 MiB rolls back its version and edits.
func TestWorkstreamSuggestionKeepTooLarge(t *testing.T) {
	s, _, _ := testStore(t)
	page, ask, _ := workstreamSuggestionSeed(t, s)
	doc := pagedoc.FromMarkdown("Target " + strings.Repeat("x", DocMax-300))
	page, _, err := s.SavePage(context.Background(), page.ID, page.Version, doc, Attribution{})
	if err != nil {
		t.Fatal(err)
	}
	ask.BaseVersion = page.Version
	ask.Edits = []SuggestionFind{{Find: "Target", Replace: strings.Repeat("y", SuggestionTextMax), Why: "Reason"}}
	suggestion := workstreamSuggestionCreate(t, s, ask)
	_, _, c, err := s.KeepSuggestion(context.Background(), suggestion.ID, "")
	if !errors.Is(err, ErrTooLarge) || len(c.Parts) != 0 {
		t.Fatalf("oversize keep: %v", err)
	}
	after, _ := s.Page(context.Background(), page.ID, false)
	open, _ := s.Suggestions(context.Background(), page.ID)
	history, _ := s.PageHistory(context.Background(), page.ID, 50)
	if !reflect.DeepEqual(page, after) || len(history) != page.Version || len(open) != 1 || open[0].Edits[0].State != "proposed" {
		t.Fatal("oversize keep changed state")
	}
}

// WP-6d Schema: suggestion.list on a schema-3 page answers an initialized empty
// list without creating the new tables or updating user_version.
func TestWorkstreamSuggestionsReadSchemaThreeWithoutWriting(t *testing.T) {
	s, workspace, _ := testStore(t)
	page, _, _ := workstreamSuggestionSeed(t, s)
	ctx := context.Background()
	if err := workspace.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("DROP TABLE workstream_suggestion_edits; DROP TABLE workstream_suggestions; PRAGMA user_version=3")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	open, err := s.Suggestions(ctx, page.ID)
	if err != nil || open == nil || len(open) != 0 {
		t.Fatalf("schema 3 list: %+v %v", open, err)
	}
	if err := workspace.Read(ctx, func(tx *sql.Tx, version int) error {
		if version != 3 {
			t.Fatalf("read migrated to %d", version)
		}
		var count int
		if err := tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('workstream_suggestions','workstream_suggestion_edits')").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatal("read built suggestion tables")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// WP-6d (b), (c), Tool: open suggestions retain every edit state, suggestions
// are oldest first and edit ids follow document order rather than call order.
func TestWorkstreamSuggestionsOrderAndEveryEditState(t *testing.T) {
	s, _, now := testStore(t)
	page, ask, _ := workstreamSuggestionSeed(t, s)
	ask.Edits = []SuggestionFind{ask.Edits[2], ask.Edits[0], ask.Edits[1]}
	first := workstreamSuggestionCreate(t, s, ask)
	if first.Edits[0].Why != ask.Edits[1].Why || first.Edits[1].Why != ask.Edits[2].Why || first.Edits[2].Why != ask.Edits[0].Why {
		t.Fatalf("not document order: %+v", first.Edits)
	}
	*now = now.Add(1_000_000)
	second := workstreamSuggestionCreate(t, s, ask)
	ctx := context.Background()
	if _, _, _, err := s.KeepSuggestion(ctx, first.ID, "e1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.DropSuggestion(ctx, first.ID, "e2"); err != nil {
		t.Fatal(err)
	}
	open, err := s.Suggestions(ctx, page.ID)
	if err != nil || len(open) != 2 || open[0].ID != first.ID || open[1].ID != second.ID {
		t.Fatalf("list order: %+v %v", open, err)
	}
	if len(open[0].Edits) != 3 || open[0].Edits[0].State != "kept" || open[0].Edits[1].State != "dropped" || open[0].Edits[2].State != "proposed" {
		t.Fatalf("list lost decided edits: %+v", open[0])
	}
}
