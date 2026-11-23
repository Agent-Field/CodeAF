package upgradenotice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Standing inventories the old chat items without changing or running any of them.
// Headless work cannot acknowledge news that only an interactive launch can show.
func Standing(home string, interactive bool) (string, error) {
	if !interactive {
		return "", nil
	}
	ack := filepath.Join(home, "v3", "standing-upgrade-noticed-v1.json")
	var held struct {
		Key string `json:"key"`
	}
	if raw, err := os.ReadFile(ack); err == nil && json.Unmarshal(raw, &held) == nil && held.Key == "standing-v1" {
		return "", nil
	}
	dir := filepath.Join(home, "v3", "standing")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var report strings.Builder
	report.WriteString("Standing orders and rules from an earlier version of codeaf were not carried over.\nRecreate schedules in /automations and rules with /remember.\nPaused and retired items below remain inactive.\n\n")
	orders, rules, paused, retired, unknown, files := 0, 0, 0, 0, 0, 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		files++
		source := filepath.Join(dir, entry.Name())
		fmt.Fprintf(&report, "Source: %s\n", source)
		raw, readErr := os.ReadFile(source)
		var item struct {
			Schema    int    `json:"schema"`
			Words     string `json:"words"`
			Workspace string `json:"workspace"`
			Status    string `json:"status"`
			Brief     struct {
				Prompt string `json:"prompt"`
			} `json:"brief"`
			When struct {
				Kind string `json:"kind"`
			} `json:"when"`
		}
		if readErr != nil {
			unknown++
			report.WriteString("unreadable file\n\n")
			continue
		}
		if json.Unmarshal(raw, &item) != nil {
			unknown++
			report.WriteString("unreadable JSON\n")
			report.Write(raw)
			report.WriteString("\n\n")
			continue
		}
		fmt.Fprintf(&report, "Words: %s\nInstruction: %s\nWorkspace: %s\nStatus: %s\nSchedule and original document:\n%s\n", item.Words, item.Brief.Prompt, item.Workspace, item.Status, raw)
		known := true
		if item.Schema < 1 || item.Schema > 4 {
			fmt.Fprintf(&report, "unknown schema %d\n", item.Schema)
			known = false
		}
		switch item.When.Kind {
		case "hold", "at", "every", "probe", "file", "idle":
		default:
			report.WriteString("unknown kind\n")
			known = false
		}
		switch item.Status {
		case "active", "paused", "retired":
		default:
			report.WriteString("unknown status\n")
			known = false
		}
		if !known {
			unknown++
		} else if item.Status == "paused" {
			paused++
		} else if item.Status == "retired" {
			retired++
		} else if item.When.Kind == "hold" {
			rules++
		} else {
			orders++
		}
		report.WriteByte('\n')
	}
	if files == 0 {
		return "", nil
	}
	var counts []string
	if orders > 0 {
		counts = append(counts, counted(orders, "standing order"))
	}
	if rules > 0 {
		counts = append(counts, counted(rules, "rule"))
	}
	if paused > 0 {
		counts = append(counts, fmt.Sprintf("%d paused", paused))
	}
	if retired > 0 {
		counts = append(counts, fmt.Sprintf("%d retired", retired))
	}
	if unknown > 0 {
		counts = append(counts, counted(unknown, "unreadable or unknown-format file"))
	}
	reportPath := filepath.Join(home, "v3", "standing-upgrade-report.txt")
	line := fmt.Sprintf("Found %s from an earlier version of codeaf. They were not carried over. Recreate schedules in /automations and rules with /remember. Your list is in %s.", strings.Join(counts, " and "), Path(reportPath))
	return Once(ack, "standing-v1", reportPath, []byte(report.String()), line)
}

func counted(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%d %s", n, noun)
}
