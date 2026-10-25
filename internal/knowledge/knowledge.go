// Package knowledge owns opt-in, project-scoped knowledge capture and browsing.
// Capture is local and model-free. Flush explicitly runs the embedded compiler.
package knowledge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	assets "github.com/Agent-Field/codeaf/experiments/knowledge"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
	"io/fs"
	_ "modernc.org/sqlite"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const Model = config.DefaultKnowledgeModel

var ErrDisabled = errors.New("knowledge is not enabled for this project")
var locks sync.Map

type Source struct {
	ID      string `json:"id"`
	Session string `json:"session"`
	Role    string `json:"role"`
	TS      string `json:"ts"`
	Text    string `json:"text"`
	Kind    string `json:"kind"`
}
type Link struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}
type InlineLink struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}
type Evidence struct {
	Source string `json:"source"`
	Quote  string `json:"quote"`
	TS     string `json:"ts,omitempty"`
	Role   string `json:"role,omitempty"`
}
type Paragraph struct {
	Text        string       `json:"text"`
	Claims      []string     `json:"claims"`
	Evidence    []Evidence   `json:"evidence"`
	InlineLinks []InlineLink `json:"inline_links"`
}
type Section struct {
	Heading    string      `json:"heading"`
	Paragraphs []Paragraph `json:"paragraphs"`
}
type Page struct {
	ID                   string     `json:"id"`
	Title                string     `json:"title"`
	Scope                string     `json:"scope"`
	Kind                 string     `json:"kind"`
	Folder               []string   `json:"folder"`
	Sections             []Section  `json:"sections"`
	Links                []Link     `json:"links"`
	Backlinks            []Link     `json:"backlinks"`
	Aliases              []string   `json:"aliases"`
	Claims               []string   `json:"claims"`
	Evidence             []Evidence `json:"-"`
	NeedsEditorialUpdate bool       `json:"needs_editorial_update"`
	LearningState        string     `json:"learning_state"`
}
type Snapshot struct {
	Enabled     bool
	Pages       []Page
	Pending     int
	NeedsUpdate bool
	LastError   string
	CostUSD     float64
	Model       string
}
type Result struct {
	Page    Page
	Snippet string
}
type Vault struct{ root, profile, project string }

func hash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func Open(profile, project string) *Vault {
	absolute, err := filepath.Abs(project)
	if err == nil {
		project = filepath.Clean(absolute)
	}
	return &Vault{root: filepath.Join(profile, "knowledge", hash(project)[:24]), profile: profile, project: project}
}
func (v *Vault) Directory() string { return v.root }
func (v *Vault) Enabled() bool {
	data, err := os.ReadFile(filepath.Join(v.root, "config.json"))
	if err != nil {
		return false
	}
	var c struct {
		Project string `json:"project"`
		Enabled bool   `json:"enabled"`
	}
	return json.Unmarshal(data, &c) == nil && c.Enabled && c.Project == v.project
}
func (v *Vault) EnabledSince() time.Time {
	b, e := os.ReadFile(filepath.Join(v.root, "config.json"))
	if e != nil {
		return time.Time{}
	}
	var c struct {
		CreatedAt string `json:"created_at"`
	}
	if json.Unmarshal(b, &c) != nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, c.CreatedAt)
	return t
}
func (v *Vault) Create() error {
	if v.Enabled() {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(v.root, "queue"), 0700); err != nil {
		return err
	}
	return writeJSON(filepath.Join(v.root, "config.json"), map[string]any{"enabled": true, "project": v.project, "schema": 1, "model": config.KnowledgeModelAt(v.profile), "created_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
func writeJSON(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".knowledge-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(name, path)
}
func (v *Vault) Capture(s Source) error {
	if !v.Enabled() {
		return nil
	}
	if s.ID == "" || s.Session == "" || strings.TrimSpace(s.Text) == "" {
		return errors.New("knowledge source requires identity, session and text")
	}
	if _, e := time.Parse(time.RFC3339Nano, s.TS); e != nil {
		return fmt.Errorf("knowledge source time: %w", e)
	}
	if s.Role != "user" && s.Role != "assistant" {
		return errors.New("knowledge captures user and assistant turns only")
	}
	expected := "agent_synthesis"
	if s.Role == "user" {
		expected = "user_statement"
	}
	if s.Kind == "" {
		s.Kind = expected
	}
	if s.Kind != expected {
		return errors.New("knowledge source kind does not match role")
	}
	lock, _ := locks.LoadOrStore(v.root, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	// Bound source spans without truncating a saved turn. IDs and timestamps replay exactly.
	runes := []rune(s.Text)
	for start := 0; start < len(runes); start += 24000 {
		part := s
		end := start + 24000
		if end > len(runes) {
			end = len(runes)
		}
		part.Text = string(runes[start:end])
		if len(runes) > 24000 {
			part.ID = fmt.Sprintf("%s:span:%d", s.ID, start)
		}
		path := filepath.Join(v.root, "queue", hash(part.ID)+".json")
		b, e := os.ReadFile(path)
		if os.IsNotExist(e) {
			name := hash(part.ID) + ".json"
			b, e = os.ReadFile(filepath.Join(v.root, "archive", name[:2], name))
		}
		if e == nil {
			var prior Source
			if json.Unmarshal(b, &prior) != nil || prior != part {
				return errors.New("knowledge immutable source changed")
			}
			continue
		}
		if !os.IsNotExist(e) {
			return e
		}
		if e = writeJSON(path, part); e != nil {
			return e
		}
	}
	return nil
}

// Status returns pending capture and compiler metadata without loading articles.
func (v *Vault) Status() (Snapshot, error) {
	s := Snapshot{Enabled: v.Enabled(), Model: config.KnowledgeModelAt(v.profile)}
	if !s.Enabled {
		return s, nil
	}
	entries, e := os.ReadDir(filepath.Join(v.root, "queue"))
	if e != nil {
		return s, e
	}
	// SQLite is authoritative for imported pending sources. The queue contains
	// only arrivals not archived yet; exclude events committed before a crash.
	dbPath := filepath.Join(v.root, "capture", "workspace.sqlite")
	var db *sql.DB
	var partition string
	if _, err := os.Stat(dbPath); err == nil {
		uri := sqliteuri.File(dbPath)
		q := uri.Query()
		q.Set("mode", "ro")
		q.Set("_pragma", "busy_timeout(1000)")
		uri.RawQuery = q.Encode()
		db, e = sql.Open("sqlite", uri.String())
		if e != nil {
			return s, e
		}
		defer db.Close()
		err = db.QueryRow("SELECT partition FROM events LIMIT 1").Scan(&partition)
		if err != nil && err != sql.ErrNoRows {
			return s, err
		}
		if err == nil {
			if e = db.QueryRow("SELECT count(*) FROM events WHERE partition=? AND status IN ('pending','revisit')", partition).Scan(&s.Pending); e != nil {
				return s, e
			}
		}
	} else if !os.IsNotExist(err) {
		return s, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(v.root, "queue", entry.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return s, err
		}
		var event Source
		if err = json.Unmarshal(raw, &event); err != nil {
			return s, err
		}
		if db != nil {
			var exists int
			err = db.QueryRow("SELECT 1 FROM events WHERE partition=? AND id=?", partition, event.ID).Scan(&exists)
			if err == nil {
				continue
			}
			if err != sql.ErrNoRows {
				return s, err
			}
		}
		s.Pending++
	}
	var status struct {
		LastError   string  `json:"last_error"`
		NeedsUpdate bool    `json:"needs_update"`
		Cost        float64 `json:"cost_usd"`
	}
	if b, err := os.ReadFile(filepath.Join(v.root, "status.json")); err == nil {
		if err = json.Unmarshal(b, &status); err != nil {
			return s, err
		}
		s.NeedsUpdate = status.NeedsUpdate || status.LastError != ""
		s.LastError = status.LastError
		s.CostUSD = status.Cost
	}
	return s, nil
}

// Snapshot loads browsable pages only when the caller needs their content.
func (v *Vault) Snapshot() (Snapshot, error) {
	s, e := v.Status()
	if e != nil || !s.Enabled {
		return s, e
	}
	b, e := os.ReadFile(filepath.Join(v.root, "knowledge", "vault.json"))
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	var data struct {
		Notes  []Page `json:"notes"`
		Claims []struct {
			ID       string     `json:"id"`
			Text     string     `json:"text"`
			Kind     string     `json:"kind"`
			Status   string     `json:"status"`
			Evidence []Evidence `json:"evidence"`
		} `json:"claims"`
		Sources map[string]struct {
			Role string `json:"role"`
			TS   string `json:"ts"`
		} `json:"sources"`
	}
	if e = json.Unmarshal(b, &data); e != nil {
		return s, e
	}
	s.Pages = data.Notes
	claimIndex := make(map[string]int, len(data.Claims))
	for i, c := range data.Claims {
		claimIndex[c.ID] = i
	}
	for i := range s.Pages {
		p := &s.Pages[i]
		seen := map[string]bool{}
		add := func(e Evidence) {
			key := e.Source + "\x00" + e.Quote
			if seen[key] {
				return
			}
			seen[key] = true
			if src, ok := data.Sources[e.Source]; ok {
				e.Role = src.Role
				e.TS = src.TS
			}
			p.Evidence = append(p.Evidence, e)
		}
		for si := range p.Sections {
			for pi := range p.Sections[si].Paragraphs {
				para := &p.Sections[si].Paragraphs[pi]
				cited := map[string]bool{}
				var evidence []Evidence
				cite := func(e Evidence) {
					key := e.Source + "\x00" + e.Quote
					if cited[key] {
						return
					}
					cited[key] = true
					if source, ok := data.Sources[e.Source]; ok {
						e.Role = source.Role
						e.TS = source.TS
					}
					evidence = append(evidence, e)
					add(e)
				}
				for _, e := range para.Evidence {
					cite(e)
				}
				for _, id := range para.Claims {
					if index, ok := claimIndex[id]; ok && data.Claims[index].Status == "active" {
						for _, e := range data.Claims[index].Evidence {
							cite(e)
						}
					}
				}
				para.Evidence = evidence
			}
		}
		var paragraphs []Paragraph
		for _, id := range p.Claims {
			index, ok := claimIndex[id]
			if !ok {
				continue
			}
			c := data.Claims[index]
			if c.Status != "active" {
				continue
			}
			for _, e := range c.Evidence {
				add(e)
			}
			paragraphs = append(paragraphs, Paragraph{Text: "[" + c.Kind + "] " + c.Text, Claims: []string{c.ID}, Evidence: c.Evidence})
		}
		if len(p.Sections) == 0 && len(paragraphs) > 0 {
			p.Sections = []Section{{Heading: "Recorded knowledge", Paragraphs: paragraphs}}
		}
	}

	sort.SliceStable(s.Pages, func(i, j int) bool { return s.Pages[i].Title < s.Pages[j].Title })
	return s, nil
}
func (v *Vault) Read(id string) (Page, error) {
	s, e := v.Snapshot()
	if e != nil {
		return Page{}, e
	}
	for _, p := range s.Pages {
		if p.ID == id {
			return p, nil
		}
	}
	return Page{}, os.ErrNotExist
}
func (v *Vault) Search(query string, limit int) ([]Result, error) {
	s, e := v.Snapshot()
	if e != nil {
		return nil, e
	}
	if limit <= 0 {
		return nil, nil
	}
	terms := strings.Fields(strings.ToLower(query))
	type hit struct {
		r Result
		n int
	}
	var hits []hit
	for _, p := range s.Pages {
		body := p.Title + " " + p.Scope
		for _, sec := range p.Sections {
			body += " " + sec.Heading
			for _, para := range sec.Paragraphs {
				body += " " + para.Text
			}
		}
		score := 0
		for _, term := range terms {
			if strings.Contains(strings.ToLower(body), term) {
				score++
			}
			if strings.Contains(strings.ToLower(p.Title), term) {
				score += 3
			}
		}
		if len(terms) > 0 && score == 0 {
			continue
		}
		snippet := p.Title
		if len(p.Sections) > 0 && len(p.Sections[0].Paragraphs) > 0 {
			snippet = p.Sections[0].Paragraphs[0].Text
		}
		rs := []rune(snippet)
		if len(rs) > 600 {
			snippet = string(rs[:600]) + "…"
		}
		hits = append(hits, hit{Result{p, snippet}, score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].n > hits[j].n })
	var result []Result
	for i, h := range hits {
		if i >= limit {
			break
		}
		result = append(result, h.r)
	}
	return result, nil
}
func runtime(profile string) (string, error) {
	var content strings.Builder
	err := fs.WalkDir(assets.Files, ".", func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		b, e := assets.Files.ReadFile(path)
		if e == nil {
			content.WriteString(path)
			content.Write(b)
		}
		return e
	})
	if err != nil {
		return "", err
	}
	root := filepath.Join(profile, "knowledge-runtime", hash(content.String())[:20])
	err = fs.WalkDir(assets.Files, ".", func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		dest := filepath.Join(root, path)
		if d.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		b, e := assets.Files.ReadFile(path)
		if e != nil {
			return e
		}
		if old, e := os.ReadFile(dest); e == nil && string(old) == string(b) {
			return nil
		}
		return writeBytes(dest, b)
	})
	return root, err
}
func writeBytes(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".asset-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(name, path)
}
func (v *Vault) Flush(ctx context.Context, key string, force bool) error {
	if !v.Enabled() {
		return ErrDisabled
	}
	root, e := runtime(v.profile)
	if e != nil {
		return e
	}
	args := []string{filepath.Join(root, "bridge.py"), "--root", v.root, "--project", v.project, "--model", config.KnowledgeModelAt(v.profile)}
	if force {
		args = append(args, "--force")
	}
	cmd := exec.CommandContext(ctx, "python3", args...)
	cmd.Env = append(os.Environ(), "OPENROUTER_API_KEY="+key, "PYTHONDONTWRITEBYTECODE=1")
	out, e := cmd.CombinedOutput()
	if e != nil {
		return fmt.Errorf("knowledge compiler (Python 3 required): %w: %s", e, strings.TrimSpace(string(out)))
	}
	return nil
}
