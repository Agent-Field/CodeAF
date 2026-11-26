package connections

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

// Google relationship fields can opt into canonical references. Email and
// attachment metadata remain available as scalars when a view only needs text.
func googleReferenceColumn(object, column string) data.CommonTypeKey {
	switch {
	case object == "gmail" && column == "sender_domain":
		return data.CommonOrganization
	case object == "gmail" && column == "attachments":
		return data.CommonDocument
	case object == "gmail" && (column == "from" || column == "to" || column == "cc"),
		object == "calendar" && column == "attendees":
		return data.CommonPerson
	default:
		return ""
	}
}

func googleSourceField(read Read, typ *data.Type, name string) (*data.Field, error) {
	f := typ.Fields[name]
	if read.GoogleLinkIdentities && f != nil && (f.Type == data.TypeRef || f.Type == data.TypeRefs) &&
		!f.Hidden && !f.Masked && f.Computed == "" && googleReferenceColumn(read.Table, read.Fields[name]) != "" {
		return f, nil
	}
	return typ.LiveField(name, false)
}

func googleReferenceValue(store *data.Store, sc data.Scope, c registration, read Read, f *data.Field, raw map[string]any, link bool) (any, error) {
	column := read.Fields[f.Name]
	common := googleReferenceColumn(read.Table, column)
	target, err := store.Type(sc, f.To)
	if err != nil {
		return nil, ErrSourcePermission
	}
	if target.Common != common || target.HasRowPolicy() || googleProtectedFields(target) {
		return nil, ErrSourcePermission
	}
	var keys []data.ExternalKey
	switch common {
	case data.CommonOrganization:
		if domain := googleText(raw, column); domain != "" {
			keys = append(keys, googleIdentityKey("domain", domain))
		}
	case data.CommonPerson:
		for _, address := range strings.Split(googleText(raw, column), ", ") {
			_, domain, ok := strings.Cut(address, "@")
			if ok && domain != "" {
				keys = append(keys, googleIdentityKey("email", address))
			}
		}
	default:
		attachments, err := googleAttachments(raw)
		if err != nil {
			return nil, err
		}
		for _, attachment := range attachments {
			key, err := encodeSourceKey(attachment["id"])
			if err != nil {
				return nil, err
			}
			keys = append(keys, externalKey(c, Read{Table: "attachments", Key: "id"}, key))
		}
	}
	ids := make([]string, 0, len(keys))
	seen := make(map[data.ExternalKey]bool, len(keys))
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		id, found, err := store.LookupBoundIdentity(data.TypeBinding{TypeID: target.ID}, key)
		if err != nil {
			return nil, err
		}
		if !found {
			// Document keys are transport identities. People and companies
			// require the separate operator link step before a read can
			// project their IDs. Neither path merges into an existing person.
			if common != data.CommonDocument && !link {
				continue
			}
			if err := store.AuditPolicy(c.App, target.ID, "source_identity_linked"); err != nil {
				return nil, err
			}
			id, err = store.LinkIdentity(common, key, "")
			if err != nil {
				return nil, err
			}
		}
		ids = append(ids, id)
	}
	if f.Type == data.TypeRefs {
		return ids, nil
	}
	if len(ids) == 1 {
		return ids[0], nil
	}
	return nil, nil
}

func googleIdentityKey(kind, address string) data.ExternalKey {
	return data.ExternalKey{Binding: "workspace-identity", Object: kind,
		Key: fmt.Sprintf("%x", sha256.Sum256([]byte(address))), Generation: 1}
}

func emailDomain(address string) string {
	if strings.Contains(address, ",") {
		return ""
	}
	_, domain, _ := strings.Cut(address, "@")
	return domain
}

// LinkGoogleIdentities is the local operator path for reserving exact email
// and domain identities from the declared Google read. A lookup on an app
// screen only resolves already reserved keys.
func (s Service) LinkGoogleIdentities(ctx context.Context, q Query) error {
	c, err := s.scoped(q.App, q.Connection)
	if err != nil {
		return err
	}
	read, ok := c.Reads[q.Read]
	if c.GoogleFixture {
		return errors.New("a fixture connection cannot reserve identities; connect the live account first")
	}
	if c.Kind != Google || !ok || !read.GoogleLinkIdentities {
		return ErrSourcePermission
	}
	store, _, typ, err := s.bind(c.Config, read, false)
	if err != nil {
		return err
	}
	if typ.HasRowPolicy() {
		return ErrSourcePermission
	}
	scope := data.Scope{App: c.App, Person: s.person(ctx)}
	fields := []*data.Field{}
	for field, column := range read.Fields {
		common := googleReferenceColumn(read.Table, column)
		if common != data.CommonPerson && common != data.CommonOrganization {
			continue
		}
		f, err := googleSourceField(read, typ, field)
		if err != nil {
			return err
		}
		if (f.Type == data.TypeRef || f.Type == data.TypeRefs) && typ.FieldPolicy(f) == "" {
			fields = append(fields, f)
		}
	}
	if len(fields) == 0 {
		return ErrRejected
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	source, grant, err := s.workspace(ctx, c, read.Table, false)
	if err != nil {
		return err
	}
	c.googleSubject = source.subject
	args := map[string]any{}
	if read.GoogleQuery != "" {
		args["q"] = read.GoogleQuery
	}
	limit := read.GoogleLimit
	if limit == 0 {
		limit = 50
	}
	rows, _, err := source.rows(ctx, read.Table, args, limit)
	if err != nil {
		return err
	}
	if _, current, err := s.workspace(ctx, c, read.Table, false); err != nil || current != grant {
		return ErrSourcePermission
	}
	current, err := s.scoped(q.App, q.Connection)
	if err != nil || current.Binding != c.Binding {
		return ErrConflict
	}
	if _, _, currentType, err := s.bind(c.Config, read, false); err != nil || currentType != typ {
		return ErrSourcePermission
	}
	for _, row := range rows {
		for _, field := range fields {
			if _, err := googleReferenceValue(store, scope, c, read, field, row, true); err != nil {
				return err
			}
		}
	}
	return nil
}
