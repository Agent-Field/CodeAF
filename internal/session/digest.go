package session

// digest.go is the transcript read as FACTS, for whoever has to rebuild what
// the harness keeps beside it (internal/cellindex).
//
// The transcript is the record; meta.json and the usage ledger are citations of
// it. This file answers "what would those citations say" from the record alone,
// in one pass and without building a conversation, so a cell materialized on a
// machine that never ran it can have them made again.

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"
)

// Digest is what one pass over a transcript yields.
type Digest struct {
	ID        string
	Workspace string
	Model     string
	Title     string
	Created   time.Time
	// LastUserAt is when the person last spoke; the session's own notes do not
	// count, as in [Meta.LastUserAt].
	LastUserAt time.Time
	// Usage is one row per journaled usage line, in file order.
	Usage []UsageLine

	placeholder string
}

// foldFunc folds one transcript line into a digest.
type foldFunc func(d *Digest, line []byte, e sessionEntry)

// digestFolds is the line types a digest reads. A type absent from it says
// nothing to a digest and is skipped.
var digestFolds = map[string]foldFunc{
	"session": foldHeader,
	"title":   foldTitle,
	"usage":   foldUsage,
	"message": foldMessage,
}

// ReadDigest reads the transcript at path. A missing file is an empty digest.
func ReadDigest(path string) (Digest, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Digest{}, nil
	}
	if err != nil {
		return Digest{}, err
	}
	defer file.Close()
	var d Digest
	scanner := bufio.NewScanner(file)
	// The buffer [Peek] takes: one pasted file must not end the scan.
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		d.fold(scanner.Bytes())
	}
	d.settle()
	return d, scanner.Err()
}

func (d *Digest) fold(line []byte) {
	var e sessionEntry
	if json.Unmarshal(line, &e) != nil {
		return
	}
	if fold := digestFolds[e.Type]; fold != nil {
		fold(d, line, e)
	}
}

// settle chooses the name: the earned title when there is a usable one, else
// the person's opening words, exactly the two names [LoadMeta] falls back
// through.
func (d *Digest) settle() {
	d.Title = healedTitle(d.Title)
	if d.Title == "" {
		d.Title = d.placeholder
	}
}

func foldHeader(d *Digest, line []byte, _ sessionEntry) {
	var h sessionHeader
	if json.Unmarshal(line, &h) != nil || d.ID != "" {
		return // the first header wins
	}
	d.ID, d.Workspace, d.Model = strings.TrimSpace(h.ID), h.Cwd, h.Model
	d.Created = parseStamp(h.Timestamp)
}

func foldTitle(d *Digest, _ []byte, e sessionEntry) {
	if title := strings.TrimSpace(e.Title); title != "" {
		d.Title = title
	}
}

func foldUsage(d *Digest, _ []byte, e sessionEntry) {
	if e.Usage == nil {
		return
	}
	u := e.Usage
	d.Usage = append(d.Usage, UsageLine{
		At: parseStamp(e.Timestamp), Model: u.Model, Role: u.Role, Calls: u.Calls,
		Input: u.Input, Output: u.Output, USD: u.CostUSD, Empty: u.Empty,
		Session: d.ID, Workspace: d.Workspace,
	})
}

func foldMessage(d *Digest, _ []byte, e sessionEntry) {
	if e.Role != "user" || e.Note {
		return
	}
	d.LastUserAt = parseStamp(e.Timestamp)
	if text := strings.TrimSpace(e.Content); text != "" && d.placeholder == "" {
		d.placeholder = placeholderTitle(text)
	}
}

func parseStamp(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// Spend is the transcript's own total: money and input plus output tokens, the
// two figures [Meta] stamps.
func (d Digest) Spend() (usd float64, tokens int) {
	for _, u := range d.Usage {
		usd += u.USD
		tokens += u.Input + u.Output
	}
	return usd, tokens
}

// Derive lays what the transcript says over m and returns it. Fields the
// transcript cannot know (effort, approval, referred places, working copies,
// archive marks) are m's and pass through untouched.
func (d Digest) Derive(m Meta) Meta {
	m.ID, m.Model = d.ID, d.Model
	if d.Workspace != "" { // a sealed journal names none: the summary already knows where it is here
		m.Workspace = d.Workspace
	}
	m.Title, m.Created, m.LastUserAt = d.Title, d.Created, d.LastUserAt
	m.SpentUSD, m.Tokens = d.Spend()
	return m
}
