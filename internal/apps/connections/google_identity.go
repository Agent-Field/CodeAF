package connections

import (
	"crypto/sha256"
	"encoding/json"
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
	if f != nil && (f.Type == data.TypeRef || f.Type == data.TypeRefs) &&
		!f.Hidden && !f.Masked && f.Computed == "" && googleReferenceColumn(read.Table, read.Fields[name]) != "" {
		return f, nil
	}
	return typ.LiveField(name, false)
}

func googleReferenceValue(store *data.Store, sc data.Scope, c registration, read Read, f *data.Field, raw map[string]any) (any, error) {
	column := read.Fields[f.Name]
	common := googleReferenceColumn(read.Table, column)
	_, target, err := store.LiveBinding(sc, f.To, false)
	if err != nil || target.Common != common || target.HasRowPolicy() || googleProtectedFields(target) {
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
		var attachments []struct{ Part string }
		if json.Unmarshal([]byte(googleText(raw, column)), &attachments) != nil {
			return nil, ErrInvalidRows
		}
		for _, attachment := range attachments {
			if attachment.Part != "" {
				keys = append(keys, externalKey(c, read, "attachment:"+googleText(raw, "id")+":"+attachment.Part))
			}
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
