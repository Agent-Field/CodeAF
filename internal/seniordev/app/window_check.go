//go:build !windows

package app

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
)

// tinyWindowTokens is the largest window senior-dev refuses to start on. A
// run keeps its brief, its tools, its checklist and its progress in its
// model's window and compacts at 60% of it; at this size and below there is
// no room left to work between compactions, and a run spends its whole budget
// re-reading what it already knew. Refusing costs nothing, because nothing has
// been spent yet.
const tinyWindowTokens = 32_768

// windowOf is how many tokens a model holds as this run will size it, and
// where that figure came from ([seniorDevModels.sizedModel]). A config block or
// the backend's own limit that names the context overrides any catalog, and
// is reported as config.
func (models seniorDevModels) windowOf(ref string) (float64, string, error) {
	providerID, modelID := normalizeModelRef(splitModelID(ref))
	_, source, err := models.sizedModel(providerID, modelID)
	if err != nil {
		return 0, "", err
	}
	_, metadata, err := models.projection(providerID, modelID)
	if err != nil {
		return 0, "", err
	}
	if _, ok := configNumber(objectValue(models.backend.config.model(providerID, modelID)["limit"])["context"]); ok ||
		models.backend.contextLimit != 0 {
		source = sizedByConfig
	}
	return metadata.Limit.Context, source, nil
}

// tinyWindow reports a model whose window is KNOWN to be at or under
// [tinyWindowTokens], and that window. A guess is never tiny: it is what
// senior-dev assumes when nothing could size the model.
func (models seniorDevModels) tinyWindow(ref string) (float64, bool) {
	window, source, err := models.windowOf(ref)
	return window, err == nil && source != sizedByGuess && window > 0 && window <= tinyWindowTokens
}

// leaveOutTinyCrewSeats takes a crew seat too small to work in out of the
// pools, with a note, exactly as [crewPools] takes out a seat nothing can
// size, and routes on senior-dev's own list when that empties the coder's
// pool. A crew is a standing choice nobody made for this brief, so its small
// light seat must not refuse a run the person never named that model for.
// keepHigh holds the coder's pool as it is: under --asked those are the
// models the person named, and [windowCheck] refuses them by name instead.
func leaveOutTinyCrewSeats(args cliArgs, keepHigh bool, models seniorDevModels, notes io.Writer) cliArgs {
	keep := func(raw string) string {
		var kept []string
		for _, ref := range splitPool(raw) {
			if window, tiny := models.tinyWindow(ref); tiny {
				_, _ = fmt.Fprintf(notes,
					"[senior-dev] the crew's %s holds only %s tokens, too few to work in; it is left out of this run\n",
					ref, groupedTokens(window))
				continue
			}
			kept = append(kept, ref)
		}
		return strings.Join(kept, ",")
	}
	args.Low, args.Frontier = keep(args.Low), keep(args.Frontier)
	if keepHigh {
		return args
	}
	if args.High = keep(args.High); args.High == "" {
		_, _ = fmt.Fprintf(notes, "[senior-dev] none of the crew's models is large enough to work in; routing on senior-dev's own list\n")
		args.High = DefaultHighModels
	}
	return args
}

// windowCheck reads the window of every model the run will call -- the
// coder's pool, the light pool its history summaries run on, and the
// frontier -- before anything is spent. It answers the refusal for models
// whose window is KNOWN to be too small ([tinyWindowTokens]), and the models
// whose window nobody knows and was guessed.
//
// A GUESS IS NEVER REFUSED. The guess is what senior-dev assumes when neither
// models.dev nor codeaf's own catalog can be read, and the model behind it is
// as likely to hold a million tokens as sixteen thousand; refusing on it would
// stop runs that would have worked. It is said instead (run.go), so a run that
// compacts every few steps says why at its start.
func windowCheck(args cliArgs, models seniorDevModels) (refusal string, guessed []string) {
	seen := map[string]bool{}
	var tiny []string
	for _, pool := range []string{args.High, args.Low, args.Frontier} {
		for _, ref := range splitPool(pool) {
			name := strings.TrimPrefix(ref, orclient.Service+"/")
			if seen[name] {
				continue
			}
			seen[name] = true
			_, source, err := models.windowOf(ref)
			if err != nil {
				continue
			}
			if source == sizedByGuess {
				guessed = append(guessed, name)
			} else if window, small := models.tinyWindow(ref); small {
				tiny = append(tiny, fmt.Sprintf("%s (%s tokens)", name, groupedTokens(window)))
			}
		}
	}
	if len(tiny) > 0 {
		refusal = "senior-dev cannot work with " + strings.Join(tiny, ", ") +
			": a run needs a model that holds more than " + groupedTokens(tinyWindowTokens) +
			" tokens to keep its brief, its tools and its progress in view, so nothing was started; ask for a model with a larger window"
	}
	return refusal, guessed
}

// sayGuessedWindows tells the person, on the run's notes and on its record,
// which models are running on the guessed window ([windowCheck]).
func sayGuessedWindows(guessed []string, notes io.Writer, events *eventWriter) {
	if len(guessed) == 0 {
		return
	}
	_, _ = fmt.Fprintf(notes,
		"[senior-dev] neither models.dev nor codeaf's model catalog knows how much %s can hold; assuming %s tokens, so its history will be compacted often\n",
		strings.Join(guessed, ", "), groupedTokens(guessedContextTokens))
	if events != nil {
		events.stage("compaction-capacity", "guessed", map[string]any{
			"models": guessed, "limit_tokens": float64(guessedContextTokens),
		})
	}
}

// groupedTokens writes a token count with thousands separators, the way every
// count a person reads is written.
func groupedTokens(tokens float64) string {
	digits := strconv.FormatInt(int64(tokens), 10)
	var out strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return out.String()
}
