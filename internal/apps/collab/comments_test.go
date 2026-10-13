package collab

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func add(t *testing.T, s *Service, scope svc.Scope, part string, anchor bridge.CommentAnchor, forWhom bridge.CommentFor) bridge.Comment {
	t.Helper()
	comment, err := s.AddComment(ctx, scope, bridge.AddCommentParams{Doc: ref.Doc, Part: part, Anchor: anchor, Text: "start in Feb, not Jan", For: forWhom})
	if err != nil {
		t.Fatal(err)
	}
	return comment
}
func list(t *testing.T, s *Service, scope svc.Scope, part string, state bridge.CommentState) []bridge.Comment {
	t.Helper()
	result, err := s.ListComments(ctx, scope, bridge.ListCommentsParams{Doc: ref.Doc, Part: part, State: state})
	if err != nil {
		t.Fatal(err)
	}
	return result.Comments
}

func TestCommentsCRUD(t *testing.T) {
	s, c, h := setup(t)
	scope := member("mona")
	anchors := []struct {
		name   string
		anchor bridge.CommentAnchor
	}{
		{"box", bridge.CommentAnchor{Box: "role"}},
		{"range", bridge.CommentAnchor{Range: json.RawMessage(`{"from":2,"to":8}`)}},
		{"cell", bridge.CommentAnchor{Cell: "C5"}},
		{"node", bridge.CommentAnchor{Node: "hiring"}},
		{"feature", bridge.CommentAnchor{Feature: "london"}},
	}
	for _, test := range anchors {
		t.Run(test.name, func(t *testing.T) {
			comment := add(t, s, scope, ref.Part, test.anchor, bridge.CommentForAgent)
			if comment.ID == "" || comment.State != bridge.CommentStateOpen || comment.By != "mona" || comment.For != bridge.CommentForAgent || comment.At != stamp(c.Now()) {
				t.Fatalf("comment = %#v", comment)
			}
			stored, err := s.Comment(ctx, scope, bridge.SendCommentParams{ID: comment.ID})
			if err != nil || !reflect.DeepEqual(comment, stored) {
				t.Fatalf("stored = %#v, %v", stored, err)
			}
			event := h.last(t, bridge.DocEventKindComment)
			if !reflect.DeepEqual(comment, *event.Comment) || event.Part != ref.Part || event.By != "mona" {
				t.Fatalf("event = %#v", event)
			}
			resolved, err := s.ResolveComment(ctx, member("raj"), bridge.ResolveCommentParams{ID: comment.ID})
			if err != nil || resolved.State != bridge.CommentStateResolved || resolved.ResolvedBy != "raj" || resolved.At != comment.At {
				t.Fatalf("resolved = %#v, %v", resolved, err)
			}
			event = h.last(t, bridge.DocEventKindComment)
			if event.By != "raj" || event.Comment.State != bridge.CommentStateResolved {
				t.Fatalf("resolve event = %#v", event)
			}
		})
	}
	if len(s.locks) != 0 {
		t.Fatal("comments acquired a lock")
	}
	person := add(t, s, scope, "slides/7.json", bridge.CommentAnchor{Box: "risk"}, bridge.CommentForPerson)
	tests := []struct {
		name, part string
		state      bridge.CommentState
		count      int
	}{
		{"all", "", "", 6}, {"part", ref.Part, "", 5}, {"open", "", bridge.CommentStateOpen, 1},
		{"resolved", "", bridge.CommentStateResolved, 5}, {"part and state", ref.Part, bridge.CommentStateOpen, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			comments := list(t, s, scope, test.part, test.state)
			if len(comments) != test.count {
				t.Fatalf("comments = %d, want %d", len(comments), test.count)
			}
		})
	}
	if got := list(t, s, scope, "slides/7.json", bridge.CommentStateOpen); len(got) != 1 || got[0].For != bridge.CommentForPerson || got[0].ID != person.ID {
		t.Fatal("person comment missing")
	}
	other := member("mona")
	other.App = "other"
	if got := list(t, s, other, "", ""); len(got) != 0 {
		t.Fatal("comments crossed apps")
	}
	_, err := s.Comment(ctx, other, bridge.SendCommentParams{ID: person.ID})
	code(t, err, bridge.CodeNotFound)
	_, err = s.ResolveComment(ctx, other, bridge.ResolveCommentParams{ID: person.ID})
	code(t, err, bridge.CodeNotFound)
	reopened := NewWithClock(s.cfg, s.all, c)
	t.Cleanup(reopened.Close)
	if got := list(t, reopened, scope, "", ""); len(got) != 6 {
		t.Fatal("comments did not survive service restart")
	}
}

func TestCommentsSharePartsDatabaseWithoutChangingOtherTables(t *testing.T) {
	s, _, _ := setup(t)
	// This is the exact app-level file owned independently by the parts service.
	if _, err := s.ListComments(ctx, member("mona"), bridge.ListCommentsParams{Doc: ref.Doc}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(s.cfg.Root, "slides", "parts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE parts_lane_fixture (value TEXT); INSERT INTO parts_lane_fixture VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}
	add(t, s, member("mona"), ref.Part, bridge.CommentAnchor{}, bridge.CommentForPerson)
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM comments`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("shared comments count = %d, %v", count, err)
	}
	var value string
	if err := db.QueryRow(`SELECT value FROM parts_lane_fixture`).Scan(&value); err != nil || value != "kept" {
		t.Fatalf("parts data = %q, %v", value, err)
	}
}

func TestCommentsNeverChangeAnExistingLock(t *testing.T) {
	s, _, _ := setup(t)
	agent := member("mona")
	agent.Caller.Agent = true
	before := take(t, s, agent)
	comment := add(t, s, member("raj"), ref.Part, bridge.CommentAnchor{Box: "role"}, bridge.CommentForPerson)
	list(t, s, member("raj"), "", "")
	if _, err := s.ResolveComment(ctx, member("raj"), bridge.ResolveCommentParams{ID: comment.ID}); err != nil {
		t.Fatal(err)
	}
	after := snapshot(s.locks[partKey{"slides", ref.Doc, ref.Part}])
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("comments changed lock: before=%#v after=%#v", before, after)
	}
}

func TestCommentGuards(t *testing.T) {
	tests := []struct {
		name string
		call func(*Service) error
		want bridge.ErrorCode
	}{
		{"viewer add", func(s *Service) error {
			scope := member("mona")
			scope.Caller.Role = svc.RoleViewer
			_, err := s.AddComment(ctx, scope, bridge.AddCommentParams{Doc: ref.Doc, Part: ref.Part, Text: "hello", For: bridge.CommentForPerson})
			return err
		}, bridge.CodeDenied},
		{"viewer resolve", func(s *Service) error {
			scope := member("mona")
			scope.Caller.Role = svc.RoleViewer
			_, err := s.ResolveComment(ctx, scope, bridge.ResolveCommentParams{ID: "missing"})
			return err
		}, bridge.CodeDenied},
		{"missing resolve", func(s *Service) error {
			_, err := s.ResolveComment(ctx, member("mona"), bridge.ResolveCommentParams{ID: "missing"})
			return err
		}, bridge.CodeNotFound},
		{"blank text", func(s *Service) error {
			_, err := s.AddComment(ctx, member("mona"), bridge.AddCommentParams{Doc: ref.Doc, Part: ref.Part, Text: " ", For: bridge.CommentForPerson})
			return err
		}, bridge.CodeInvalid},
		{"invalid recipient", func(s *Service) error {
			_, err := s.AddComment(ctx, member("mona"), bridge.AddCommentParams{Doc: ref.Doc, Part: ref.Part, Text: "hello", For: "team"})
			return err
		}, bridge.CodeInvalid},
		{"invalid anchor JSON", func(s *Service) error {
			_, err := s.AddComment(ctx, member("mona"), bridge.AddCommentParams{Doc: ref.Doc, Part: ref.Part, Text: "hello", For: bridge.CommentForPerson, Anchor: bridge.CommentAnchor{Range: json.RawMessage("{")}})
			return err
		}, bridge.CodeInvalid},
		{"invalid state", func(s *Service) error {
			_, err := s.ListComments(ctx, member("mona"), bridge.ListCommentsParams{Doc: ref.Doc, State: "hidden"})
			return err
		}, bridge.CodeInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { s, _, _ := setup(t); code(t, test.call(s), test.want) })
	}
}
