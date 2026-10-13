package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

type snapshot map[string][]bridge.Record

// result keeps execution and data success separate: a polite refusal can be
// valid speech while leaving the owner's requested change undone. Read tasks
// check that the agent used a records read and left the seeded content alone;
// the full reply and snapshots let a person judge factual quality blind.
type result struct {
	Model       string              `json:"model"`
	App         string              `json:"app"`
	Task        string              `json:"task"`
	Text        string              `json:"task_text"`
	OK          bool                `json:"ok"`
	Error       string              `json:"error,omitempty"`
	Seconds     float64             `json:"wall_seconds"`
	Cost        float64             `json:"cost_usd"`
	Tools       int                 `json:"tool_calls"`
	Final       string              `json:"final_reply"`
	DataChanged bool                `json:"data_changed"`
	DataOK      bool                `json:"data_ok"`
	Check       string              `json:"data_check"`
	Expected    map[string]any      `json:"expected_fields,omitempty"`
	Before      snapshot            `json:"before,omitempty"`
	After       snapshot            `json:"after,omitempty"`
	Events      []bridge.AgentEvent `json:"events,omitempty"`
}

func printTable(results []result) {
	fmt.Printf("%-9s %-34s %-16s %7s %10s %5s %-5s %s\n", "APP", "MODEL", "TASK", "SECONDS", "USD", "TOOLS", "OK", "DATA")
	for _, r := range results {
		status, data := "ok", "pass"
		if !r.OK {
			status = "error"
		}
		if !r.DataOK {
			data = "fail"
		}
		fmt.Printf("%-9s %-34s %-16s %7.1f %10.6f %5d %-5s %s\n", r.App, r.Model, r.Task, r.Seconds, r.Cost, r.Tools, status, data)
		if r.Error != "" {
			fmt.Println("  error:", r.Error)
		}
	}
}

// Numbered filenames reveal neither model nor task to the person reviewing
// replies. Identity remains in the JSON for joining their scores afterwards.
func saveResult(dir string, index int, r result) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fmt.Sprintf("%03d.jsonl", index+1)), append(raw, '\n'), 0o644)
}
