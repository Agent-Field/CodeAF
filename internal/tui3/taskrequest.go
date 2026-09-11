package tui3

import "strings"

// A worker's first journal message is generated context, not a fresh utterance
// by the person. Recognize only the canonical opening document, and only at
// the room replay boundary. Later wake/checkpoint notes keep their own lane.
const taskRequestAsk = "WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS"

func canonicalTaskRequest(text string) bool {
	return strings.HasPrefix(text, taskRequestAsk+"\nThis is the message ") ||
		strings.HasPrefix(text, "THE WORK\n\n")
}

// requestDisplayText is presentation only. The entry keeps the exact journal
// payload for identity and refresh; the model's input never changes.
// Put this task's own work first so a child doesn't open on its parent's whole
// project. All section bodies and context rules remain available on expansion.
func requestDisplayText(e *entry) string {
	if !e.brief || !canonicalTaskRequest(e.text) {
		return e.text
	}
	type section struct{ name, body string }
	labels := map[string]string{
		"SOME OF WHAT WAS SAID AROUND THIS WORK":                      "Conversation context",
		"CALLS THAT HAVE ALREADY RUN":                                 "Prior evidence",
		"WHAT THIS BRIEF ASSUMES, AND WAS CHECKED BEFORE YOU STARTED": "Verified assumptions",
		taskRequestAsk:    "Original request",
		"THE WORK":        "Task request",
		"WHAT TO PRODUCE": "Deliverable",
		"DONE WHEN":       "Completion criteria",
		"THE FOLDER THIS WORK IS ABOUT, AND YOUR OWN COPY OF IT": "Workspace",
		"THE PERSON'S ORIGINAL MESSAGE":                          "Original message reference",
	}
	var sections []section
	var current section
	flush := func() {
		if current.name != "" {
			current.body = strings.TrimSpace(current.body)
			sections = append(sections, current)
		}
	}
	lines := strings.Split(e.text, "\n")
	for i, line := range lines {
		name, known := labels[line]
		if known && (i == 0 || lines[i-1] == "") {
			flush()
			current = section{name: name}
			continue
		}
		current.body += line + "\n"
	}
	flush()
	var out []string
	primary := "Original request"
	for _, s := range sections {
		if s.name == "Task request" {
			primary = s.name
			break
		}
	}
	emit := func(s section) {
		if s.name == "Original request" {
			// The priority rule is still available, after the actual quotation.
			if rule, body, ok := strings.Cut(s.body, "\n\n"); ok {
				out = append(out, s.name+"\n"+body+"\n\nContext\n"+rule)
				return
			}
		}
		out = append(out, s.name+"\n"+s.body)
	}
	// Keep the assignment and acceptance together; inherited context follows.
	for _, name := range []string{primary, "Deliverable", "Completion criteria", "Workspace"} {
		for _, s := range sections {
			if s.name == name {
				emit(s)
			}
		}
	}
	for _, s := range sections {
		if s.name == primary {
			continue
		}
		switch s.name {
		case "Deliverable", "Completion criteria", "Workspace":
			continue
		}
		emit(s)
	}
	return strings.Join(out, "\n\n")
}

// Keep the request beside a bounded recent transcript. The explicit seam keeps
// a long task honest: expanding work reveals the retained tail, not lost rows.
func keepRoomTail(blocks []entry, tail int) []entry {
	if tail < 3 || len(blocks) <= tail {
		return keepTail(blocks, tail)
	}
	for _, e := range blocks[:len(blocks)-tail] {
		if e.brief {
			out := make([]entry, 0, tail)
			out = append(out, e, entry{kind: entrySeam, text: "Earlier work is outside this saved view."})
			return append(out, blocks[len(blocks)-(tail-2):]...)
		}
	}
	return keepTail(blocks, tail)
}
