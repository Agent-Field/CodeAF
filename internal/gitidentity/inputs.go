package gitidentity

// A PROGRAM'S COPY CARRIES THE PERSON'S UNTRACKED FILES AS INPUTS, and one rule
// decides which of them its branch takes, read the same way by codeaf's
// finishing commit and by the program's own commits (internal/seniordev's
// recorder and its per-write commits):
//
//	AN INPUT THE RUN LEFT AS IT WAS STAYS OUT; ONE IT CHANGED IS ITS WORK.
//
// Leaving every input out lost work: a person who started a new parser.py and
// asked senior-dev to finish it got a branch with the tests and without the
// parser, and the finished function survived only in the run's log (review of
// #1674). Taking every input in put a credentials.json or a large local file on
// a branch that is always kept. What tells the two apart is whether the run
// touched the file, so each input's fingerprint is taken as it is copied in and
// compared whenever a commit is about to be made.
//
// A FINGERPRINT IS NEVER A GIT OBJECT. It is a SHA-256 of the bytes, taken
// outside git, so an input the run leaves alone never reaches the repository's
// object store — which a `git hash-object -w` or a staging would do.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// InputsEnv names, in the program's environment, the file listing its copy's
// inputs and their fingerprints ([WriteInputs]). It is senior-dev's name
// beside SENIOR_DEV_IGNORED_AT_START, which codeaf sets the same way.
const InputsEnv = "SENIOR_DEV_INPUTS_AT_START"

// Inputs is a copy's inputs: each path, relative to the copy's top and
// slash-separated, with its fingerprint when it was copied in. An empty
// fingerprint is an input whose copying had not finished, which is kept out.
type Inputs map[string]string

// Fingerprint is a file's fingerprint: the SHA-256 of its bytes, or of its
// target for a symbolic link, and false when there is no file there.
func Fingerprint(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", false
	}
	sum := sha256.New()
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return "", false
		}
		_, _ = io.WriteString(sum, "link\x00"+target)
		return hex.EncodeToString(sum.Sum(nil)), true
	}
	if !info.Mode().IsRegular() {
		return "", false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	if _, err := io.Copy(sum, file); err != nil {
		return "", false
	}
	return hex.EncodeToString(sum.Sum(nil)), true
}

// LeftAlone says path is one of the inputs and the run has not changed it in
// the copy at root: it still has the fingerprint it was copied in with, it is
// gone (an untracked file's deletion is nothing a branch can hold), or its
// fingerprint was never taken. A path that is not an input is never left
// alone in this sense — it is the run's, or the repository's.
func (in Inputs) LeftAlone(root, path string) bool {
	was, ok := in[path]
	if !ok {
		return false
	}
	if was == "" {
		return true
	}
	now, there := Fingerprint(filepath.Join(root, filepath.FromSlash(path)))
	return !there || now == was
}

// Changed is the inputs the run changed in the copy at root, in no order.
func (in Inputs) Changed(root string) []string {
	var changed []string
	for path := range in {
		if !in.LeftAlone(root, path) {
			changed = append(changed, path)
		}
	}
	return changed
}

// WriteInputs writes the inputs to file as NUL-separated fingerprint and path
// pairs, readable by [ReadInputs]; a path may hold any byte but NUL.
func WriteInputs(file string, in Inputs) error {
	var body strings.Builder
	for path, sum := range in {
		body.WriteString(sum + "\x00" + path + "\x00")
	}
	return os.WriteFile(file, []byte(body.String()), 0o600)
}

// ReadInputs reads what [WriteInputs] wrote. An empty name is no list at all,
// which is every run codeaf did not start in a copy.
func ReadInputs(file string) (Inputs, error) {
	if file == "" {
		return nil, nil
	}
	body, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSuffix(string(body), "\x00"), "\x00")
	if len(fields) == 1 && fields[0] == "" {
		return Inputs{}, nil
	}
	if len(fields)%2 != 0 {
		return nil, errors.New("the list of a copy's inputs is cut short")
	}
	in := Inputs{}
	for i := 0; i < len(fields); i += 2 {
		in[fields[i+1]] = fields[i]
	}
	return in, nil
}
