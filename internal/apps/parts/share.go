package parts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"strings"
	"unicode"
)

func (c *session) share(ctx context.Context, doc string) (*bridge.Share, error) {
	if c.scope.Data == svc.DataDraft {
		return nil, nil
	}
	share := bridge.Share{Doc: doc}
	err := c.tx.QueryRowContext(ctx, `SELECT slug,"on",by,at FROM share_links WHERE data=? AND doc=?`, mode(c.scope), doc).Scan(&share.Slug, &share.On, &share.By, &share.At)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	share.URL = "codeaf://d/" + share.Slug
	return &share, nil
}
func slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	if b.Len() == 0 {
		return "document"
	}
	return b.String()
}
func (c *session) newSlug(ctx context.Context, doc, title string) (string, error) {
	base := slug(title)
	var exists bool
	err := c.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM share_links WHERE data=? AND slug=?)`, mode(c.scope), base).Scan(&exists)
	if err != nil || !exists {
		return base, err
	}
	hash := sha256.Sum256([]byte(doc))
	for n := 1; ; n++ {
		candidate := fmt.Sprintf("%s-%x-%d", base, hash[:4], n)
		err = c.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM share_links WHERE data=? AND slug=?)`, mode(c.scope), candidate).Scan(&exists)
		if err != nil || !exists {
			return candidate, err
		}
	}
}
func (s *Service) SetShare(ctx context.Context, scope svc.Scope, p bridge.SetShareParams) (bridge.Share, error) {
	if scope.Data == svc.DataDraft {
		return bridge.Share{}, conflict("Share works on the applied app")
	}
	c, err := s.open(ctx, scope)
	if err != nil {
		return bridge.Share{}, err
	}
	defer c.close()
	d, err := c.readDoc(p.Doc)
	if err != nil {
		return bridge.Share{}, err
	}
	old, err := c.share(ctx, p.Doc)
	if err != nil {
		return bridge.Share{}, err
	}
	name := ""
	if old != nil {
		name = old.Slug
	} else {
		name, err = c.newSlug(ctx, p.Doc, d.Title)
		if err != nil {
			return bridge.Share{}, err
		}
	}
	result := bridge.Share{Doc: p.Doc, Slug: name, URL: "codeaf://d/" + name, On: p.On, By: scope.Caller.Member, At: timestamp()}
	_, err = c.tx.ExecContext(ctx, `INSERT INTO share_links(data,doc,slug,"on",by,at) VALUES(?,?,?,?,?,?) ON CONFLICT(data,doc) DO UPDATE SET "on"=excluded."on",by=excluded.by,at=excluded.at`, mode(scope), p.Doc, name, p.On, result.By, result.At)
	if err != nil {
		return result, err
	}
	c.event(p.Doc, bridge.DocEvent{Kind: bridge.DocEventKindShare, Share: &result})
	return result, c.commit()
}
func (s *Service) OpenShare(ctx context.Context, scope svc.Scope, p bridge.OpenShareParams) (bridge.DocInfo, error) {
	if scope.Data == svc.DataDraft {
		return bridge.DocInfo{}, conflict("Share works on the applied app")
	}
	c, err := s.open(ctx, scope)
	if err != nil {
		return bridge.DocInfo{}, err
	}
	defer c.close()
	var doc string
	err = c.tx.QueryRowContext(ctx, `SELECT doc FROM share_links WHERE data=? AND slug=? AND "on"=1`, mode(scope), p.Slug).Scan(&doc)
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.DocInfo{}, missing("share link not found")
	}
	if err != nil {
		return bridge.DocInfo{}, err
	}
	return c.info(ctx, doc)
}
