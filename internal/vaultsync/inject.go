package vaultsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// ErrNoIdentity is the one sentence a machine without an identity gets in place
// of a cell's secrets.
var ErrNoIdentity = errors.New("this machine has no identity yet: codeaf identity import")

// EntrySource is what Inject reads from the vault: a project's secrets in one
// stable order. *keys.Vault provides it.
type EntrySource interface {
	Entries(project string) ([]keys.Entry, error)
}

// Injection says what one Inject did, by secret name only.
type Injection struct {
	Written []string // names appended to .env
	Skipped []string // names the person's own .env line already sets to another value
}

// Inject is the handoff.Taker.After hook: it writes the cell's secrets into the
// cell's workspace as .env and tells Notify, in one sentence, what it left alone.
func (s Syncer) Inject(ctx context.Context, c cell.Cell) error {
	in, err := s.Apply(ctx, c)
	if err == nil && len(in.Skipped) > 0 && s.Notify != nil {
		s.Notify(".env keeps your own value for " + strings.Join(in.Skipped, ", "))
	}
	return err
}

// Apply is Inject with its report returned. It refuses before touching the
// disk when the machine has no identity, and it never writes when nothing is
// missing from .env.
func (s Syncer) Apply(_ context.Context, c cell.Cell) (Injection, error) {
	src, ok := s.Vault.(EntrySource)
	if s.CellKeyID == "" || !ok {
		return Injection{}, ErrNoIdentity
	}
	entries, err := src.Entries(keys.ScopeOf(c))
	if err != nil {
		return Injection{}, err
	}
	path := filepath.Join(c.Root, ".env")
	have, err := readEnv(path)
	if err != nil {
		return Injection{}, err
	}
	in, add := plan(entries, keys.DotenvValues(have))
	return in, appendEnv(path, have, add)
}

// plan splits the entries into lines to add and names to skip. A name .env
// already sets to the vault's value is in sync and neither; a name set to
// another value is the person's, and is skipped. A value that cannot sit on one
// line is skipped too, since writing it would corrupt the next line.
func plan(entries []keys.Entry, set map[string]string) (Injection, []byte) {
	var in Injection
	var add strings.Builder
	for _, e := range entries {
		cur, present := set[e.Name]
		switch {
		case present && cur == e.Value:
		case present || strings.ContainsAny(e.Value, "\r\n"):
			in.Skipped = append(in.Skipped, e.Name)
		default:
			add.WriteString(line(e) + "\n")
			in.Written = append(in.Written, e.Name)
		}
	}
	return in, []byte(add.String())
}

// line is NAME=value, quoted only when a reader would otherwise strip or
// misread the value.
func line(e keys.Entry) string {
	v := e.Value
	if v != strings.TrimSpace(v) || strings.HasPrefix(v, `"`) || strings.HasPrefix(v, "'") {
		v = `"` + v + `"`
	}
	return e.Name + "=" + v
}

func readEnv(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(raw), err
}

// appendEnv adds lines after what the person wrote, on a fresh line, and
// creates a new file at mode 0600. An existing file keeps its mode.
func appendEnv(path, have string, add []byte) error {
	if len(add) == 0 {
		return nil
	}
	if have != "" && !strings.HasSuffix(have, "\n") {
		add = append([]byte("\n"), add...)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(add)
	return errors.Join(err, f.Close())
}
