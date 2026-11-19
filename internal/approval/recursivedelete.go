package approval

import (
	"path"
	"strings"
)

// ── recursive deletes outside the agent's folder ────────────────────────────
//
// THE TABLE IN bash.go CAUGHT `rm -rf /tmp/site_check` AND MISSED `rm -r
// /tmp/site_check`. It matched a spelling, so the same act with the `-f` left
// off, the flags split (`-r -f`), the letters swapped (`-Rf`), the long form
// (`--recursive`), or a tree removed through `find -delete` or `xargs rm -r`
// ran under an allow-all policy without a question. Canvas agents on two
// models deleted scratch folders in /tmp that way on 2026-11-18.
//
// So this reads the command instead of matching it. A recursive delete is
// found by what it does, however its flags are spelled, and then each target
// is placed:
//
//	inside   a relative path that stays under the folder the command started
//	         in, which is the agent's own folder: `rm -r build`, `rm -rf
//	         dist/*`, `find . -name '*.tmp' -delete`. These stay ask-free,
//	         because cleaning up its own work is most of what an agent deletes.
//	outside  everything else: an absolute path, a home path (`~`), a path
//	         built from a variable or a substitution (it cannot be placed
//	         without running it), a `..` that climbs out, and any relative
//	         path after a `cd` that left the folder.
//
// THIS GATE DOES NOT KNOW WHERE THE AGENT'S FOLDER IS. It is asked about a
// command line and nothing else, so an absolute path is outside even when it
// happens to name the agent's own folder. That costs one question on an
// unusual spelling of an ordinary cleanup, and it keeps the answer a property
// of the line a person can read on the question card.

// recursiveDeleteOutside reports the first command on the line that removes a
// tree outside the folder the line starts in, named the way [criticalName]
// names a table hit.
func recursiveDeleteOutside(command string) (string, bool) {
	return recursiveDeleteIn(command, 0)
}

// shellNesting bounds how many `sh -c '…'` layers are read. A line nested
// deeper than this is unusual enough that it is treated as unreadable, and an
// unreadable delete is an outside one.
const shellNesting = 3

func recursiveDeleteIn(command string, depth int) (string, bool) {
	parts := SplitBashCommand(command)
	// here is where the line stands relative to the folder it started in, as a
	// cleaned relative path; away is set once a `cd` has left that folder, and
	// from then on no relative path on the line can be placed inside it.
	here, away := ".", false
	for index, part := range parts {
		for _, words := range CommandWords(part.Command) {
			words = commandProper(withoutRedirects(words))
			if len(words) == 0 {
				continue
			}
			switch program := path.Base(words[0]); program {
			case "cd", "pushd":
				here, away = moveTo(here, away, words[1:])
			case "rm":
				if targets, recursive := rmTargets(words[1:]); recursive && !allInside(here, away, targets) {
					return criticalName(part.Command), true
				}
			case "find":
				if roots, deletes := findDeletes(words[1:]); deletes && !allInside(here, away, roots) {
					return criticalName(part.Command), true
				}
			case "xargs":
				if !xargsRemovesTrees(words[1:]) {
					continue
				}
				// The paths arrive on stdin, so they can be placed only when
				// the command feeding the pipe is a find whose own roots can.
				if index > 0 && parts[index-1].Separator == "|" {
					if roots, ok := findRoots(parts[index-1].Command); ok && allInside(here, away, roots) {
						continue
					}
				}
				return criticalName(strings.TrimSpace(command)), true
			case "sh", "bash", "zsh", "dash":
				script, ok := shellScript(words[1:])
				if !ok {
					continue
				}
				if depth >= shellNesting {
					return criticalName(part.Command), true
				}
				// The inner script starts where the outer line stands, so a
				// script after a `cd /tmp` is read as already away.
				switch {
				case away:
					script = "cd / && " + script
				case here != ".":
					script = "cd " + here + " && " + script
				}
				if hit, ok := recursiveDeleteIn(script, depth+1); ok {
					return hit, true
				}
			}
		}
	}
	return "", false
}

// withoutRedirects drops the redirections CommandWords keeps as words, with
// the file each one names and the descriptor number written before it, so
// `rm -r build 2>/dev/null` is not read as deleting /dev/null.
func withoutRedirects(words []string) []string {
	kept := make([]string, 0, len(words))
	for index := 0; index < len(words); index++ {
		word := words[index]
		if word == "" || (word[0] != '>' && word[0] != '<') {
			kept = append(kept, word)
			continue
		}
		if last := len(kept) - 1; last >= 1 && strings.Trim(kept[last], "0123456789") == "" {
			kept = kept[:last]
		}
		index++
	}
	return kept
}

// commandProper drops what stands in front of the program that runs: variable
// assignments and the wrappers that only change how it runs (`sudo`, `env`,
// `command`, `nohup`, `nice`, `time`, `timeout`, `exec`, `busybox`), so `sudo
// -u root rm -r /srv/x` is read as the `rm` it is.
func commandProper(words []string) []string {
	for len(words) > 0 {
		word := words[0]
		if isAssignment(word) {
			words = words[1:]
			continue
		}
		switch path.Base(word) {
		case "sudo", "doas":
			words = skipOptions(words[1:], "u", "g", "C", "D", "h", "p", "r", "t", "U")
		case "env":
			words = skipOptions(words[1:], "u", "C", "S")
		case "nice":
			words = skipOptions(words[1:], "n")
		case "timeout":
			words = skipOptions(words[1:], "s", "k")
			if len(words) > 0 {
				// The duration.
				words = words[1:]
			}
		case "command", "builtin", "exec", "nohup", "time", "busybox":
			words = skipOptions(words[1:])
		default:
			return words
		}
	}
	return words
}

// skipOptions drops leading option words, and the value after any short option
// named in valued when it is written as its own word (`-u root`).
func skipOptions(words []string, valued ...string) []string {
	for len(words) > 0 && strings.HasPrefix(words[0], "-") && words[0] != "-" {
		option := words[0]
		words = words[1:]
		if option == "--" {
			break
		}
		for _, name := range valued {
			if option == "-"+name && len(words) > 0 {
				words = words[1:]
			}
		}
	}
	return words
}

func isAssignment(word string) bool {
	name, _, found := strings.Cut(word, "=")
	if !found || name == "" {
		return false
	}
	for index, char := range name {
		letter := char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
		if !letter && (index == 0 || char < '0' || char > '9') {
			return false
		}
	}
	return true
}

// rmTargets reads an rm's arguments: whether any flag asks for a recursive
// delete, however it is spelled, and the paths it names.
func rmTargets(args []string) (targets []string, recursive bool) {
	options := true
	for _, arg := range args {
		switch {
		case options && arg == "--":
			options = false
		case options && arg == "--recursive":
			recursive = true
		case options && strings.HasPrefix(arg, "--"):
		case options && strings.HasPrefix(arg, "-") && arg != "-":
			if strings.ContainsAny(arg[1:], "rR") {
				recursive = true
			}
		default:
			targets = append(targets, arg)
		}
	}
	return targets, recursive
}

// findDeletes reads a find's arguments: the roots it walks, and whether its
// expression deletes what it finds, by `-delete` or by an `-exec rm`.
func findDeletes(args []string) (roots []string, deletes bool) {
	roots, expression := splitFind(args)
	for index, word := range expression {
		switch word {
		case "-delete":
			deletes = true
		case "-exec", "-execdir", "-ok", "-okdir":
			if inner := commandProper(expression[index+1:]); len(inner) > 0 && path.Base(inner[0]) == "rm" {
				deletes = true
			}
		}
	}
	return roots, deletes
}

// findRoots reads the roots of a find standing alone, for the xargs case.
func findRoots(command string) ([]string, bool) {
	segments := CommandWords(command)
	if len(segments) != 1 {
		return nil, false
	}
	words := commandProper(segments[0])
	if len(words) == 0 || path.Base(words[0]) != "find" {
		return nil, false
	}
	roots, _ := splitFind(words[1:])
	return roots, true
}

// splitFind separates find's starting points from its expression. Its leading
// -H/-L/-P options are not roots, and a find given no root walks ".".
func splitFind(args []string) (roots, expression []string) {
	for len(args) > 0 && (args[0] == "-H" || args[0] == "-L" || args[0] == "-P") {
		args = args[1:]
	}
	index := 0
	for index < len(args) && !strings.HasPrefix(args[index], "-") && args[index] != "(" && args[index] != "!" {
		index++
	}
	roots, expression = args[:index], args[index:]
	if len(roots) == 0 {
		roots = []string{"."}
	}
	return roots, expression
}

// xargsRemovesTrees reports whether the command xargs runs is a recursive rm or
// a deleting find. Its own options are skipped by looking for the program by
// name, because several of them take a value and none of them is `rm`.
func xargsRemovesTrees(args []string) bool {
	for index, word := range args {
		if strings.HasPrefix(word, "-") {
			continue
		}
		inner := commandProper(args[index:])
		if len(inner) == 0 {
			return false
		}
		switch path.Base(inner[0]) {
		case "rm":
			_, recursive := rmTargets(inner[1:])
			return recursive
		case "find":
			_, deletes := findDeletes(inner[1:])
			return deletes
		}
	}
	return false
}

// shellScript is the script of `sh -c '<script>'`.
func shellScript(args []string) (string, bool) {
	for index, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return "", false
		}
		if strings.Contains(arg[1:], "c") && !strings.HasPrefix(arg, "--") && index+1 < len(args) {
			return args[index+1], true
		}
	}
	return "", false
}

// moveTo follows one cd. A relative step keeps the line placeable while it
// stays under the starting folder; anything else, including a bare `cd` (home)
// and `cd -`, leaves it.
func moveTo(here string, away bool, args []string) (string, bool) {
	args = skipOptions(args)
	if len(args) == 0 {
		return here, true
	}
	if away {
		return here, true
	}
	next, inside := placeRelative(here, args[0])
	if !inside {
		return here, true
	}
	return next, false
}

// allInside reports whether every target is a relative path under the folder
// the line started in, read from where the line stands now. An rm with no
// target it can read (its target was a substitution) is not inside.
func allInside(here string, away bool, targets []string) bool {
	if len(targets) == 0 || away {
		return false
	}
	for _, target := range targets {
		if _, inside := placeRelative(here, target); !inside {
			return false
		}
	}
	return true
}

// placeRelative joins a target onto where the line stands and reports whether
// the result is still under the starting folder. A target that starts at the
// root or at home, names a variable, or has its words expanded by the shell in
// a way this reader does not follow, cannot be placed and is not inside.
func placeRelative(here, target string) (string, bool) {
	if target == "" || target == "-" || strings.HasPrefix(target, "/") || strings.HasPrefix(target, "~") || strings.ContainsAny(target, "$`") {
		return here, false
	}
	joined := path.Clean(path.Join(here, target))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return here, false
	}
	return joined, true
}
