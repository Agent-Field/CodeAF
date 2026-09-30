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
//
// THE SHARED HALF LIVES OVER A PLAIN STRING ([requestDisplayFor]) because the
// reader is no longer the transcript's alone: a run's plan page holds the same
// generated work order as its task's brief, and a person who opens that page
// must not read the machinery the transcript already reshapes for them. The
// transcript's own caller is untouched below — same entry, same gate, same
// drawing as before the half was lifted out.
func requestDisplayText(e *entry) string {
	if !e.brief {
		return e.text
	}
	return requestDisplayFor(e.text)
}

// requestDisplayFor is the reader over the text itself: the recognition, the
// plain headings, and this task's own work first, for every surface that holds
// a stored work order. A text that is not the generated document is returned
// unchanged — a person's own brief, typed after `/task` or written by hand, is
// drawn exactly as it was given, and no caller needs to know the difference
// before it calls.
func requestDisplayFor(text string) string {
	sections, ok := requestSections(text)
	if !ok {
		return text
	}
	out := make([]string, len(sections))
	for i, s := range sections {
		out[i] = s.name + "\n" + s.body
	}
	return strings.Join(out, "\n\n")
}

// requestSection is one part of a generated work order as a person reads it:
// its plain heading and its body.
type requestSection struct{ name, body string }

// requestSections is the generated work order in the order a person reads it —
// this task's own work first, then what it inherited — each part under its
// plain heading, and false for a text that is not the generated document.
// [requestDisplayFor] joins them into one string; a program's brief page draws
// them as a document (programbrief.go).
func requestSections(text string) ([]requestSection, bool) {
	if !canonicalTaskRequest(text) {
		return nil, false
	}
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
	var sections []requestSection
	var current requestSection
	flush := func() {
		if current.name != "" {
			current.body = strings.TrimSpace(current.body)
			sections = append(sections, current)
		}
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		name, known := labels[line]
		if known && (i == 0 || lines[i-1] == "") {
			flush()
			current = requestSection{name: name}
			continue
		}
		current.body += line + "\n"
	}
	flush()
	var out []requestSection
	primary := "Original request"
	for _, s := range sections {
		if s.name == "Task request" {
			primary = s.name
			break
		}
	}
	emit := func(s requestSection) {
		if s.name == "Original request" {
			// The priority rule is still available, after the actual quotation.
			if rule, body, ok := strings.Cut(s.body, "\n\n"); ok {
				out = append(out, requestSection{s.name, body}, requestSection{"Context", rule})
				return
			}
		}
		out = append(out, s)
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
	return out, true
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
