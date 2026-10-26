package knowledge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/sqliteuri"
)

const knowledgeSchema = `
 PRAGMA journal_mode=WAL;
 CREATE TABLE IF NOT EXISTS state(key TEXT PRIMARY KEY,payload TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS events(partition TEXT,id TEXT,payload TEXT NOT NULL,arrived REAL NOT NULL,status TEXT NOT NULL DEFAULT 'pending',attempts INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(partition,id));
 CREATE INDEX IF NOT EXISTS pending_lookup ON events(partition,status,arrived,id);
 CREATE TABLE IF NOT EXISTS publications(partition TEXT PRIMARY KEY,payload TEXT NOT NULL,revision INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS receipts(id TEXT PRIMARY KEY,model TEXT NOT NULL,role TEXT NOT NULL,cost REAL NOT NULL DEFAULT 0,payload TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS overrides(id TEXT PRIMARY KEY,payload TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS pages(id TEXT PRIMARY KEY,title TEXT NOT NULL,scope TEXT NOT NULL,payload TEXT NOT NULL);
 CREATE VIRTUAL TABLE IF NOT EXISTS page_fts USING fts5(id UNINDEXED,title,scope,body,tokenize='unicode61');
 CREATE TABLE IF NOT EXISTS history_errors(journal TEXT PRIMARY KEY,message TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS cursors(journal TEXT PRIMARY KEY,offset INTEGER NOT NULL,skip INTEGER NOT NULL,scope TEXT NOT NULL,prefix_hash TEXT NOT NULL DEFAULT '');
 CREATE TABLE IF NOT EXISTS proposals(partition TEXT,id TEXT,payload TEXT,PRIMARY KEY(partition,id));
 CREATE TABLE IF NOT EXISTS source_windows(partition TEXT,id TEXT,start INTEGER,length INTEGER,PRIMARY KEY(partition,id));
 CREATE TABLE IF NOT EXISTS claim_keys(partition TEXT,id TEXT,PRIMARY KEY(partition,id));
 CREATE TABLE IF NOT EXISTS proposal_sources(partition TEXT,id TEXT,source TEXT,PRIMARY KEY(partition,id,source));
 CREATE INDEX IF NOT EXISTS proposal_source_lookup ON proposal_sources(partition,source);
 CREATE TABLE IF NOT EXISTS watch_terms(term TEXT NOT NULL,question TEXT NOT NULL,PRIMARY KEY(term,question));
 CREATE TABLE IF NOT EXISTS watches(partition TEXT,id TEXT,terms TEXT,PRIMARY KEY(partition,id));
 CREATE TABLE IF NOT EXISTS wakes(partition TEXT,id TEXT,source TEXT,reason TEXT,PRIMARY KEY(partition,id,source));
`

var schemaLocks sync.Map
var schemaReady sync.Map

func (v *Vault) database() (*sql.DB, error) {
	if err := os.MkdirAll(v.root, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(v.root, "knowledge.sqlite")
	uri := sqliteuri.File(path)
	q := uri.Query()
	q.Add("_pragma", "busy_timeout(1000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Set("_txlock", "immediate")
	uri.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// SQLite owns both the compiler checkpoints and the effective pages. Opening
	// never performs a history scan or a provider request.
	readyLock, _ := schemaLocks.LoadOrStore(path, &sync.Mutex{})
	mu := readyLock.(*sync.Mutex)
	mu.Lock()
	_, ready := schemaReady.Load(path)
	_, statErr := os.Stat(path)
	if !ready || os.IsNotExist(statErr) {
		if _, err = db.Exec(knowledgeSchema); err == nil {
			err = os.Chmod(path, 0600)
		}
		if err == nil {
			schemaReady.Store(path, true)
		}
	}
	mu.Unlock()
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

type stateReader interface{ QueryRow(string, ...any) *sql.Row }
type stateWriter interface {
	Exec(string, ...any) (sql.Result, error)
}

func stateDocument(db stateReader, key string) (document, error) {
	var raw string
	err := db.QueryRow("SELECT payload FROM state WHERE key=?", key).Scan(&raw)
	if err == sql.ErrNoRows {
		return document{}, nil
	}
	if err != nil {
		return nil, err
	}
	var result document
	err = json.Unmarshal([]byte(raw), &result)
	return result, err
}
func saveState(db stateWriter, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = db.Exec("INSERT INTO state(key,payload) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET payload=excluded.payload WHERE state.payload<>excluded.payload", key, string(raw))
	return err
}
func (v *Vault) state(key string) (document, error) {
	db, err := v.database()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return stateDocument(db, key)
}
func (v *Vault) Enabled() bool {
	value, err := v.state("config")
	return err == nil && flag(value["enabled"])
}
func (v *Vault) EnabledSince() time.Time {
	value, err := v.state("config")
	if err != nil {
		return time.Time{}
	}
	at, _ := time.Parse(time.RFC3339Nano, textValue(value["created_at"]))
	return at
}
func (v *Vault) Capture(source Source) error {
	if !v.Enabled() {
		return nil
	}
	if err := validateCapture(source); err != nil {
		return err
	}
	if source.Kind == "" {
		source.Kind = "agent_synthesis"
		if source.Role == "user" {
			source.Kind = "user_statement"
		}
	}
	db, err := v.database()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = v.captureTx(tx, source); err != nil {
		return err
	}
	return tx.Commit()
}
func validateCapture(s Source) error {
	if s.ID == "" || s.Session == "" || strings.TrimSpace(s.Text) == "" {
		return errors.New("knowledge source requires identity, session and text")
	}
	if _, err := time.Parse(time.RFC3339Nano, s.TS); err != nil {
		return errors.New("knowledge source requires a valid timestamp")
	}
	if s.Role != "user" && s.Role != "assistant" {
		return errors.New("knowledge captures user and assistant turns only")
	}
	expected := "agent_synthesis"
	if s.Role == "user" {
		expected = "user_statement"
	}
	if s.Kind != "" && s.Kind != expected {
		return errors.New("knowledge source kind does not match role")
	}
	return nil
}
func (v *Vault) captureTx(tx *sql.Tx, s Source) error {
	runes := []rune(s.Text)
	for start := 0; start < len(runes); start += 24000 {
		part := s
		part.Text = string(runes[start:min(len(runes), start+24000)])
		if len(runes) > 24000 {
			part.ID = s.ID + ":span:" + mustJSON(start)
		}
		if part.Kind == "" {
			part.Kind = "agent_synthesis"
			if part.Role == "user" {
				part.Kind = "user_statement"
			}
		}
		source := document{"id": part.ID, "session": part.Session, "role": part.Role, "ts": part.TS, "text": part.Text, "provenance": document{"kind": part.Kind, "content_hash": identity(part.Text), "recorded_at": part.TS, "valid_at": part.TS, "locator": document{"session": part.Session, "message": part.ID, "project": part.Project}, "revision": nil, "independently_verified": false}}
		var raw string
		err := tx.QueryRow("SELECT payload FROM events WHERE partition='profile' AND id=?", part.ID).Scan(&raw)
		if err == nil {
			var prior document
			if json.Unmarshal([]byte(raw), &prior) != nil || identity(prior) != identity(source) {
				return errors.New("knowledge immutable source changed")
			}
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		stamp, _ := time.Parse(time.RFC3339Nano, part.TS)
		if _, err = tx.Exec("INSERT INTO events(partition,id,payload,arrived) VALUES('profile',?,?,?)", part.ID, mustJSON(source), float64(stamp.UnixNano())/1e9); err != nil {
			return err
		}
		if err = wakeFromSource(tx, source); err != nil {
			return err
		}
	}
	return nil
}
func (v *Vault) Status() (Snapshot, error) {
	db, err := v.database()
	if err != nil {
		return Snapshot{}, err
	}
	defer db.Close()
	return v.status(db)
}
func (v *Vault) status(db *sql.DB) (Snapshot, error) {
	snapshot := Snapshot{Model: Model}
	var configRaw, statusRaw, jobRaw, revisionRaw string
	err := db.QueryRow(`SELECT coalesce((SELECT payload FROM state WHERE key='config'),'{}'),coalesce((SELECT payload FROM state WHERE key='status'),'{}'),coalesce((SELECT payload FROM state WHERE key='job'),'{}'),coalesce((SELECT payload FROM state WHERE key='revision'),'{}'),(SELECT count(*) FROM events WHERE partition='profile' AND status IN ('pending','revisit'))`).Scan(&configRaw, &statusRaw, &jobRaw, &revisionRaw, &snapshot.Pending)
	if err != nil {
		return snapshot, err
	}
	var settings, status, job document
	if err = json.Unmarshal([]byte(configRaw), &settings); err != nil {
		return snapshot, err
	}
	if err = json.Unmarshal([]byte(statusRaw), &status); err != nil {
		return snapshot, err
	}
	if err = json.Unmarshal([]byte(jobRaw), &job); err != nil {
		return snapshot, err
	}
	var revision document
	if err = json.Unmarshal([]byte(revisionRaw), &revision); err != nil {
		return snapshot, err
	}
	snapshot.Revision = textValue(revision["value"])
	snapshot.Enabled = flag(settings["enabled"])
	snapshot.LastError = textValue(status["last_error"])
	snapshot.NeedsUpdate = flag(status["needs_update"])
	snapshot.CostUSD = number(status["cost_usd"])
	snapshot.Model = knowledgeModel(v.profile)
	snapshot.Build = BuildStatus{Phase: BuildPhase(textValue(job["phase"])), Chats: int(number(job["chats"])), Imported: int(number(job["imported"])), Sources: int(number(job["sources"])), Problems: int(number(job["problems"])), Error: textValue(job["error"])}
	return snapshot, nil
}
func readPages(db *sql.DB) ([]Page, error) {
	rows, err := db.Query("SELECT payload FROM pages ORDER BY title,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Page{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var page Page
		if err = json.Unmarshal([]byte(raw), &page); err != nil {
			return nil, err
		}
		result = append(result, page)
	}
	return result, rows.Err()
}
func (v *Vault) Snapshot() (Snapshot, error) {
	db, err := v.database()
	if err != nil {
		return Snapshot{}, err
	}
	defer db.Close()
	// A deferred read transaction pins the revision and pages to one WAL
	// snapshot without acquiring the immediate write lock used by mutations.
	if _, err = db.Exec("BEGIN DEFERRED"); err != nil {
		return Snapshot{}, err
	}
	defer db.Exec("ROLLBACK")
	snapshot, err := v.status(db)
	if err != nil || !snapshot.Enabled {
		return snapshot, err
	}
	snapshot.Pages, err = readPages(db)
	return snapshot, err
}
func (v *Vault) Read(id string) (Page, error) {
	db, err := v.database()
	if err != nil {
		return Page{}, err
	}
	defer db.Close()
	var raw string
	if err = db.QueryRow("SELECT payload FROM pages WHERE id=?", id).Scan(&raw); err == sql.ErrNoRows {
		return Page{}, os.ErrNotExist
	} else if err != nil {
		return Page{}, err
	}
	var p Page
	err = json.Unmarshal([]byte(raw), &p)
	return p, err
}
func (v *Vault) Search(query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return nil, nil
	}
	limit = min(limit, 100)
	db, err := v.database()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	terms := compilerWords.FindAllString(query, -1)
	args := []any{}
	statement := "SELECT payload FROM pages ORDER BY title,id LIMIT ?"
	if len(terms) > 0 {
		match := []string{}
		for _, term := range terms[:min(20, len(terms))] {
			match = append(match, `"`+strings.ReplaceAll(term, `"`, `""`)+`"*`)
		}
		statement = "SELECT p.payload FROM page_fts JOIN pages p ON p.id=page_fts.id WHERE page_fts MATCH ? ORDER BY bm25(page_fts,0,4,1,1),p.id LIMIT ?"
		args = append(args, strings.Join(match, " OR "))
	}
	args = append(args, limit)
	rows, err := db.Query(statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Result{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p Page
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		snippet := p.Title
		if len(p.Sections) > 0 && len(p.Sections[0].Paragraphs) > 0 {
			snippet = p.Sections[0].Paragraphs[0].Text
		}
		runes := []rune(snippet)
		if len(runes) > 600 {
			snippet = string(runes[:600]) + "…"
		}
		result = append(result, Result{Page: p, Snippet: snippet})
	}
	return result, rows.Err()
}
func storePages(tx *sql.Tx, pages []Page) error {
	type storedPage struct {
		rowID   int64
		payload string
	}
	existing := make(map[string]storedPage)
	rows, err := tx.Query("SELECT rowid,id,payload FROM pages")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var page storedPage
		if err = rows.Scan(&page.rowID, &id, &page.payload); err != nil {
			rows.Close()
			return err
		}
		existing[id] = page
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Old fixtures/publications may have independently assigned FTS row IDs.
	// Reconcile that layout once while keeping all subsequent writes keyed by
	// the indexed pages row ID, including deletions.
	indexed := make(map[int64]string)
	rows, err = tx.Query("SELECT rowid,id FROM page_fts")
	if err != nil {
		return err
	}
	for rows.Next() {
		var rowID int64
		var id string
		if err = rows.Scan(&rowID, &id); err != nil {
			rows.Close()
			return err
		}
		indexed[rowID] = id
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	wanted := make(map[string]bool, len(pages))
	for _, page := range pages {
		wanted[page.ID] = true
	}
	for id, prior := range existing {
		if wanted[id] {
			continue
		}
		if _, err = tx.Exec("DELETE FROM pages WHERE id=?", id); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM page_fts WHERE rowid=?", prior.rowID); err != nil {
			return err
		}
		delete(indexed, prior.rowID)
	}
	liveRows := make(map[int64]bool, len(pages))
	for _, p := range pages {
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		prior, present := existing[p.ID]
		rowID := prior.rowID
		changed := !present || prior.payload != string(raw)
		if changed {
			err = tx.QueryRow("INSERT INTO pages(id,title,scope,payload) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,scope=excluded.scope,payload=excluded.payload RETURNING rowid", p.ID, p.Title, p.Scope, string(raw)).Scan(&rowID)
			if err != nil {
				return err
			}
		}
		liveRows[rowID] = true
		if !changed && indexed[rowID] == p.ID {
			continue
		}
		if _, err = tx.Exec("DELETE FROM page_fts WHERE rowid=?", rowID); err != nil {
			return err
		}
		var body strings.Builder
		for _, section := range p.Sections {
			body.WriteString(section.Heading)
			body.WriteByte(' ')
			for _, para := range section.Paragraphs {
				body.WriteString(para.Text)
				body.WriteByte(' ')
			}
		}
		if _, err = tx.Exec("INSERT INTO page_fts(rowid,id,title,scope,body) VALUES(?,?,?,?,?)", rowID, p.ID, p.Title, p.Scope, body.String()); err != nil {
			return err
		}
	}
	for rowID := range indexed {
		if !liveRows[rowID] {
			if _, err = tx.Exec("DELETE FROM page_fts WHERE rowid=?", rowID); err != nil {
				return err
			}
		}
	}
	return nil
}
func (v *Vault) commitPublication(vault, learning, registry document, digest string) error {
	if err := validatePublication(vault); err != nil {
		return err
	}
	db, err := v.database()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = saveState(tx, "generated", vault); err != nil {
		return err
	}
	overrides, err := readOverrides(tx)
	if err != nil {
		return err
	}
	effective := applyNativeOverrides(clone(vault), overrides)
	linkCompilation(effective)
	pages, err := materializePages(effective)
	if err != nil {
		return err
	}
	pages, err = applyPageOverrides(pages, overrides)
	if err != nil {
		return err
	}
	if err = storePages(tx, pages); err != nil {
		return err
	}
	if err = saveState(tx, "revision", document{"value": identity(effective)}); err != nil {
		return err
	}
	if err = saveState(tx, "published", effective); err != nil {
		return err
	}
	if err = storeQuestionWatches(tx, learning); err != nil {
		return err
	}
	if err = saveState(tx, "learning", learning); err != nil {
		return err
	}
	if err = saveState(tx, "topics", registry); err != nil {
		return err
	}
	if err = saveState(tx, "projection", document{"input_hash": digest}); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM state WHERE key='organization_boundary'"); err != nil {
		return err
	}
	return tx.Commit()
}
func validatePublication(vault document) error {
	claims := index(items(vault["claims"]))
	notes := index(items(vault["notes"]))
	sources := object(vault["sources"])
	if len(claims) != len(items(vault["claims"])) || len(notes) != len(items(vault["notes"])) {
		return errors.New("duplicate knowledge identity")
	}
	for _, claim := range claims {
		if textValue(claim["id"]) == "" || !validSourceEvidence(items(claim["evidence"]), sources, false) {
			return errors.New("unsupported knowledge claim evidence")
		}
	}
	for _, note := range notes {
		if textValue(note["id"]) == "" || !subset(stringsValue(note["claims"]), claims) {
			return errors.New("unknown knowledge claim reference")
		}
		for _, link := range items(note["links"]) {
			if notes[textValue(link["id"])] == nil {
				return errors.New("unknown knowledge link target")
			}
		}
		for _, section := range items(note["sections"]) {
			for _, para := range items(section["paragraphs"]) {
				if !subset(stringsValue(para["claims"]), claims) || !validSourceEvidence(items(para["evidence"]), sources, false) {
					return errors.New("unsupported knowledge paragraph")
				}
			}
		}
	}
	return nil
}
func pageBody(p Page) string {
	var body strings.Builder
	body.WriteString(p.Title)
	for _, s := range p.Sections {
		for _, para := range s.Paragraphs {
			body.WriteString(para.Text)
		}
	}
	return body.String()
}
