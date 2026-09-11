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
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
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
// runner over the model bridge (its exec_model.go): an agent.loop with tools is
// the provider's bounded tool loop and one without them is one completion; each
// tool.call is one of the seven bare tools below; each verify and each free-text
// condition is one small judgement; and the walk — the branch law, the loop's
// rounds, the dynamism budget — is the package's, unchanged. The run's trace is
// saved beside the harness and the trail comes back as the turn's answer.
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
//     overrides it either way, which is the field that exists for exactly this
//     — and so does a TURN that named one: "research the pricing tiers with
//     opus" reaches this file as the run's model, resolved to an id the session
//     checked against the same catalog the picker draws (its harness.go), and
//     is handed to the bridge as the default every agent.loop node rides.

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
// THE SEAMS ARE THE CONVERSATION'S OWN, handed down whole rather than
// reassembled here, so that a run's belt carries the same generation verbs the
// conversation's does, the same absences, and — the half that was missing — the
// same answer about where what it makes lands and how the person finds it again.
// Every one of them may be zero, which is a machine with no media models writing
// to the legacy rung, and a harness belt of seven wire tools, exactly as before.
//
// The Seer is the ONE seam this door fills rather than passes on, because it is
// the run's own client — built here, from the person's settings, and held by
// nothing upstream that could have handed it over.
func v3RunHarness(store *subharness.Store, settings config.Config, model, workspace string,
	seams session.HarnessBeltSeams,
) func(ctx context.Context, name, text, runModel string, step func(subharness.Trail)) (string, subharness.Usage, error) {
	if store == nil {
		return nil
	}
	configured := settings.ClientConfig(model)
	configured.Timeout = harnessTimeout
	client, err := provider.NewClient(configured)
	if err != nil {
		return nil
	}
	// The run's own client is what view_image looks with: a harness that can
	// check the picture it just made needs somewhere to send it, and this is the
	// completer the run already holds.
	seams.Seer = client
	tools := v3HarnessToolBridges(workspace, seams)
	return func(ctx context.Context, name, text, runModel string, step func(subharness.Trail)) (string, subharness.Usage, error) {
		h, err := store.Load(name, 0)
		if err != nil {
			return "", subharness.Usage{}, err
		}
		// THE RUN'S OWN BILL, kept per run and not per client: the client is
		// built once at launch and outlives every run made through it, so a
		// ledger beside it would hand the session the whole day's spend on the
		// second harness somebody ran. The bridge folds each call into this as
		// it lands (subharness's usage.go), and what comes back is what the
		// session charges the person for (internal/session's harness.go) — the
		// half that was missing while designing a harness was billed and
		// running one was free.
		spent := &subharness.Usage{}
		// THE WALK IS WATCHED, so the session can say what the run is doing while
		// it does it. The step handed on is the trail's own entry — the same one
		// the card below is rendered from — and the session decides what a surface
		// is told about it; nothing here shapes it (internal/session's harness.go).
		trace, runErr := subharness.RunWatched(ctx, h, subharness.ModelExec(client, subharness.ModelExecOpts{
			Harness:  h,
			RunTool:  tools.Run,
			Toolbelt: tools.Belt,
			// No Ask: a gate auto-approves here and the trail says so.
			Store: store,
			// What the turn asked this run to think with, empty when it asked
			// for nothing. A node that pinned its own model still wins.
			Model: runModel,
			Usage: spent,
		}), step)
		if trace.Id.Name == "" {
			// The run never started — an invalid page. There is no evidence to
			// keep and nothing to report but why. The ledger comes back anyway:
			// a page that failed validation made no calls, and saying so with
			// the zero value is cheaper than a second shape for nothing.
			return "", *spent, runErr
		}
		saved, saveErr := store.SaveRun(trace)
		return harnessReport(trace, saved, saveErr, runErr), *spent, nil
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

// v3HarnessTools is the tool.call bridge: the wire tools plus whichever media
// verbs this machine has models for, over this session's workspace.
//
// It is [session.HarnessBelt] and no longer a bare.AllTools of its own. That
// call used to be made independently here, in harnessMachinery and in
// harnessToolNames — three lists nothing held together, so a verb the designer
// was offered was not necessarily a verb the lint would accept or this bridge
// could resolve. harness_belt.go is the one answer all three now read, and it
// states what a harness gets and what it deliberately does not.
type v3HarnessToolBridge struct {
	Run  func(ctx context.Context, tool, args string) (string, error)
	Belt subharness.Toolbelt
}

// v3HarnessToolBridges builds both argument doors over ONE registry. A fixed
// tool.call keeps the page-friendly sentence grammar below; an agent.loop
// already writes the schema's structured object. Both become the same raw JSON
// payload before [bare.Tool.Execute], so there is one execution path and one
// meaning for failure.
func v3HarnessToolBridges(workspace string, seams session.HarnessBeltSeams) v3HarnessToolBridge {
	belt := map[string]bare.Tool{}
	var defs []ai.ToolDefinition
	for _, tool := range session.HarnessBelt(workspace, seams) {
		belt[tool.Name] = tool
		var parameters map[string]any
		// These are the package's pinned schema literals. If one ever stops being
		// JSON, omitting its parameters makes the wire defect visible in tests
		// without making chat startup a new error-returning operation.
		_ = json.Unmarshal(tool.Schema, &parameters)
		defs = append(defs, ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{
			Name: tool.Name, Description: tool.Description, Parameters: parameters,
		}})
	}
	execute := func(ctx context.Context, name string, payload json.RawMessage) (string, error) {
		tool, ok := belt[name]
		if !ok {
			return "", fmt.Errorf("there is no tool named %q on this surface", name)
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
	return v3HarnessToolBridge{
		Run: func(ctx context.Context, name, args string) (string, error) {
			payload, err := harnessToolArgs(name, args)
			if err != nil {
				return "", err
			}
			return execute(ctx, name, payload)
		},
		Belt: subharness.Toolbelt{
			Defs: defs,
			Call: func(ctx context.Context, name string, args map[string]any) (string, error) {
				payload, err := json.Marshal(args)
				if err != nil {
					return "", fmt.Errorf("encode %s arguments: %w", name, err)
				}
				return execute(ctx, name, payload)
			},
		},
	}
}

// v3HarnessTools keeps the fixed-argument bridge as a small, testable surface.
func v3HarnessTools(workspace string, seams session.HarnessBeltSeams) func(ctx context.Context, tool, args string) (string, error) {
	return v3HarnessToolBridges(workspace, seams).Run
}

// harnessToolPrimary is the one field a tool's arguments collapse to when a
// harness page writes them as a sentence rather than as JSON. `bash` with
// `go test ./...` is what a person writes on a card, and holding them to
// `{"command":"go test ./..."}` would make the page unreadable to buy nothing.
//
// The media verbs are here for exactly the same reason: `generate_image` with
// `a wide banner of a harbour at dawn` is what a person writes on a card, and
// every one of them has ONE argument that is obviously the subject — the prompt,
// the words to speak, the picture to look at. Their other arguments (a size, a
// voice, a destination) are optional and are written as JSON when a page wants
// them, which is the same bargain bash makes.
//
// The tools NOT in this table — edit and write — take two fields that cannot be
// guessed apart, so they take JSON and say so.
var harnessToolPrimary = map[string]string{
	"bash":           "command",
	"read":           "path",
	"ls":             "path",
	"grep":           "pattern",
	"find":           "pattern",
	"generate_image": "prompt",
	"generate_music": "prompt",
	"generate_video": "prompt",
	"speak":          "text",
	"view_image":     "path",
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
