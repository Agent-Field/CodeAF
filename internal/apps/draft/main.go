package draft

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Main is `codeaf app <verb>`: exit 0 ok, 1 refused, 2 usage, 3 Builder not
// running. A write verb answers "ok <what changed>" on stdout; every failure
// is one or more "error: …" lines on stderr.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, strings.Join(Usages(), "\n"))
		return exitIf(len(args) == 0, ExitUsage)
	}
	name, v, rest, ok := lookup(args)
	if !ok || v.Internal {
		return fail(stderr, usage("unknown verb %q — the verbs are:\n%s", strings.Join(args[:1], " "), strings.Join(Usages(), "\n")))
	}
	text, err := dispatch(name, v, rest, stdin)
	if err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintln(stdout, text)
	return ExitOK
}

func exitIf(cond bool, code int) int {
	if cond {
		return code
	}
	return ExitOK
}

func fail(stderr io.Writer, err error) int {
	var e *Error
	if !errors.As(err, &e) {
		e = &Error{Code: ExitRefused, Lines: []string{err.Error()}}
	}
	fmt.Fprintln(stderr, e.Error())
	return e.Code
}

// dispatch runs one verb and returns the line to print. The three kinds of
// verb differ only in where the answer comes from.
func dispatch(name string, v Verb, args []string, stdin io.Reader) (string, error) {
	dir, err := findDir()
	if err != nil {
		return "", err
	}
	switch {
	case v.Read != nil:
		return readVerb(dir, v, args)
	case v.Remote:
		return remoteCall(dir, name, v, args, stdin)
	}
	return writeVerb(dir, name, v, args, stdin)
}

func writeVerb(dir, name string, v Verb, args []string, stdin io.Reader) (string, error) {
	body := ""
	if v.Body {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return "", usage("cannot read stdin: %v", err)
		}
		body = string(raw)
	}
	s, err := Open(dir)
	if err != nil {
		return "", err
	}
	e, err := s.Append(AuthorBuilder, name, args, body)
	if err != nil {
		return "", err
	}
	return "ok " + e.Line, nil
}

func readVerb(dir string, v Verb, args []string) (string, error) {
	s, err := Open(dir)
	if err != nil {
		return "", err
	}
	st, err := s.State()
	if err != nil {
		return "", err
	}
	return v.Read(st, s.Defaults(), args)
}

func remoteCall(dir, name string, v Verb, args []string, stdin io.Reader) (string, error) {
	if _, err := v.Parse(args, stdin); err != nil {
		return "", err
	}
	reply, err := call(dir, Request{Verb: name, Args: args})
	if err != nil {
		return "", err
	}
	if !reply.OK {
		return "", refuse("%s", strings.TrimPrefix(strings.TrimSpace(reply.Text), "error: "))
	}
	return strings.TrimRight(reply.Text, "\n"), nil
}

// findDir locates the draft folder: the override, then the folder of the
// plandb store the worker's shell already names, then the nearest folder above
// the working directory that holds a journal.
func findDir() (string, error) {
	if dir := os.Getenv(DirEnv); dir != "" {
		return dir, nil
	}
	if db := os.Getenv("PLANDB_DB"); db != "" {
		return filepath.Dir(db), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", usage("cannot find the draft: %v", err)
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, JournalFile)); err == nil {
			return dir, nil
		}
		if filepath.Dir(dir) == dir {
			return "", usage("no app draft found — set %s, or run inside a Builder task", DirEnv)
		}
	}
}

// Defaults reads defaults.json beside the journal, or gives zero values when
// the Builder saved none.
func (s *Store) Defaults() Defaults {
	var d Defaults
	if raw, err := os.ReadFile(s.path(DefaultsFile)); err == nil {
		_ = json.Unmarshal(raw, &d)
	}
	return d
}

// SaveDefaults writes defaults.json for `codeaf app show`, which cannot see
// the create sheet.
func (s *Store) SaveDefaults(d Defaults) error {
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(DefaultsFile), raw, 0o600)
}
