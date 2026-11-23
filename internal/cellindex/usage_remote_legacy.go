package cellindex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// Older task events carried the source ledger's end offset instead of a call
// identity. On the worker computer that durable event is an exact link back
// to the original row; matching prices or timestamps alone would guess.
func restoreRemoteUsageIDs(path string, lines []session.UsageLine) {
	wanted := map[string][]int{}
	for i, line := range lines {
		if line.CallID != "" || line.RemoteEvent == "" {
			continue
		}
		attempt, _, ok := strings.Cut(line.RemoteEvent, ":")
		if !ok || !strings.HasPrefix(attempt, "att_") || filepath.Base(attempt) != attempt {
			continue
		}
		wanted[attempt] = append(wanted[attempt], i)
	}
	if len(wanted) == 0 {
		return
	}
	offsets := map[int64][]int{}
	for attempt, indices := range wanted {
		f, err := os.Open(filepath.Join(filepath.Dir(path), "taskdoor", attempt, "events.jsonl"))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var event struct {
				Type    string `json:"t"`
				Attempt string `json:"attempt"`
				Seq     uint64 `json:"seq"`
				Source  string `json:"source"`
				Offset  int64  `json:"offset"`
			}
			if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Type != "spend" || event.Attempt != attempt || event.Source != "usage" || event.Offset <= 0 {
				continue
			}
			key := fmt.Sprintf("%s:%d", attempt, event.Seq)
			for _, i := range indices {
				if lines[i].RemoteEvent == key {
					offsets[event.Offset] = append(offsets[event.Offset], i)
				}
			}
		}
		f.Close()
	}
	if len(offsets) == 0 {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var offset int64
	for scanner.Scan() {
		raw := scanner.Bytes()
		offset += int64(len(raw) + 1)
		indices := offsets[offset]
		if len(indices) == 0 {
			continue
		}
		var paid session.UsageLine
		if json.Unmarshal(raw, &paid) != nil {
			continue
		}
		for _, i := range indices {
			incoming := &lines[i]
			if incoming.Model == paid.Model && incoming.Input == paid.Input && incoming.Output == paid.Output && incoming.USD == paid.USD {
				incoming.CallID = session.UsageCallID(paid)
			}
		}
	}
}
