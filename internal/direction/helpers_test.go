package direction

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// sqlTx keeps test signatures short.
type sqlTx = sql.Tx

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "collections.db"))
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func card(t *testing.T, proposal string) PersonReceipt {
	t.Helper()
	r, err := FromCardAnswer(proposal)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func folder(t *testing.T, s *Store, name string) string {
	t.Helper()
	c, err := s.ws.Create(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func rule(text string, targets ...Target) Draft {
	return Draft{Kind: Rule, Title: "Rule", Text: text, QuoteOrigin: AdoptedWording,
		Source: Source{Class: SourceConversation, ID: "chat"}, Targets: targets}
}

func finding(text string, targets ...Target) Draft {
	d := rule(text, targets...)
	d.Kind = Finding
	return d
}

func chatTarget(id string) Target { return Target{Kind: TargetConversation, Ref: id} }

func folderTarget(id string, reach Reach) Target {
	return Target{Kind: TargetCollection, Ref: id, Reach: reach}
}

var model = As(AuthorModel, "session-1")

// musts returns a checker for a (Revision, error) pair that fails t on error.
func musts(t *testing.T) func(Revision, error) Revision {
	return func(r Revision, err error) Revision {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
}

// liveOf is the whole live index in key order.
func liveOf(t *testing.T, s *Store) []liveRow {
	t.Helper()
	var rows []liveRow
	err := workspace.ReadSnapshot(context.Background(), s.ws, func(tx *sql.Tx) error {
		var err error
		rows, err = storedLive(context.Background(), tx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(rows, func(i, j int) bool { return rowKey(rows[i]) < rowKey(rows[j]) })
	return rows
}

func rowKey(r liveRow) string {
	return fmt.Sprint(r.kind, "\x00", r.ref, "\x00", r.session, "\x00", r.lane, "\x00", r.record)
}
