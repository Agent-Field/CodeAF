package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// combine reads the results.jsonl of several runs into one row list, marks the
// apps in b7 as earlier calls-road failures, and puts those first.
func combine(dirs []string, b7 string) ([]row, error) {
	marked := map[string]bool{}
	for _, id := range strings.Split(b7, ",") {
		marked[strings.TrimSpace(id)] = true
	}
	var rows []row
	for _, dir := range dirs {
		more, err := readRows(filepath.Join(dir, "results.jsonl"))
		if err != nil {
			return nil, err
		}
		rows = append(rows, more...)
	}
	for i := range rows {
		rows[i].B7 = marked[rows[i].ID]
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].B7 && !rows[j].B7 })
	return rows, nil
}

func readRows(path string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []row
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r row
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, sc.Err()
}
