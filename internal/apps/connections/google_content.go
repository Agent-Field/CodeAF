package connections

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

// GoogleContent returns protected text to the trusted host or the person's
// own approved model. Browsing and sync never fetch this text.
type GoogleContent struct {
	Text  string `json:"text"`
	Class string `json:"class"`
}

type GoogleAudience uint8

const (
	GooglePerson GoogleAudience = iota + 1
	GoogleOwnModel
)

// ReadGoogleContent is a point read with the same app and delegated account
// checks as metadata. Audience comes from the trusted caller, never the app.
func (s Service) ReadGoogleContent(ctx context.Context, q Query, id string, audience GoogleAudience) (GoogleContent, error) {
	if audience != GooglePerson && audience != GoogleOwnModel {
		return GoogleContent{}, errors.New("Google Workspace content contains personal information and cannot leave through an outside harness")
	}
	c, err := s.scoped(q.App, q.Connection)
	if err != nil {
		return GoogleContent{}, err
	}
	read, ok := c.Reads[q.Read]
	if c.Kind != Google || !ok || read.Table == "calendar" {
		return GoogleContent{}, ErrRejected
	}
	store, binding, typ, err := s.bind(c.Config, read, false)
	if err != nil {
		return GoogleContent{}, err
	}
	// The content is separate from mapped metadata, so it cannot enforce a
	// local row filter or a hidden/masked field declaration.
	sc := data.Scope{App: q.App, Person: s.person(ctx)}
	if typ.HasRowPolicy() || googleProtectedFields(typ) {
		store.AuditRecordPolicy(sc, typ, "source_read_refused")
		return GoogleContent{}, ErrSourcePermission
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	source, grant, err := s.workspace(ctx, c, read.Table, false)
	if err != nil {
		return GoogleContent{}, err
	}
	c.googleGrant = grant
	raw, err := effectKey(store, c, read, binding, id)
	if err != nil {
		return GoogleContent{}, err
	}
	key, ok := raw.(string)
	if !ok || !googleID.MatchString(key) || key == "." || key == ".." {
		return GoogleContent{}, ErrRejected
	}
	text, err := source.content(ctx, read.Table, key)
	if err != nil {
		return GoogleContent{}, err
	}
	if _, current, err := s.workspace(ctx, c, read.Table, false); err != nil || current != grant {
		return GoogleContent{}, ErrSourcePermission
	}
	current, err := s.scoped(c.App, c.ID)
	if err != nil || current.Binding != c.Binding {
		return GoogleContent{}, ErrConflict
	}
	if _, current, currentType, err := s.bind(c.Config, read, false); err != nil || current != binding || currentType != typ {
		return GoogleContent{}, ErrSourcePermission
	}
	return GoogleContent{Text: text, Class: "pii"}, nil
}

func googleProtectedFields(typ *data.Type) bool {
	for _, field := range typ.Fields {
		if field.Masked || typ.FieldPolicy(field) != "" {
			return true
		}
	}
	return false
}
