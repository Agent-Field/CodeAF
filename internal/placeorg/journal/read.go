package journal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Replay reads every segment in dir and returns the ops journaled for run,
// in the order they were written. A trailing partial line — the bytes a
// crash left after the last newline — is not an entry, and it does not hide
// the finished lines before it. Lines for other runs are skipped. The
// self-check compares this list with the plan.
func Replay(dir, run string) ([]placeorg.Op, error) {
	paths, err := segments(dir)
	if err != nil {
		return nil, err
	}
	var ops []placeorg.Op
	for _, path := range paths {
		ops, err = opsIn(path, run, ops)
		if err != nil {
			return nil, err
		}
	}
	return ops, nil
}

// segments lists sealed files oldest first, then the live file. A missing
// directory is an empty journal: nothing has been written yet, which is a
// readable record of no runs.
func segments(dir string) ([]string, error) {
	list, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var sealed []int
	live := false
	for _, e := range list {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == FileName {
			live = true
			continue
		}
		if n, ok := sealedIndex(name); ok {
			sealed = append(sealed, n)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sealed)))
	out := make([]string, 0, len(sealed)+1)
	for _, n := range sealed {
		out = append(out, filepath.Join(dir, sealedName(n)))
	}
	if live {
		out = append(out, filepath.Join(dir, FileName))
	}
	return out, nil
}

func sealedName(n int) string {
	return FileName + "." + strconv.Itoa(n)
}

// sealedIndex accepts places-journal.jsonl.N for N >= 1, and rejects
// suffixes Atoi would forgive ("01", "+1") so a stray file is not a segment.
func sealedIndex(name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, FileName+".")
	if !ok || rest == "" {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 || strconv.Itoa(n) != rest {
		return 0, false
	}
	return n, true
}

// shiftSealed renames N → N+1 from the oldest down, then the live file to
// .1. Oldest first means a failure stops before the live file moves, except
// on the last rename.
func shiftSealed(dir string) error {
	highest, err := highestSealed(dir)
	if err != nil {
		return err
	}
	for i := highest; i >= 1; i-- {
		from := filepath.Join(dir, sealedName(i))
		to := filepath.Join(dir, sealedName(i+1))
		if err := os.Rename(from, to); err != nil {
			return err
		}
	}
	return os.Rename(filepath.Join(dir, FileName), filepath.Join(dir, sealedName(1)))
}

func highestSealed(dir string) (int, error) {
	list, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, e := range list {
		if n, ok := sealedIndex(e.Name()); ok && n > max {
			max = n
		}
	}
	return max, nil
}

func opsIn(path, run string, dst []placeorg.Op) ([]placeorg.Op, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	n := 0
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("placeorg/journal: %s: %w", path, err)
		}
		if len(line) > 0 && line[len(line)-1] == '\n' {
			n++
			op, ok, perr := opFrom(line, run)
			if perr != nil {
				return nil, fmt.Errorf("placeorg/journal: %s:%d: %w", path, n, perr)
			}
			if ok {
				dst = append(dst, op)
			}
		}
		if err == io.EOF {
			return dst, nil
		}
	}
}

func opFrom(line []byte, run string) (placeorg.Op, bool, error) {
	line = bytes.TrimRight(line, "\r\n")
	if len(bytes.TrimSpace(line)) == 0 {
		return placeorg.Op{}, false, nil
	}
	var e placeorg.Entry
	if err := json.Unmarshal(line, &e); err != nil {
		return placeorg.Op{}, false, err
	}
	if e.Kind != placeorg.EntryOp || e.Run != run || e.Op == nil {
		return placeorg.Op{}, false, nil
	}
	return *e.Op, true, nil
}
