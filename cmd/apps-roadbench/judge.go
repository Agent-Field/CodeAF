package main

import (
	"os/exec"
	"strings"
)

// runJudge calls `<judge> <app-id> <dir>` and reads the verdict. The judge
// gets the files and the app id only, never the road.
func runJudge(judge, id, dir string) (verdict, reason string) {
	fields := strings.Fields(judge)
	if len(fields) == 0 {
		return "pending", ""
	}
	out, err := exec.Command(fields[0], append(fields[1:], id, dir)...).CombinedOutput()
	if err != nil {
		return "pending", "judge failed: " + firstLine(string(out))
	}
	return parseVerdict(string(out))
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// parseVerdict finds the first useful|thin|useless word; the rest of the
// output is the reason.
func parseVerdict(out string) (verdict, reason string) {
	for _, word := range strings.Fields(strings.ToLower(out)) {
		w := strings.Trim(word, ".,:;()[]\"'`*#")
		if w == "useful" || w == "thin" || w == "useless" {
			return w, firstLine(out)
		}
	}
	return "pending", "no verdict in judge output"
}
