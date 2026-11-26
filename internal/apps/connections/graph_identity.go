package connections

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

func workspaceIdentityKey(kind, value string) data.ExternalKey {
	return data.ExternalKey{Binding: "workspace-identity", Object: kind,
		Key: fmt.Sprintf("%x", sha256.Sum256([]byte(value))), Generation: 1}
}

func graphIdentity(read Read, raw map[string]any) data.ExternalKey {
	if read.Table == "person" {
		if email, _ := raw["email"].(string); email != "" {
			return workspaceIdentityKey("email", email)
		}
	} else if read.Table == "company" {
		if domain, _ := raw["domain"].(string); domain != "" {
			return workspaceIdentityKey("domain", domain)
		}
	}
	return data.ExternalKey{}
}

func graphPersonColumn(read Read, column string) bool {
	if read.Table == "outlook" {
		return column == "from" || column == "to" || column == "cc"
	}
	return read.Table == "calendar" && (column == "attendees" || column == "organizer")
}

func graphReference(store *data.Store, sc data.Scope, field *data.Field, value any) (any, error) {
	binding, target, err := store.LiveBinding(sc, field.To, false)
	if err != nil || binding.Common != data.CommonPerson || target.HasRowPolicy() {
		return nil, ErrSourcePermission
	}
	addresses, _ := value.(string)
	var ids []string
	seen := map[string]bool{}
	for _, value := range strings.Split(addresses, ", ") {
		address := normalizedEmail(value)
		if address == "" || seen[address] {
			continue
		}
		seen[address] = true
		id, err := store.LinkIdentity(data.CommonPerson, workspaceIdentityKey("email", address), "")
		if err != nil {
			return nil, ErrInvalidRows
		}
		ids = append(ids, id)
	}
	if field.Type == data.TypeRefs {
		return ids, nil
	}
	if len(ids) == 1 {
		return ids[0], nil
	}
	return nil, nil
}
