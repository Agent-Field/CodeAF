package config

// THE SHAPE AN "ALWAYS" IS BANKED AS.
//
// A person who presses always on `git status --short` is not saying that one
// string of characters and nothing else; they are saying they are done being
// asked about git status. The card used to write the line down verbatim
// (approvalmemory.go's [RememberBashApproval]), which meant the next call —
// `git status --porcelain`, the same work with one more flag — asked again, and
// the always they pressed bought them one keystroke exactly once. A rule that
// answers only the line it was written for is a rule nobody presses twice.
//
// So the card offers SHAPES, and this file derives them. Three laws.
//
//   - THE SHAPES COME FROM THE LINE AND ARE SHOWN BEFORE THEY ARE WRITTEN. This
//     is a deriver, not an inference: it proposes, the person picks, and what
//     they picked is what is written. Nothing here widens anything on its own.
//   - THE LINE ITSELF IS ALWAYS ON OFFER, and it is always last. It is the
//     narrowest thing that can be written down and the one that needs no
//     thought, so it is the answer somebody reaches for when none of the wider
//     ones is what they meant.
//   - A SHAPE THAT PINS NOTHING DOWN IS NOT OFFERED AT ALL ([bashShapeHolds]).
//     A bare `*` is not an approval, it is the end of the question; and a shape
//     whose only word is sudo says the person approved elevation rather than a
//     command. Neither is a thing anybody means to press a letter for.

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/approval"
)

// bashShapeCount is how many shapes the card offers. Three is what a person can
// read across one line and choose between without re-reading, and the third of
// them is always the line itself — so the widening on offer is two shapes and a
// way out, which is a decision rather than a menu.
const bashShapeCount = 3

// BashShapes derives the shapes one command line could be remembered as, in the
// order the card offers them: the command's own words first, then the program
// alone, then the line itself.
//
// The order is not width. `git status*` is narrower than `git *` and leads
// anyway, because it is the shape a person pressing always on git status
// usually means, and the widest reading of what they did is the one that has to
// be read carefully rather than the one that is offered first. The LINE is last
// for the reason the head of this file gives.
//
// A COMPOUND LINE DERIVES NOTHING. An allow rule vouches only for a command it
// matches whole (internal/approval's bash.go), so every shape drawn off
// `cd /tmp && rm -rf build` would be a rule that cannot fire, and offering one
// would be offering somebody a choice between three things that do nothing.
//
// The result is never longer than [bashShapeCount], and where it is not empty
// its last entry is the trimmed line — which is what lets the card say "just
// this line" about that entry and mean it.
func BashShapes(line string) []string {
	line = strings.TrimSpace(line)
	if !bashShapeHolds(line) {
		return nil
	}
	shapes := make([]string, 0, bashShapeCount)
	add := func(shape string) {
		if !bashShapeHolds(shape) {
			return
		}
		for _, had := range shapes {
			if had == shape {
				return
			}
		}
		shapes = append(shapes, shape)
	}
	fields := strings.Fields(line)
	if run := verbRun(fields); len(run) >= 2 {
		add(strings.Join(run, " ") + "*")
	}
	if len(fields) > 0 && bareWord(fields[0]) {
		// THE PROGRAM NAME KEEPS ITS SPACE. `git*` is a shape about gitleaks and
		// git-lfs as much as about git, and a person reading it would not know
		// that. Past the program name the words are that program's own
		// vocabulary, where `git status*` is still a shape about git status.
		add(fields[0] + " *")
	}
	add(line)
	if len(shapes) > bashShapeCount {
		// The line is the entry that must survive a trim, so the cut is taken
		// out of the middle rather than off the end.
		shapes = append(shapes[:bashShapeCount-1], shapes[len(shapes)-1])
	}
	return shapes
}

// verbRun is the leading run of plain words — the program and the subcommands
// under it, `docker compose up` out of `docker compose up -d`.
//
// It stops at the first token that is not a bare word, and that token is nearly
// always the point where the line stops being about a KIND of work and starts
// being about this particular call: a flag, a path, a quoted argument, a
// wildcard, an assignment. Everything before it is the sentence a person would
// say out loud about what they just approved.
func verbRun(fields []string) []string {
	for index, field := range fields {
		if !bareWord(field) {
			return fields[:index]
		}
	}
	return fields
}

// bareWord reports whether a token is a plain command word: a letter, then
// letters, digits and the three punctuation marks command names carry. A token
// with a slash in it is a path, one with a quote in it is an argument, and one
// starting with '-' is a flag — none of them is part of the name of the work.
func bareWord(token string) bool {
	if token == "" {
		return false
	}
	for index := 0; index < len(token); index++ {
		char := token[index]
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z':
		case index > 0 && char >= '0' && char <= '9':
		case index > 0 && (char == '-' || char == '_' || char == '.'):
		default:
			return false
		}
	}
	return true
}

// bashShapeHolds reports whether a shape is worth writing into somebody's
// settings — the four refusals, and every one of them is a rule that would say
// less than the person pressing the key believes it says.
//
//   - IT HAS TO PIN A WORD DOWN. A shape made of wildcards and punctuation
//     answers for every command there is, and the row is a list of approvals
//     rather than a switch that turns approving off.
//   - SUDO IS NOT A COMMAND. A shape whose only word is sudo or doas says the
//     person approved running things as root, which is the opposite of what
//     they were being asked.
//   - A COMPOUND SHAPE CANNOT FIRE, on internal/approval's matching law, and a
//     rule that cannot fire is a line in somebody's settings claiming an
//     approval that does nothing.
//   - AN ESCAPE IS NOT TEXT. The line reached this surface from a model, and a
//     control byte in it is a rule nobody can read back in the settings sheet
//     and a row that repaints the terminal that prints it.
func bashShapeHolds(shape string) bool {
	shape = strings.TrimSpace(shape)
	if shape == "" || !approval.Vouchable(shape) {
		return false
	}
	for _, r := range shape {
		if r < ' ' || r == 0x7f {
			return false
		}
	}
	words := literalWords(shape)
	for _, word := range words {
		if word != "sudo" && word != "doas" {
			return true
		}
	}
	return false
}

// literalWords is what a shape actually names: its text with the wildcards
// taken out, kept only where a run of it carries a letter or a digit. It is how
// "does this pin anything down" is asked, and it is asked of the shape rather
// than of the line because a glob is the thing being written.
func literalWords(shape string) []string {
	fields := strings.Fields(strings.ReplaceAll(shape, "*", " "))
	words := make([]string, 0, len(fields))
	for _, field := range fields {
		if hasLetterOrDigit(field) {
			words = append(words, field)
		}
	}
	return words
}

func hasLetterOrDigit(text string) bool {
	for index := 0; index < len(text); index++ {
		char := text[index]
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			return true
		}
	}
	return false
}
