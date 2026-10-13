package collab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const commentColumns = `id, doc, part, anchor, text, "by", "for", state, at, resolved_by`
const commentSchema = `CREATE TABLE IF NOT EXISTS comments (
 id TEXT PRIMARY KEY, doc TEXT NOT NULL, part TEXT NOT NULL,
 anchor TEXT NOT NULL, text TEXT NOT NULL, "by" TEXT NOT NULL,
 "for" TEXT NOT NULL, state TEXT NOT NULL, at TEXT NOT NULL,
 resolved_by TEXT NOT NULL DEFAULT ''
)`

// Each operation opens its own handle so the parts lane keeps its connection lifecycle.
func (s *Service) database(ctx context.Context, scope svc.Scope) (*sql.DB, error) {
	if !relative(scope.App) || strings.Contains(scope.App, "/") || s.cfg.Root == "" {
		return nil, bridge.Errorf(bridge.CodeInvalid, "app and apps root are required")
	}
	dir := filepath.Join(s.cfg.Root, scope.App)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	location := url.URL{Scheme: "file", Path: filepath.Join(dir, "parts.db")}
	query := location.Query()
	query.Add("_pragma", "busy_timeout(5000)")
	location.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", location.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, commentSchema); err == nil {
		_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS comments_doc_part_state ON comments(doc, part, state)`)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Service) AddComment(ctx context.Context, scope svc.Scope, p bridge.AddCommentParams) (bridge.Comment, error) {
	if err := s.validate(ctx, scope, p.Doc, p.Part, true); err != nil {
		return bridge.Comment{}, err
	}
	if err := writable(scope); err != nil {
		return bridge.Comment{}, err
	}
	if strings.TrimSpace(p.Text) == "" || (p.For != bridge.CommentForAgent && p.For != bridge.CommentForPerson) {
		return bridge.Comment{}, bridge.Errorf(bridge.CodeInvalid, "comment text and agent or person recipient are required")
	}
	anchor, err := json.Marshal(p.Anchor)
	if err != nil {
		return bridge.Comment{}, bridge.Errorf(bridge.CodeInvalid, "comment anchor must be JSON")
	}
	db, err := s.database(ctx, scope)
	if err != nil {
		return bridge.Comment{}, err
	}
	defer db.Close()
	comment := bridge.Comment{ID: uuid.NewString(), Doc: p.Doc, Part: p.Part, Anchor: p.Anchor, Text: p.Text, By: holder(scope.Caller), For: p.For, State: bridge.CommentStateOpen, At: stamp(s.clock.Now())}
	_, err = db.ExecContext(ctx, `INSERT INTO comments (`+commentColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, comment.ID, comment.Doc, comment.Part, string(anchor), comment.Text, comment.By, comment.For, comment.State, comment.At, comment.ResolvedBy)
	if err != nil {
		return bridge.Comment{}, err
	}
	// Own the anchor bytes so a caller cannot mutate a published event after returning.
	comment.Anchor = bridge.CommentAnchor{}
	if err := json.Unmarshal(anchor, &comment.Anchor); err != nil {
		return bridge.Comment{}, err
	}
	s.publishComment(scope.App, comment, comment.By)
	return comment, nil
}

func (s *Service) ListComments(ctx context.Context, scope svc.Scope, p bridge.ListCommentsParams) (bridge.ListCommentsResult, error) {
	result := bridge.ListCommentsResult{Comments: []bridge.Comment{}}
	if err := s.validate(ctx, scope, p.Doc, p.Part, p.Part != ""); err != nil {
		return result, err
	}
	if p.State != "" && p.State != bridge.CommentStateOpen && p.State != bridge.CommentStateResolved && p.State != bridge.CommentStateSent {
		return result, bridge.Errorf(bridge.CodeInvalid, "invalid comment state")
	}
	db, err := s.database(ctx, scope)
	if err != nil {
		return result, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT `+commentColumns+` FROM comments WHERE doc = ? AND (? = '' OR part = ?) AND (? = '' OR state = ?) ORDER BY at, rowid`, p.Doc, p.Part, p.Part, p.State, p.State)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return result, err
		}
		result.Comments = append(result.Comments, comment)
	}
	return result, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanComment(row scanner) (bridge.Comment, error) {
	var comment bridge.Comment
	var anchor string
	err := row.Scan(&comment.ID, &comment.Doc, &comment.Part, &anchor, &comment.Text, &comment.By, &comment.For, &comment.State, &comment.At, &comment.ResolvedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return comment, bridge.Errorf(bridge.CodeNotFound, "comment not found")
	}
	if err != nil {
		return comment, err
	}
	err = json.Unmarshal([]byte(anchor), &comment.Anchor)
	return comment, err
}

// Comment reads the note for comments.send; starting an Ask belongs to the gateway.
func (s *Service) Comment(ctx context.Context, scope svc.Scope, p bridge.SendCommentParams) (bridge.Comment, error) {
	if err := s.commentScope(ctx, scope, p.ID); err != nil {
		return bridge.Comment{}, err
	}
	db, err := s.database(ctx, scope)
	if err != nil {
		return bridge.Comment{}, err
	}
	defer db.Close()
	return scanComment(db.QueryRowContext(ctx, `SELECT `+commentColumns+` FROM comments WHERE id = ?`, p.ID))
}

func (s *Service) ResolveComment(ctx context.Context, scope svc.Scope, p bridge.ResolveCommentParams) (bridge.Comment, error) {
	if err := s.commentScope(ctx, scope, p.ID); err != nil {
		return bridge.Comment{}, err
	}
	if err := writable(scope); err != nil {
		return bridge.Comment{}, err
	}
	db, err := s.database(ctx, scope)
	if err != nil {
		return bridge.Comment{}, err
	}
	defer db.Close()
	comment, err := scanComment(db.QueryRowContext(ctx, `UPDATE comments SET state = ?, resolved_by = ? WHERE id = ? RETURNING `+commentColumns, bridge.CommentStateResolved, holder(scope.Caller), p.ID))
	if err != nil {
		return bridge.Comment{}, err
	}
	s.publishComment(scope.App, comment, holder(scope.Caller))
	return comment, nil
}
func (s *Service) commentScope(ctx context.Context, scope svc.Scope, id string) error {
	if id == "" {
		return bridge.Errorf(bridge.CodeInvalid, "comment id is required")
	}
	return s.validate(ctx, scope, "comments", "", false)
}
func (s *Service) publishComment(app string, comment bridge.Comment, by string) {
	comment.Anchor.Range = append(json.RawMessage(nil), comment.Anchor.Range...)
	s.publish(app, comment.Doc, bridge.DocEvent{Kind: bridge.DocEventKindComment, Part: comment.Part, Comment: &comment, By: by})
}
