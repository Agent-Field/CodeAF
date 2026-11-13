// Package knowledgefixture seeds native SQLite publications for reader tests.
// Production code never imports this package or reads fixture JSON files.
package knowledgefixture

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
)

func Read(path string) ([]byte, error) {
	uri := sqliteuri.File(path)
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var raw string
	err = db.QueryRow("SELECT payload FROM state WHERE key='generated'").Scan(&raw)
	return []byte(raw), err
}
func Write(path string, raw []byte, _ os.FileMode) error {
	vault := knowledge.Open(filepath.Dir(filepath.Dir(path)))
	if err := vault.StartBuild(); err != nil {
		return err
	}
	uri := sqliteuri.File(path)
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return err
	}
	defer db.Close()
	var data struct {
		Notes  []knowledge.Page `json:"notes"`
		Claims []struct {
			ID, Text, Kind, Status string
			Evidence               []knowledge.Evidence
		}
		Sources map[string]struct{ Role, TS string }
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO state(key,payload) VALUES('job','{"phase":"ready"}') ON CONFLICT(key) DO UPDATE SET payload=excluded.payload`); err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	revision, _ := json.Marshal(map[string]string{"value": hex.EncodeToString(sum[:])})
	if _, err = tx.Exec("INSERT INTO state(key,payload) VALUES('revision',?) ON CONFLICT(key) DO UPDATE SET payload=excluded.payload", string(revision)); err != nil {
		return err
	}
	for _, key := range []string{"generated", "published"} {
		if _, err = tx.Exec("INSERT INTO state(key,payload) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET payload=excluded.payload", key, string(raw)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM pages;DELETE FROM page_fts"); err != nil {
		return err
	}
	for _, p := range data.Notes {
		evidence := map[string]bool{}
		hydrate := func(e knowledge.Evidence) knowledge.Evidence {
			s := data.Sources[e.Source]
			e.Role = s.Role
			e.TS = s.TS
			key := e.Source + "\x00" + e.Quote
			if !evidence[key] {
				p.Evidence = append(p.Evidence, e)
				evidence[key] = true
			}
			return e
		}
		for _, e := range p.OriginalEvidence {
			hydrate(e)
		}
		for si := range p.Sections {
			for pi := range p.Sections[si].Paragraphs {
				para := &p.Sections[si].Paragraphs[pi]
				for ei, e := range para.Evidence {
					para.Evidence[ei] = hydrate(e)
				}
				for _, id := range para.Claims {
					for _, c := range data.Claims {
						if c.ID == id && c.Status == "active" {
							for _, e := range c.Evidence {
								para.Evidence = append(para.Evidence, hydrate(e))
							}
						}
					}
				}
			}
		}
		fallback := []knowledge.Paragraph{}
		for _, id := range p.Claims {
			for _, c := range data.Claims {
				if c.ID == id && c.Status == "active" {
					refs := []knowledge.Evidence{}
					for _, e := range c.Evidence {
						refs = append(refs, hydrate(e))
					}
					fallback = append(fallback, knowledge.Paragraph{Text: "[" + c.Kind + "] " + c.Text, Claims: []string{id}, Evidence: refs})
				}
			}
		}
		if len(p.Sections) == 0 && len(fallback) > 0 {
			p.Sections = []knowledge.Section{{Heading: "Recorded knowledge", Paragraphs: fallback}}
		}
		encoded, err := json.Marshal(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(encoded)
		p.Revision = hex.EncodeToString(sum[:])
		encoded, err = json.Marshal(p)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO pages VALUES(?,?,?,?)", p.ID, p.Title, p.Scope, string(encoded)); err != nil {
			return err
		}
		var body strings.Builder
		for _, s := range p.Sections {
			body.WriteString(s.Heading)
			for _, p := range s.Paragraphs {
				body.WriteByte(' ')
				body.WriteString(p.Text)
			}
		}
		if _, err = tx.Exec("INSERT INTO page_fts VALUES(?,?,?,?)", p.ID, p.Title, p.Scope, body.String()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
