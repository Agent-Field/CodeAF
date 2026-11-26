package connections

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

type mailIndex struct {
	Authority string
	Cursor    string
	Rows      map[string]map[string]any
}

func (g googleSource) loadMailIndex(ctx context.Context, store *data.Store, app, connection, read, authority string) (mailIndex, string, string, error) {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(app+"/"+connection+"/"+read)))
	raw, digest, err := store.SourceIndex(key)
	var index mailIndex
	if err != nil {
		return index, key, digest, err
	}
	if len(raw) > 0 && decodeJSON(raw, &index) != nil {
		return index, key, digest, ErrInvalidRows
	}
	if index.Authority != authority {
		index = mailIndex{Authority: authority}
	}
	if index.Cursor != "" {
		err = g.refreshMailIndex(ctx, &index)
		if errors.Is(err, ErrCursorExpired) {
			index.Cursor = ""
		} else if err != nil {
			return index, key, digest, err
		}
	}
	if index.Cursor == "" {
		index.Rows = make(map[string]map[string]any)
		var profile struct{ HistoryID string }
		if _, err := g.get(ctx, gmailPath+"/profile", nil, &profile); err != nil {
			return index, key, digest, err
		}
		if profile.HistoryID == "" {
			return index, key, digest, ErrInvalidRows
		}
		index.Cursor = profile.HistoryID
	}
	return index, key, digest, nil
}

func (g googleSource) refreshMailIndex(ctx context.Context, index *mailIndex) error {
	// A page may list the same message more than once. Delete each changed
	// metadata row, then let the scoped list decide which keys to refill.
	query := url.Values{"startHistoryId": {index.Cursor}, "maxResults": {"100"}}
	for range 5 {
		var page struct {
			HistoryID, NextPageToken string
			History                  []struct {
				Messages                                                   []struct{ ID string }
				MessagesAdded, MessagesDeleted, LabelsAdded, LabelsRemoved []struct{ Message struct{ ID string } }
			}
		}
		status, err := g.get(ctx, gmailPath+"/history", query, &page)
		if status == http.StatusNotFound {
			return ErrCursorExpired
		}
		if err != nil {
			return err
		}
		for _, history := range page.History {
			for _, msg := range history.Messages {
				delete(index.Rows, msg.ID)
			}
			for _, changes := range [][]struct{ Message struct{ ID string } }{
				history.MessagesAdded, history.MessagesDeleted, history.LabelsAdded, history.LabelsRemoved,
			} {
				for _, change := range changes {
					delete(index.Rows, change.Message.ID)
				}
			}
		}
		if page.NextPageToken == "" {
			if page.HistoryID == "" {
				return ErrInvalidRows
			}
			index.Cursor = page.HistoryID
			return nil
		}
		query.Set("pageToken", page.NextPageToken)
	}
	// A large gap refreshes the bounded index rather than projecting stale
	// metadata. The subsequent bootstrap samples the current mailbox.
	return ErrCursorExpired
}

func (g googleSource) indexedMail(ctx context.Context, index *mailIndex, query string, limit int) ([]map[string]any, bool, error) {
	ids, next, err := g.gmailIDs(ctx, query, "", limit)
	if err != nil {
		return nil, false, err
	}
	missing := make([]string, 0, len(ids))
	for _, id := range ids {
		if index.Rows[id] == nil {
			missing = append(missing, id)
		}
	}
	fetched, err := g.messages(ctx, missing)
	if err != nil {
		return nil, false, err
	}
	for _, row := range fetched {
		index.Rows[googleText(row, "id")] = row
	}
	rows := make([]map[string]any, 0, len(ids))
	active := make(map[string]map[string]any, len(ids))
	for _, id := range ids {
		if row := index.Rows[id]; row != nil {
			rows = append(rows, row)
			active[id] = row
		}
	}
	// Retain only the current bounded mailbox slice. Bodies are never present.
	index.Rows = active
	return rows, next != "", nil
}

func saveMailIndex(store *data.Store, key, previous string, index mailIndex) error {
	raw, err := json.Marshal(index)
	if err != nil {
		return ErrInvalidRows
	}
	if len(raw) > maxBytes {
		return ErrTooLarge
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	if digest == previous {
		return nil
	}
	return store.SaveSourceIndex(key, previous, digest, raw)
}
