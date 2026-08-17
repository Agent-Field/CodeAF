package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// THE OTHER HALF OF THE HARNESS OFFER.
//
// internal/session decides WHETHER a sub-harness takes a turn: it matches what
// somebody typed against the registry, raises one card, and waits for the
// answer (its harness.go). It deliberately knows nothing about what running one
// means — Config.RunHarness is the seam, and until this file existed the seam
// was nil, which is detection off. So the card could never appear, and a
// registry a person had saved harnesses into was a registry nothing could run.
//
// This is the surface's side: the registry the card is matched against, and the
// runner a yes reaches. Both are wired at launch, from things this process
// already has — the state root's store, this session's provider settings, the
// workspace — and neither performs a network request to be built.
//
// WHAT A YES ACTUALLY GETS. The program is walked by internal/subharness's own
// runner over the model bridge (its exec_model.go): each agent.loop is one
// completion, each tool.call is one of the four wire tools below, each verify
// and each free-text condition is one small judgement, and the walk — the
// branch law, the loop's rounds, the dynamism budget — is the package's,
// unchanged. The run's trace is saved beside the harness and the trail comes
// back as the turn's answer.
//
// TWO THINGS ARE NOT WIRED, and they are absences rather than gaps:
//
//   - THE GATE HAS NOBODY TO ASK. A human.gate auto-approves and says so in the
//     trail. This surface's questions are all about a call the model just made
//     (the consent card) or an account it reached for (the connect card); there
//     is no free-text ask to bridge to, and inventing one on the way past would
//     be a second question lane nobody designed. A harness with a real gate
//     wants the session's own loop, which is the next wave's work.
//   - THE MODEL IS THE ONE THE SESSION LAUNCHED ON. /model moves the
//     conversation, not this client, because the client is built once here and
//     the session exposes no hook to follow. A node that names its own model
//     overrides it either way, which is the field that exists for exactly this.

// harnessTimeout bounds one node's completion. It matches internal/session's
// own provider timeout: a harness node is a non-streamed call like the
// compaction summary is, and the backstop is against a wedged endpoint rather
// than a limit on how long a run may take.
const harnessTimeout = 10 * time.Minute

// v3HarnessEntries is the registry as DETECTION reads it: one entry per saved
// harness, at its head version.
//
// The entries carry no cue list, because a harness page has none to carry — the
// store holds programs, and [subharness.Entry.Cues] is a designer's trigger
// vocabulary that nothing yet writes down. That is not a hole to paper over: a
// harness with no cues is found by NAMING it ("run the research harness", which
// is the naming signal at 0.9) and by nothing else, so the card appears when
// somebody asked for it by name and stays quiet otherwise. When cues land on
// the page, they land here.
//
// A store that cannot be read answers with nothing, which is detection off. A
// registry is not worth failing a launch over.
func v3HarnessEntries(store *subharness.Store) []subharness.Entry {
	if store == nil {
		return nil
	}
	names, err := store.Names()
	if err != nil {
		return nil
	}
	var entries []subharness.Entry
	for _, name := range names {
		h, err := store.Load(name, 0)
		if err != nil {
			// One unreadable page does not hide the rest of the registry.
			continue
		}
		entries = append(entries, subharness.Entry{
			Name:        h.Id.Name,
			Description: h.Id.Desc,
			Revision:    h.Id.Version,
		})
	}
	return entries
}

// v3RunHarness builds what a yes on the offer card reaches.
//
// NIL IS DETECTION OFF, and it is returned rather than an error for the reason
// the entries are: a settings row that cannot build a client is a reason to run
// the ordinary turn, not a reason to refuse to open a conversation. The session
// checks this seam for nil before it matches anything (its harness.go).
func v3RunHarness(store *subharness.Store, settings config.Config, model, workspace string) func(ctx context.Context, name, text string) (string, error) {
	if store == nil {
		return nil
	}
	client, err := provider.NewClient(provider.Config{
		APIKey:         settings.APIKey,
		BaseURL:        settings.BaseURL,
		Model:          model,
		Timeout:        harnessTimeout,
		SiteURL:        settings.SiteURL,
		SiteName:       settings.SiteName,
		SiteCategories: settings.SiteCategories,
	})
	if err != nil {
		return nil
	}
	tools := v3HarnessTools(workspace)
	return func(ctx context.Context, name, text string) (string, error) {
		h, err := store.Load(name, 0)
		if err != nil {
			return "", err
		}
		trace, runErr := subharness.Run(ctx, h, subharness.ModelExec(client, subharness.ModelExecOpts{
			Harness: h,
			RunTool: tools,
			// No Ask: a gate auto-approves here and the trail says so.
			Store: store,
		}))
		if trace.Id.Name == "" {
			// The run never started — an invalid page. There is no evidence to
			// keep and nothing to report but why.
			return "", runErr
		}
		saved, saveErr := store.SaveRun(trace)
		return harnessReport(trace, saved, saveErr, runErr), nil
	}
}

// harnessReport is what the turn says back: the trail, then what the run itself
// produced, then where the evidence landed.
//
// A FAILED RUN STILL REPORTS. The alternative — handing the error back to the
// session, which draws it as an error and records nothing — would throw away
// the trail, which is the one thing worth having when a harness went wrong. The
// card's own head says `name · v1 · failed` and the failing step carries a ✗,
// so nothing here is dressing a failure up as an answer.
func harnessReport(trace subharness.Trace, saved string, saveErr, runErr error) string {
	parts := []string{subharness.RunCard(trace)}
	if out := harnessFinalOut(trace); out != "" {
		parts = append(parts, out)
	}
	if runErr != nil {
		parts = append(parts, "The run ended here: "+runErr.Error())
	}
	switch {
	case saveErr != nil:
		parts = append(parts, "The trace could not be saved: "+saveErr.Error())
	case saved != "":
		parts = append(parts, "trace · "+saved)
	}
	return strings.Join(parts, "\n\n")
}

// harnessFinalOut is what the run actually produced, in full.
//
// A plain [subharness.Run] fills Trace.Out for nobody — that field is written
// by the package's own Runner, which this wiring does not use because it wants
// the walk and not the runner's status handling. The trail is where the outputs
// really are, and the LAST one that said anything is the run's answer. Reading
// it here rather than teaching Run to fill it keeps the walk unchanged, which
// is the half of this feature two slices share.
func harnessFinalOut(trace subharness.Trace) string {
	if out := strings.TrimSpace(trace.Out); out != "" {
		return out
	}
	for at := len(trace.Trail) - 1; at >= 0; at-- {
		if out := strings.TrimSpace(trace.Trail[at].Out); out != "" {
			return out
		}
	}
	return ""
}

// v3HarnessTools is the tool.call bridge: the four wire tools plus the three
// read-only ones, over this session's workspace.
//
// It is [bare.AllTools] rather than the session's own belt because the belt is
// assembled inside an agent this wiring does not hold, and because the belt is
// the MODEL's — it carries notes, jobs, connected accounts and image
// generation, which are things a conversation reaches for and not things a
// saved procedure should inherit by accident. A harness names the tools it
// wants on its whitelist, and this is the set those names can resolve to.
func v3HarnessTools(workspace string) func(ctx context.Context, tool, args string) (string, error) {
	belt := map[string]bare.Tool{}
	for _, tool := range bare.AllTools(workspace) {
		belt[tool.Name] = tool
	}
	return func(ctx context.Context, name, args string) (string, error) {
		tool, ok := belt[name]
		if !ok {
			return "", fmt.Errorf("there is no tool named %q on this surface", name)
		}
		payload, err := harnessToolArgs(name, args)
		if err != nil {
			return "", err
		}
		text, failed, err := tool.Execute(ctx, payload)
		if err != nil {
			return "", err
		}
		if failed {
			// A tool that refused is a node that failed, which is what the
			// condition language's `failed` is for — not an output the next
			// step would go on to verify as though it were work.
			if text = strings.TrimSpace(text); text != "" {
				return "", errors.New(text)
			}
			return "", fmt.Errorf("%s failed and said nothing", name)
		}
		return text, nil
	}
}

// harnessToolPrimary is the one field a tool's arguments collapse to when a
// harness page writes them as a sentence rather than as JSON. `bash` with
// `go test ./...` is what a person writes on a card, and holding them to
// `{"command":"go test ./..."}` would make the page unreadable to buy nothing.
//
// The tools NOT in this table — edit and write — take two fields that cannot be
// guessed apart, so they take JSON and say so.
var harnessToolPrimary = map[string]string{
	"bash": "command",
	"read": "path",
	"ls":   "path",
	"grep": "pattern",
	"find": "pattern",
}

func harnessToolArgs(tool, args string) (json.RawMessage, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return json.RawMessage("{}"), nil
	}
	if strings.HasPrefix(args, "{") && json.Valid([]byte(args)) {
		return json.RawMessage(args), nil
	}
	field, ok := harnessToolPrimary[tool]
	if !ok {
		return nil, fmt.Errorf("%s takes its arguments as a JSON object, and %q is not one", tool, args)
	}
	payload, err := json.Marshal(map[string]string{field: args})
	if err != nil {
		return nil, err
	}
	return payload, nil
}
