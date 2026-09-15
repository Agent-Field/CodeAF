package tui3

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/tui2/modelui"
)

// WHAT WENT INTO A PICTURE, when somebody opens the step.
//
// The row above says which file came out and which image model drew it
// (toolstat.go's [app.pictureTarget]); this is the other half of the same
// question, and it is the half a person opens the step for — they want to read
// the prompt, because the prompt is the part they would change. It is drawn as
// PROSE and never as the JSON it arrived in: a person opening a row to read one
// sentence should not have to find it inside a payload, and every other
// expansion on this surface already shapes its tool's arguments for reading
// (toolview.go's [app.detailBody] — an edit's diff, a bash call's command).
//
// The two tiers are the row's own (toolview.go's parameter hierarchy). The
// prompt is the substance and takes the primary ink; everything that QUALIFIES
// it — the frame it was asked for, the pictures it worked from, the model that
// drew — recedes to dim, one fact per line.
//
// EVERYTHING HERE COMES OFF THE ROADS THE ROW ALREADY DRAWS FROM: the arguments
// session already sends for this call (Event.Args, bounded by its own
// argsLimit and marked where it was cut, so a prompt too long to carry arrives
// saying so and is never fetched a second way), and the model out of the tool's
// own result. Nothing is asked of the disk and nothing new is put on the wire.

// promptWindow caps the prompt block, in lines, like every other expansion cap
// (toolstat.go's table). Twelve wrapped lines is a long and specific prompt
// read whole; past that the block folds and "… N more lines" lifts it, which is
// the same gesture the rest of this surface answers a long expansion with.
const promptWindow = 12

// imageCallRows is the prose head of an open `generate_image` step: the prompt,
// then one dim line per other input, then the model line. It returns the rows
// it kept and how many it dropped, exactly as [app.cap] does, because the
// prompt is the one part of it that can run long.
//
// It answers nothing at all for a call whose arguments never arrived — a begin
// with no payload, which is what a provider that does not stream tool calls
// sends — and then the expansion is the picture alone, exactly as it was.
func (a *app) imageCallRows(e *entry, width int) ([]string, int) {
	// IT ANSWERS FOR ONE HAND AND SAYS SO ITSELF, the way [app.commandRows]
	// answers only for a call carrying a command. The running expansion asks
	// this block without knowing which tool it is drawing (toolview.go's
	// [app.liveDetail]), and the other making verbs carry a `prompt` field too —
	// a video's, a tune's — which this block's words are wrong about: "drawn
	// with" is a sentence about a picture. So the gate is here, once, rather
	// than in each caller.
	if e == nil || e.tool != "generate_image" {
		return nil, 0
	}
	fields := argsOf(e.detail.Args)
	rows, more := a.cappedPromptRows(e, argString(fields, "prompt"), width)
	for _, line := range imageInputWords(fields) {
		rows = append(rows, a.pal.dim(fit(line, width)))
	}
	if line := imageModelLine(fields, e.detail.Output); line != "" {
		rows = append(rows, a.pal.dim(fit(line, width)))
	}
	return rows, more
}

// cappedPromptRows is the prompt, quoted and wrapped to the block's width.
//
// It is QUOTED because it is somebody's own words sitting among facts about
// them, and the quotes are the plain ASCII pair rather than the typographic
// one: this block is drawn on every terminal this surface reaches, including
// the ones with no repertoire beyond ASCII, and a quote mark is not a mark from
// the vocabulary that could answer for those (docs/design/icons/DESIGN.md
// governs glyphs, and this is punctuation).
//
// A call with no prompt draws nothing rather than an empty pair of quotes.
func (a *app) cappedPromptRows(e *entry, prompt string, width int) ([]string, int) {
	// The prompt is SOMEBODY ELSE'S BYTES and reaches this block with whatever
	// the model put in it, so every line of it goes through [drawableLine] —
	// which drops the newline with the other control bytes. The paragraph
	// breaks the author wrote are kept by splitting first and rejoining after,
	// because a prompt written in stanzas is a prompt read in stanzas.
	lines := strings.Split(strings.ReplaceAll(prompt, "\r\n", "\n"), "\n")
	for index, line := range lines {
		lines[index] = drawableLine(line)
	}
	prompt = strings.TrimSpace(strings.Join(lines, "\n"))
	if prompt == "" {
		return nil, 0
	}
	wrapped := wrap(`"`+prompt+`"`, width)
	painted := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		painted = append(painted, a.pal.ink(fit(line, width)))
	}
	if e.full || len(painted) <= promptWindow {
		return painted, 0
	}
	return painted[:promptWindow], len(painted) - promptWindow
}

// imageInputWords is every OTHER input the call carried, one short line each,
// in the order a person asks about them: the shape of the frame, then the
// pictures it was told to work from.
//
// An input nobody gave is not a line. There is no "default" row and no "—":
// leaving `size` out means the image model's own default, and a surface that
// wrote the word "default" would be claiming to know what that is.
func imageInputWords(fields map[string]json.RawMessage) []string {
	var out []string
	// The two frame arguments are one fact — how big, or what shape — and the
	// tool passes whichever arrived through untouched, so the line says the
	// caller's own spelling rather than a normalized one.
	if size := strings.TrimSpace(argString(fields, "size")); size != "" {
		out = append(out, size)
	}
	if ratio := strings.TrimSpace(argString(fields, "aspect_ratio")); ratio != "" {
		out = append(out, ratio)
	}
	if from := argPaths(fields, "reference_paths"); len(from) > 0 {
		out = append(out, "from "+strings.Join(from, ", "))
	}
	return out
}

// imageModelLine is what the block says about which model drew.
//
// THREE SHAPES, AND WHICH ONE APPEARS IS A FACT RATHER THAN A CHOICE:
//
//   - `drawn with paint-5` — the call left the model out, or asked for the name
//     it got. This is the ordinary line.
//   - `asked for best · drawn with paint-5` — the call asked in its own words
//     and the catalog answered with a name. Both halves are worth keeping: the
//     word is what somebody would repeat, the slug is what actually ran.
//   - `asked for seedream` alone — the call is still running, so the request is
//     known and the answer is not yet.
//
// A call that named no model and has not finished produces NOTHING, which is
// the emptiness law: the default is resolved inside the tool against the live
// catalog, and a surface that printed its own guess at it would be inventing a
// name three layers from anyone who could check it.
func imageModelLine(fields map[string]json.RawMessage, output string) string {
	asked := strings.TrimSpace(argString(fields, "model"))
	drew := modelui.ModelWord(generatedPictureModel(output))
	switch {
	case drew == "" && asked == "":
		return ""
	case drew == "":
		return "asked for " + asked
	case asked == "" || strings.EqualFold(asked, drew):
		return "drawn with " + drew
	default:
		return "asked for " + asked + " · drawn with " + drew
	}
}

// argPaths reads one array-of-strings argument as the drawable words a row can
// show. A field that is not an array of strings yields nothing rather than its
// raw JSON, because this block's whole promise is that it is prose.
func argPaths(fields map[string]json.RawMessage, key string) []string {
	raw, ok := fields[key]
	if !ok {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(drawableLine(value)); value != "" {
			out = append(out, value)
		}
	}
	return out
}
