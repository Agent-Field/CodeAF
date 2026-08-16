// Package afield is the Go-only AgentField control-plane visibility layer:
// deterministic short labels for DAG nodes plus an asynchronous event/note
// reporter. It has no TypeScript counterpart and must never be imported by
// parity-frozen engine packages.
package afield

import (
	"encoding/json"
	"path"
	"strconv"
	"strings"
)

// maxLabelLen keeps reasoner ids readable in the control-plane trace column.
const maxLabelLen = 28

// normalTail are dangling words dropped from a label's end: word-count
// truncation leaves articles ("build calcsrv a") that carry no meaning.
var normalTail = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "of": true,
	"to": true, "for": true, "with": true, "in": true, "on": true, "by": true,
}

// Normalize renders a node label in the trace's single uniform format:
// lowercase kebab-case keeping letters, digits, and path glyphs ("./*"), so
// "Implement expr/ast parser" and "classify goal" render as
// "implement-expr/ast-parser" and "classify-goal". Every label leaves this
// package through Normalize; mixed-case prose never reaches the trace.
func Normalize(label string) string {
	var b strings.Builder
	pending := false
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '/', r == '*':
			if pending && b.Len() > 0 {
				b.WriteByte('-')
			}
			pending = false
			b.WriteRune(r)
		default:
			pending = true
		}
	}
	out := strings.NewReplacer("/-", "/", "-/", "/").Replace(b.String())
	return dropTail(strings.Trim(out, "-."))
}

func dropTail(s string) string {
	for {
		cut := strings.LastIndexByte(s, '-')
		if cut < 0 || !normalTail[s[cut+1:]] {
			return s
		}
		s = s[:cut]
	}
}

// ToolLabel derives a 2-3 word node name from a tool call, e.g.
// "read main.go", "go test", "grep TODO". Falls back to the tool name.
func ToolLabel(name, argsJSON string) string {
	args := map[string]any{}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	str := func(key string) string {
		value, _ := args[key].(string)
		return strings.TrimSpace(value)
	}
	label := ""
	switch name {
	case "read", "write", "edit":
		if file := str("filePath"); file != "" {
			label = name + " " + path.Base(file)
		}
	case "bash":
		label = CommandLabel(str("command"))
	case "grep", "glob":
		if pattern := str("pattern"); pattern != "" {
			label = name + " " + clip(pattern, 16)
		}
	case "apply_patch":
		if file := patchTarget(str("patch")); file != "" {
			label = "patch " + file
		} else {
			label = "apply patch"
		}
	case "task":
		intent := IntentLabel(str("description"))
		if intent == "" {
			intent = IntentLabel(str("prompt"))
		}
		if intent != "" {
			label = "task " + intent
		}
	case "plandb":
		if command := str("command"); command != "" {
			label = "plandb " + firstWord(command)
		}
	}
	if label == "" {
		label = strings.ReplaceAll(name, "_", " ")
	}
	return clip(Normalize(label), maxLabelLen)
}

// commandWrappers are tokens skipped before the command that names a bash
// invocation; env assignments (FOO=bar) are skipped independently.
var commandWrappers = map[string]bool{
	"sudo": true, "env": true, "command": true, "exec": true, "nice": true,
	"nohup": true, "time": true, "xvfb-run": true,
}

// subcommandTools name their action in the second token ("go test").
var subcommandTools = map[string]bool{
	"go": true, "git": true, "npm": true, "pnpm": true, "yarn": true, "bun": true,
	"cargo": true, "make": true, "docker": true, "kubectl": true, "pip": true,
	"pip3": true, "apt": true, "apt-get": true, "gh": true, "plandb": true,
	"python": true, "python3": true, "node": true, "bash": true, "sh": true,
}

// CommandLabel reduces a shell command line to its leading command and, when
// conventional, its subcommand: "cd x && go test ./... -v" -> "go test".
func CommandLabel(command string) string {
	fields := strings.Fields(command)
	for len(fields) > 0 {
		head := fields[0]
		switch {
		case strings.Contains(head, "=") && !strings.HasPrefix(head, "="):
			fields = fields[1:]
		case head == "cd" || head == "timeout":
			// Skip the wrapper plus its argument and any chain operator.
			rest := fields[1:]
			for len(rest) > 0 && rest[0] != "&&" && rest[0] != ";" {
				rest = rest[1:]
			}
			if len(rest) > 0 {
				rest = rest[1:]
			}
			fields = rest
		case commandWrappers[head]:
			fields = fields[1:]
		default:
			label := path.Base(head)
			if subcommandTools[label] && len(fields) > 1 &&
				!strings.HasPrefix(fields[1], "-") && !strings.Contains(fields[1], "/") {
				label += " " + fields[1]
			}
			return clip(Normalize(label), maxLabelLen)
		}
	}
	return "bash"
}

// leadIns are throat-clearing prefixes stripped (repeatedly) from assistant
// text before extracting an intent label. Longer phrases sort first so they
// win over their own prefixes.
var leadIns = []string{
	"i am going to", "i'm going to", "im going to", "i will now", "i need to",
	"we need to", "i want to", "let me start by", "i will", "i'll", "let me",
	"let's", "lets", "going to", "need to", "start by", "starting with",
	"first,", "first", "next,", "next", "now,", "now", "then,", "then",
	"okay,", "okay", "ok,", "ok", "sure,", "sure", "alright,", "alright",
	"great,", "great", "good,", "perfect,", "perfect", "done,", "so",
}

// IntentLabel compresses the opening of an assistant message into at most
// three significant words: "I'll fix the parser precedence bug first" ->
// "fix the parser". Returns "" when nothing usable remains.
func IntentLabel(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimLeft(text, "#*->`_~\"' \t")
	// Only the first line/sentence carries the intent.
	if cut := strings.IndexAny(text, "\n.!?:;("); cut > 0 {
		text = text[:cut]
	}
	for pass := 0; pass < 4; pass++ {
		lower := strings.ToLower(text)
		stripped := false
		for _, lead := range leadIns {
			if strings.HasPrefix(lower, lead+" ") || lower == lead {
				text = strings.TrimSpace(text[len(lead):])
				stripped = true
				break
			}
		}
		if !stripped {
			break
		}
	}
	// Markdown emphasis reads as noise in a node name wherever it appears.
	text = strings.NewReplacer("`", "", "*", "", "_", " ").Replace(text)
	words := strings.Fields(text)
	if len(words) > 3 {
		words = words[:3]
	}
	label := strings.Trim(strings.Join(words, " "), ",.!?:;\"'")
	return clip(Normalize(label), maxLabelLen)
}

// stageLabels maps codeaf pipeline stage keys to human-facing node names.
var stageLabels = map[string]string{
	"bootstrap":     "bootstrap",
	"pre-gates":     "pre gates",
	"classifier":    "classify goal",
	"product":       "product brief",
	"architecture":  "design architecture",
	"planner":       "plan tasks",
	"issue-writer":  "write issues",
	"plan-apply":    "apply plan",
	"scheduler":     "dispatch leaves",
	"stale-reaper":  "reap stale",
	"audit":         "audit",
	"fix-generator": "generate fixes",
	"pr-ready":      "pr gate",
	"entry-agent":   "entry agent",
	"root-cut":      "root cut",
	"resume":        "resume run",
}

// StageLabel names a pipeline stage node, appending the cycle number for
// stages that recur ("audit 2"). Unknown stages fall back to the raw key
// with dashes spaced.
func StageLabel(stage string, data map[string]any) string {
	label, ok := stageLabels[stage]
	if !ok {
		label = strings.ReplaceAll(stage, "-", " ")
	}
	if cycle, ok := numeric(data["cycle"]); ok && cycle > 0 {
		label += " " + strconv.Itoa(cycle)
	}
	return clip(Normalize(label), maxLabelLen)
}

func numeric(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}

// patchTarget extracts the first touched file from an apply_patch envelope.
func patchTarget(patch string) string {
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"*** Update File:", "*** Add File:", "*** Delete File:"} {
			if strings.HasPrefix(line, prefix) {
				return path.Base(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
			}
		}
	}
	return ""
}

func firstWord(s string) string {
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	clipped := s[:max]
	if idx := strings.LastIndexAny(clipped, " -"); idx > max/2 {
		clipped = clipped[:idx]
	}
	return dropTail(strings.TrimRight(clipped, " -./"))
}
