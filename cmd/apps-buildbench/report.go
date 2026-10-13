package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func printResult(r *result, verbose bool) {
	status := "PASS"
	if !r.Pass {
		status = "FAIL"
	}
	fmt.Printf("%-9s %-34s plan %5.1fs  write %5.1fs  fix %d (%5.1fs)  $%.4f  %s\n",
		r.Preset, r.Model, r.PlanSec, r.WriteSec, r.FixRounds, r.FixSec, r.Cost, status)
	if r.Error != "" {
		fmt.Println("  error:", r.Error)
	}
	if !verbose {
		return
	}
	printList("issue", r.Issues)
	printList("note", r.Notes)
}

func printList(label string, items []string) {
	for _, item := range items {
		fmt.Printf("  %s: %s\n", label, item)
	}
}

// save writes the app files and result.json under out/<model>/<preset>.
func save(out string, r *result) error {
	dir := filepath.Join(out, strings.ReplaceAll(r.Model, "/", "_"), r.Preset)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for path, data := range r.Files {
		target := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	summary, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "result.json"), summary, 0o644)
}
