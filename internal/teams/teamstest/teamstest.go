// Package teamstest states a teams store for tests outside internal/teams.
//
// A test that wrote teams.json by hand stated exactly what was stored, repairs
// and all; [teams.Save] tidies first, so it cannot state a store another build
// left untidy. [Write] puts rows in the store with SQL, as stored, and moves
// the store's revision so every reader sees them. It knows the store's schema,
// which is the store's own business: it is for tests and nothing else.
package teamstest

import (
	"database/sql"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/sqlitefile"
	"github.com/Agent-Field/codeaf/internal/teams"
	_ "modernc.org/sqlite"
)

// Doc is the old teams.json's shape, which [Write] reads and [JSON] answers.
type Doc struct {
	Version int          `json:"version"`
	Teams   []teams.Team `json:"teams"`
}

// Write makes profile's store hold exactly the teams raw lists (in the shape of
// [Doc]), in order and unrepaired, replacing whatever it held.
func Write(t testing.TB, profile string, raw []byte) {
	t.Helper()
	var d Doc
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	WriteTeams(t, profile, d.Teams)
}

// WriteTeams is [Write] of a list already decoded.
func WriteTeams(t testing.TB, profile string, list []teams.Team) {
	t.Helper()
	// Saving nothing makes the store, its schema and its revision row.
	if _, err := os.Stat(teams.Path(profile)); os.IsNotExist(err) {
		if err := teams.Save(profile, nil); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", sqlitefile.URI(teams.Path(profile), "mode=rw&_pragma=busy_timeout(10000)&_txlock=immediate"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	var rev int64
	if err := tx.QueryRow("SELECT rev FROM meta WHERE id = 1").Scan(&rev); err != nil {
		t.Fatal(err)
	}
	rev++
	exec("DELETE FROM teams")
	exec("DELETE FROM members")
	exec("DELETE FROM briefs")
	for pos, team := range list {
		bare := team
		bare.Members, bare.FormerMembers = nil, nil
		doc, err := json.Marshal(bare)
		if err != nil {
			t.Fatal(err)
		}
		made := int64(0)
		if !team.Made.IsZero() {
			made = team.Made.UnixNano()
		}
		exec("INSERT INTO teams (id, pos, rev, parent, made_ns, closed, root, factory, doc) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			team.ID, pos, rev, team.Parent, made, team.Closed(), team.Root, team.FactoryManaged, string(doc))
		for former, members := range [][]teams.Member{team.Members, team.FormerMembers} {
			for i, m := range members {
				factory := m.FactoryBrief
				m.Briefs, m.FactoryBrief = nil, ""
				raw, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				exec("INSERT INTO members (team_id, former, pos, key, doc) VALUES (?, ?, ?, ?, ?)", team.ID, former == 1, i, m.Key, string(raw))
				if factory != "" && former == 0 {
					exec("INSERT OR REPLACE INTO briefs (team_id, key, seq, doc) VALUES (?, ?, 0, ?)", team.ID, m.Key, factory)
				}
			}
		}
	}
	exec("UPDATE meta SET rev = ? WHERE id = 1", rev)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// JSON is the store's teams, as stored and without briefs, as one document in
// the shape of [Doc].
func JSON(t testing.TB, profile string) []byte {
	t.Helper()
	f, err := teams.Snapshot(profile)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(Doc{Version: teams.Version, Teams: f.Teams}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Files are the store's files: the database, its WAL and its shared memory.
func Files(profile string) []string {
	path := teams.Path(profile)
	return []string{path, path + "-wal", path + "-shm"}
}

// Hide moves the store's files aside, so every reader finds no store. The
// answer puts the store back as it was; calling it twice is calling it once.
func Hide(t testing.TB, profile string) (restore func()) {
	t.Helper()
	return Break(t, profile, nil)
}

// Break moves the store's files aside and puts junk where the database was,
// so every reader finds a store it cannot read (nil junk puts nothing there:
// [Hide]). The answer puts the store back as it was; calling it twice is
// calling it once.
func Break(t testing.TB, profile string, junk []byte) (restore func()) {
	t.Helper()
	files := Files(profile)
	for _, path := range files {
		if err := os.Rename(path, path+".kept"); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if junk != nil {
		if err := os.WriteFile(files[0], junk, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for _, path := range files {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					t.Error(err)
				}
				if err := os.Rename(path+".kept", path); err != nil && !os.IsNotExist(err) {
					t.Error(err)
				}
			}
		})
	}
}
