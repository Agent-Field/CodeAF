package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/rtk"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The base tool set is five tools, and the count is the design. Pull-only
// recall and configured media capabilities are appended at the leaf boundary.
//
// Every definition is re-sent on every turn, and every result stays in context
// for every turn after it arrives, so the question is not "what would be
// convenient" but "what earns its place in a prompt paid for repeatedly".
//
//	sh     one definition buys read, list, search, find, move, curl, and
//	       everything nobody has thought of yet. The shell already composes, so
//	       batching needs no schema help.
//	job    keeps long-lived shell work from blocking the linear loop while
//	       preserving shell composition for logs and readiness monitors.
//	write  because content must not pass through a shell. Emitting a document
//	       through a heredoc means quoting prose, which corrupts it and burns
//	       tokens on escaping.
//	edit   because changing a paragraph should cost a paragraph, not a whole
//	       file re-typed.
//	web    the only capability a shell genuinely lacks. urls is plural because
//	       research is search-once-then-read-several, and batching that turns
//	       five round-trips into one.
const (
	maxToolResultBytes = 12 << 10
	maxCommandSeconds  = 120

	// spillBytes is where a result stops being worth carrying. Anything larger
	// is written to a file and represented in context by a preview and a path.
	//
	// This is the single highest-leverage bound in the system. A turn resends
	// every earlier observation, so a k-turn loop pays for its results k times
	// over — one 25-turn leaf was 54% of a whole run's input tokens. Spilling
	// caps what any observation can cost, and the model loses nothing it cannot
	// get back: reading part of a file is one `sh` call away.
	//
	// The threshold sits where a typical single document — a source file, a
	// fetched page's article body — still fits whole. Set at 4KB it truncated
	// most of the very files an agent was asked to change, and the agent spent
	// its run re-fetching slices of things it had already read. Aging is the
	// decay pass's job; spill only has to stop the genuinely huge result.
	spillBytes   = 10 << 10
	previewBytes = 4 << 10
	// maxRecallResultBytes bounds persistent memory more tightly than ordinary
	// observations: recall is a map used to choose what to read, not a second
	// copy of the territory.
	maxRecallResultBytes = 8 << 10
)

// Result is one tool's answer. A failure is a Result, never a Go error: the
// model has to see what went wrong to fix it, and aborting the loop over a
// mistyped path throws away every turn that came before.
type Result struct {
	Content string
	IsError bool
	// reportedJobs prevents a status-bearing result from being memoised and
	// replayed later as if its transient background state were still current.
	reportedJobs bool
	// Followup carries multimodal content that must reach the next model turn.
	// The ordinary text result is still emitted first so tool-call pairing
	// remains valid on every OpenAI-compatible backend.
	Followup []ai.ContentPart
	Usage    Usage
}

func errorf(format string, args ...any) Result {
	return Result{Content: fmt.Sprintf(format, args...), IsError: true}
}

// Toolbox executes tool calls against one workspace on behalf of one node.
type Toolbox struct {
	workspace *Workspace
	// leaf is who this toolbox works for, and it names every file the leaf's
	// machinery writes: the artifact bucket, the spilled observations, the
	// background job logs. It is a string because the identity a caller holds is
	// not always a number — see Task.NodeKey.
	leaf    string
	web     *Web
	history *store.Store
	media   *MediaTools
	jobs    *jobRegistry
	// spills is atomic because a turn's tool calls execute concurrently, and
	// two large results spilling at once must not race the counter into the
	// same file name.
	spills atomic.Int64

	// armed names the optional capability families whose schemas this leaf is
	// currently carrying. It is guarded because a turn's tool calls run
	// concurrently and Definitions is read between turns.
	armedMu sync.Mutex
	armed   map[string]bool
	// armedOrder is the same set in the order it was armed, and it exists for
	// the prompt cache rather than for bookkeeping. Emitting the armed schemas
	// in a fixed order would insert a newly armed family in FRONT of one already
	// in hand — a rewrite of the middle of the tool block, which re-bills every
	// byte behind it — where emitting them in arrival order can only ever append
	// at the tail. See Definitions.
	armedOrder []string

	// share is the worker's one-line channel to the rest of its job. Nil for a
	// job with no siblings — the schema is only carried where somebody is
	// listening. Installed from Task.Share at run start.
	share func(line string) error
}

// The optional capability families, and the whole of why they are optional.
//
// Every definition is re-sent on every turn of every leaf. Measured on a
// six-cell benchmark, the media and document schemas were ~1,112 tokens of
// each leaf turn's ~3,778-token fixed floor — 18% of every input token the run
// spent — and not one of those cells could have used a single one of them. A
// bugfix cannot generate a video. A release note cannot read a PDF that does
// not exist.
//
// The answer is not to remove the tools; the resident's charter, watch and
// media journeys genuinely need them. It is to stop paying for them by
// default. A leaf carries the core loop plus one small tool that says what
// else exists; asking for a family arms it for the next turn and every turn
// after. The judgement of whether the work needs a camera stays with the model
// doing the work, made at the moment it knows — not predicted for it at
// compile time by a call that has never seen the workspace.
//
// The cost is honest and worth naming: a job that does need media pays one
// extra turn and one prefix-cache invalidation at the moment it arms. A job
// that does not — the overwhelming majority — pays nothing at all, ever.
const (
	FamilyMedia    = "media"
	FamilyDocument = "documents"
)

// The tools each family carries. Arming is per tool rather than per family so
// a structural signal can admit exactly what it justifies: an attached
// screenshot is a reason to be able to look at an image, not a reason to be
// able to score a film.
var familyTools = map[string][]string{
	FamilyMedia:    {"generate_image", "generate_music", "generate_video", "speak", "view_image"},
	FamilyDocument: {"read_document"},
}

// Arm admits named capability families or individual tools for the rest of
// this leaf's life. It is how a structural fact about the assignment — an
// attached image, an attached document — buys back exactly the schema it
// justifies before the first turn, and how the discovery tool answers a
// worker that asked.
func (t *Toolbox) Arm(names ...string) {
	t.armedMu.Lock()
	defer t.armedMu.Unlock()
	if t.armed == nil {
		t.armed = map[string]bool{}
	}
	for _, name := range names {
		if tools, ok := familyTools[name]; ok {
			for _, tool := range tools {
				t.armOne(tool)
			}
			continue
		}
		t.armOne(name)
	}
}

// armOne records one newly armed tool once, keeping the arrival order the tool
// block is emitted in. Called with armedMu held.
func (t *Toolbox) armOne(name string) {
	if t.armed[name] {
		return
	}
	t.armed[name] = true
	t.armedOrder = append(t.armedOrder, name)
}

func (t *Toolbox) isArmed(name string) bool {
	t.armedMu.Lock()
	defer t.armedMu.Unlock()
	return t.armed[name]
}

// armedInOrder is a snapshot of what this leaf holds, oldest first.
func (t *Toolbox) armedInOrder() []string {
	t.armedMu.Lock()
	defer t.armedMu.Unlock()
	order := make([]string, len(t.armedOrder))
	copy(order, t.armedOrder)
	return order
}

// offered names the families this leaf could still arm — configured on the
// brain, and not already in hand. It is prose only: it phrases the error a
// worker reads when it calls a tool it has not got. It decides nothing about
// the tool list, because a tool list that moves when a family is armed is the
// defect this file spent a whole commit removing.
func (t *Toolbox) offered() []string {
	var families []string
	if t.media != nil && t.media.Provider != nil && !t.isArmed("generate_image") {
		families = append(families, FamilyMedia)
	}
	if t.media != nil && t.media.DocumentClient != nil && !t.isArmed("read_document") {
		families = append(families, FamilyDocument)
	}
	return families
}

// configuredFamilies says whether this machine has anything to arm at all. It
// reads the brain's wiring and never the armed set, which is exactly what makes
// it constant for the whole life of a leaf — and identical across every leaf of
// a run, since they all share one brain.
func (t *Toolbox) configuredFamilies() bool {
	return t.media != nil && (t.media.Provider != nil || t.media.DocumentClient != nil)
}

// capabilitiesDefinition is the whole discovery surface: one tool, one
// argument, and an enum the code owns because the families are the code's own
// grouping of its own tools. The model reads its own work and decides; nothing
// here matches a phrase against the brief.
//
// Every byte of it is frozen, and that is the fix rather than the style.
//
// The description used to be assembled from whichever families were still
// unarmed, and the enum with it. Tool definitions ride at the front of every
// request, ahead of the entire transcript, so the moment a worker armed media
// the sentence describing the families changed, the prefix diverged at the tool
// block, and the whole prompt behind it was re-billed cold. Arming is supposed
// to cost exactly one invalidation — the new schemas appended at the end of the
// tool list, which is the price the design already accepted and named. This was
// a second, larger one nobody had costed, paid at the front instead of the back.
//
// So the enum stays full width too, including a family this machine may not
// have configured. Asking for one that is missing is answered by capabilities
// itself, in a sentence that tells the worker to do the job without it and say
// so — one wasted call in the rare case, against a definition block that never
// moves for any leaf on any turn.
//
// It no longer retires, either, and that was the last moving part. Retiring it
// once everything on offer was armed removed a definition from the MIDDLE of
// the tool list — every schema behind it shifted, so the turn that finished
// arming paid a second full-prompt invalidation on top of the one arming
// already costs. The tool is ~90 tokens; the block it was displacing is the
// whole prompt. So it stays for the life of any leaf whose machine has a family
// configured, and a worker that asks for something it already holds is answered
// in one cheap line — see capabilities.
func capabilitiesDefinition() ai.ToolDefinition {
	return define("capabilities",
		"Load tools you do not have yet; they arrive on your next turn. Ask once, only if the work needs one. "+
			"media: generate images, music, video, speech; look at an image. "+
			"documents: read a PDF, DOCX or PPTX into text.",
		map[string]any{
			"need": map[string]any{"type": "string", "enum": []string{FamilyMedia, FamilyDocument}},
		}, "need")
}

// capabilities arms what was asked for and says what arrived. The reply names
// the tools rather than the family, because the next turn's schema list is
// what the worker will actually be holding.
func (t *Toolbox) capabilities(args map[string]any) Result {
	need := strings.TrimSpace(stringArg(args, "need"))
	tools, known := familyTools[need]
	if !known {
		// The families, not what is left to arm: the enum is the same on every
		// turn now, so the correction has to name the same thing the enum does
		// or the two disagree in front of the model.
		return errorf("no capability family named %q. Available: %s, %s", need, FamilyMedia, FamilyDocument)
	}
	switch need {
	case FamilyMedia:
		if t.media == nil || t.media.Provider == nil {
			return errorf("media generation is not configured on this machine — do what the assignment needs without it and say plainly in your answer that it could not be done")
		}
	case FamilyDocument:
		if t.media == nil || t.media.DocumentClient == nil {
			return errorf("document parsing is not configured on this machine — do what the assignment needs without it and say plainly in your answer that it could not be done")
		}
	}
	// Already in hand: the cheap no-op that lets the definition stay in the
	// prompt forever. A worker that asks twice costs one short tool result at
	// the end of the transcript, which is appended and therefore free of any
	// prefix invalidation; retiring the tool to prevent the second ask would
	// rewrite the tool block instead, and that is paid for by every remaining
	// turn of the leaf.
	if t.holdsAll(tools) {
		return Result{Content: "Already loaded — " + strings.Join(tools, ", ") +
			" are in your tool list now. Use them; do not ask again."}
	}
	t.Arm(need)
	return Result{Content: "Loaded for your next turn and every turn after: " + strings.Join(tools, ", ") +
		". Their full descriptions are in your tool list from here on."}
}

// holdsAll reports that every named tool is already armed.
func (t *Toolbox) holdsAll(tools []string) bool {
	for _, tool := range tools {
		if !t.isArmed(tool) {
			return false
		}
	}
	return len(tools) > 0
}

// shareLine hands one line to the rest of the job. The write itself is the
// caller's closure — the toolbox knows nothing about where notes live, which
// is what keeps this package free of the store's job topology.
func (t *Toolbox) shareLine(args map[string]any) Result {
	if t.share == nil {
		return errorf("this work has no other workers to tell")
	}
	line := strings.TrimSpace(stringArg(args, "line"))
	if line == "" {
		return errorf("share needs the one line the others should read")
	}
	// One line means one line: a paragraph shared to every sibling is paid for
	// in every sibling's every remaining turn.
	if len(line) > shareLineBytes {
		clipped := line[:shareLineBytes]
		for len(clipped) > 0 && !utf8.ValidString(clipped) {
			clipped = clipped[:len(clipped)-1]
		}
		line = clipped + "…"
	}
	if err := t.share(line); err != nil {
		return errorf("could not pass that along: %v", err)
	}
	return Result{Content: "Passed along. The other workers read it between turns."}
}

// shareLineBytes bounds one shared line. It is a bound on cost, not on
// content: every byte here is re-read by every sibling on every remaining
// turn, so a note pays rent everywhere at once.
const shareLineBytes = 300

func NewToolbox(workspace *Workspace, leaf string, web *Web) *Toolbox {
	return &Toolbox{workspace: workspace, leaf: leaf, web: web, jobs: newJobRegistry(workspace, leaf)}
}

// NewToolboxWithStore adds persistent recall to the generic toolbox. A nil
// store deliberately collapses to NewToolbox so one-shot leaves retain the
// base-definition prompt.
func NewToolboxWithStore(workspace *Workspace, leaf string, web *Web, history *store.Store) *Toolbox {
	return &Toolbox{workspace: workspace, leaf: leaf, web: web, history: history, jobs: newJobRegistry(workspace, leaf)}
}

func newToolboxWithMedia(workspace *Workspace, leaf string, web *Web, history *store.Store, media *MediaTools) *Toolbox {
	return &Toolbox{workspace: workspace, leaf: leaf, web: web, history: history, media: media, jobs: newJobRegistry(workspace, leaf)}
}

// mediaModelArgDescription teaches the model argument in one breath: the slot
// default is right for routine work, and "best" is for the times quality is
// the point. It is resent every turn, so it stays one sentence.
const mediaModelArgDescription = `optional model: omit for the default, "best" when the user asked for quality or this is the final deliverable, or a model name`

// Definitions are what the model sees. Descriptions are terse because they are
// resent every turn, but each one states the thing an agent gets wrong without
// being told.
//
// The ORDER is load-bearing and is the second half of the cache-shape fix. Tool
// definitions ride at the very front of every request, ahead of the whole
// transcript, so the first byte of this block that differs between two calls
// re-bills everything behind it at full price. The list is therefore built in
// three strata, widest agreement first:
//
//  1. the five tools every leaf on every machine always has, in a fixed order;
//  2. the discovery tool, present for the life of any leaf whose machine has an
//     optional family configured — a property of the brain, never of what this
//     leaf has armed, so it never appears or vanishes mid-run;
//  3. per-leaf conditionals (recall, share) and then the armed schemas.
//
// Only stratum 3 can move, and it can only ever grow at the tail: arming a
// family appends, so the invalidation is bounded by the schemas actually added
// rather than by everything that used to sit behind the thing that moved.
func (t *Toolbox) Definitions() []ai.ToolDefinition {
	definitions := []ai.ToolDefinition{
		define("sh", "Run a shell command in the workspace. Use it to read, list, search, and inspect. cmd is one command string (chain with && and pipes), or an array of commands run in order, stopping at the first failure. For INDEPENDENT commands, prefer separate sh calls in the same turn — they run at the same time. For servers, builds over a minute, or watch loops, set bg:true and use the job tool — do not block on them.", map[string]any{
			"cmd": prop("string", "shell command, or an array of commands run serially"),
			"t":   prop("integer", "timeout seconds; default 60, or 900 with bg"),
			"bg":  prop("boolean", "start as a background job"),
		}, "cmd"),
		define("job", "Check or wait on background jobs. Prefer one wait over repeated checks. Compose monitors from bg shell loops (e.g. bg: until curl -s :8080/health; do sleep 1; done — then wait on it). Kill servers when done testing; anything still running dies with the leaf. To keep a server running after the task, use keep — never nohup.", map[string]any{
			"id":   prop("integer", "job id; omit to list jobs"),
			"wait": prop("integer", "seconds to wait for exit, maximum 120"),
			"kill": prop("boolean", "terminate the job process group"),
			"keep": map[string]any{"type": "object", "description": "request promotion to a user-owned service", "properties": map[string]any{
				"name":   prop("string", "short service name"),
				"health": prop("string", "port:5173, url:http://..., or cmd:..."),
			}, "required": []string{"name", "health"}},
		}),
		define("write", "Write a file with exact content. Use this for any deliverable prose; never emit documents through sh.", map[string]any{
			"path": prop("string", "workspace-relative path"),
			"text": prop("string", "full file content"),
		}, "path", "text"),
		define("edit", "Replace an exact string in a file. Far cheaper than rewriting a long file to change part of it.", map[string]any{
			"path": prop("string", "workspace-relative path"),
			"old":  prop("string", "exact text to replace, must appear once"),
			"new":  prop("string", "replacement text"),
		}, "path", "old", "new"),
		define("web", "Search the web, or fetch pages as text. Pass q to search. Pass urls to fetch several pages in one call, which is much faster than one at a time.", map[string]any{
			"q":    prop("string", "search query"),
			"urls": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "page urls to fetch"},
			"n":    prop("integer", "max search results, default 6"),
		}),
	}
	// Stratum 2: the door to the optional families. Its presence follows the
	// machine's wiring and nothing else, so it is in the same slot on every turn
	// of every leaf of a run, or on none of them.
	if t.configuredFamilies() {
		definitions = append(definitions, capabilitiesDefinition())
	}
	// Stratum 3 begins here: everything below is per-leaf or per-arming, and is
	// only ever appended.
	if t.history != nil {
		definitions = append(definitions, define("recall", "Search folded work and the notebook. Recall gives the map, not the territory: use the returned digest to choose what matters, then read the returned pointer paths with sh for the verbatim details. terms are free text; scope_cues are optional workspace or file paths.", map[string]any{
			"terms":      prop("string", "words describing the prior work or lesson"),
			"scope_cues": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "optional workspace or file paths"},
			"limit":      prop("integer", "maximum fold and notebook hits, default 5, maximum 10"),
		}, "terms"))
	}
	if t.share != nil {
		definitions = append(definitions, define("share", "Tell the other workers on this job one line they need: a discovery about the material, a pitfall, a decision they must match. It reaches them between their turns. Only what changes how someone else acts — never progress reports, never your own status.", map[string]any{
			"line": prop("string", "one sentence the rest of the job needs"),
		}, "line"))
	}
	// The optional schemas ride the prompt only once this leaf has a reason to
	// carry them: a structural one it was armed with before turn 1, or the
	// worker's own request through the discovery tool.
	//
	// They are emitted in ARMING order, and that is the whole of what makes
	// arming an append. A fixed order looks tidier and is wrong: with the media
	// schemas written above read_document, a leaf that armed documents first and
	// media second would have five definitions inserted IN FRONT of the schema
	// it was already carrying, which is a rewrite of the middle of the block and
	// costs the entire transcript behind it. Emitting in the order they arrived
	// means the newest schema is always last, whatever the route in.
	//
	// Each optional schema is admitted on its own name, not on its family's,
	// so an attached screenshot buys view_image without also buying a video
	// generator it has no use for.
	catalog := t.optionalDefinitions()
	for _, name := range t.armedInOrder() {
		if definition, available := catalog[name]; available {
			definitions = append(definitions, definition)
		}
	}
	return definitions
}

// optionalDefinitions is every schema this machine could arm, keyed by name.
// A family the brain has not wired produces no entries, so a leaf that armed a
// tool the machine cannot serve carries no schema for it — and is answered in
// prose by capabilities instead.
func (t *Toolbox) optionalDefinitions() map[string]ai.ToolDefinition {
	catalog := map[string]ai.ToolDefinition{}
	if t.media == nil {
		return catalog
	}
	if t.media.Provider != nil {
		for _, definition := range []ai.ToolDefinition{
			define("generate_image", "Generate one or more images into the workspace media directory. reference_paths may name existing workspace images for image-to-image work.", map[string]any{
				"prompt":          prop("string", "what to generate"),
				"n":               prop("integer", "number of images, default 1, maximum 10"),
				"size":            prop("string", "optional image size or aspect ratio"),
				"model":           prop("string", mediaModelArgDescription),
				"reference_paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "workspace image paths to use as references"},
			}, "prompt"),
			define("generate_music", "Generate a music clip as an MP3 in the workspace media directory.", map[string]any{
				"prompt": prop("string", "music prompt or lyrics"),
				"format": prop("string", "optional output format; mp3 is currently supported"),
				"model":  prop("string", mediaModelArgDescription),
			}, "prompt"),
			define("generate_video", "Generate a video into the workspace media directory. This call waits for the asynchronous provider job to finish, for up to ten minutes. The first two reference_paths become first/last frames; any remaining images are style references.", map[string]any{
				"prompt":          prop("string", "what to generate"),
				"duration":        prop("integer", "optional duration in seconds"),
				"resolution":      prop("string", "optional resolution such as 480p, 720p, or 1080p"),
				"aspect_ratio":    prop("string", "optional aspect ratio such as 16:9"),
				"model":           prop("string", mediaModelArgDescription),
				"reference_paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "workspace image paths for first/last frames and style guidance"},
			}, "prompt"),
			define("speak", "Synthesize speech as an MP3 in the workspace media directory.", map[string]any{
				"text":  prop("string", "text to speak"),
				"voice": prop("string", "optional voice, default alloy"),
				"model": prop("string", mediaModelArgDescription),
			}, "text"),
			define("view_image", "Look at a workspace image. If the current model cannot see, a vision model looks and reports back — pass question for a targeted check.", map[string]any{
				"path":     prop("string", "workspace-relative image path"),
				"question": prop("string", "optional targeted question about the image"),
			}, "path"),
		} {
			catalog[definition.Function.Name] = definition
		}
	}
	if t.media.DocumentClient != nil {
		catalog["read_document"] = define("read_document", "Read a PDF into text. Free and local when possible; scanned documents escalate to OCR through the rail. Repeat reads are cached.", map[string]any{
			"path":     prop("string", "workspace-relative PDF, DOCX, or PPTX path"),
			"pages":    prop("string", "optional PDF page range such as 1-5"),
			"question": prop("string", "optional question the worker will answer from the extracted text"),
		}, "path")
	}
	return catalog
}

func reflexPromotionDefinition() ai.ToolDefinition {
	return define("promote", "Stop this reflex and hand the original request to a full job. Use immediately when the action is not one obvious reversible step. partial says what you learned or changed before stopping.", map[string]any{
		"partial": prop("string", "useful partial result or discovery for the full job to build on"),
	}, "partial")
}

// reflexPromotion reads the executor's explicit larger-than-it-looked verdict.
// The promote call is intercepted by Linear and never reaches the toolbox.
func reflexPromotion(calls []ai.ToolCall) (string, bool) {
	for _, call := range calls {
		if call.Function.Name != "promote" {
			continue
		}
		var args struct {
			Partial string `json:"partial"`
		}
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return "", true
		}
		return strings.TrimSpace(args.Partial), true
	}
	return "", false
}

// Execute dispatches one call. An unknown name is answered with the valid list
// rather than refused, because a model that guessed a tool name can recover
// from being told the real ones and cannot recover from a dead loop.
func (t *Toolbox) Execute(ctx context.Context, name string, arguments string) (outcome Result) {
	// A tool that panics is one bad call, not a dead worker. The model reads
	// the fault the way it reads any other tool error and picks another move;
	// the stack goes to the log, where it is useful.
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = guard.Note("exec/tool "+name, recovered)
			outcome = errorf("internal fault in this tool call — recorded to the log: %v. Try a different approach.", recovered)
		}
	}()
	var args map[string]any
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return t.finishResult(errorf("arguments were not valid JSON: %v", err))
		}
	}
	var result Result
	switch name {
	case "sh":
		result = t.sh(ctx, args)
	case "job":
		result = t.job(args)
	case "write":
		result = t.write(args)
	case "edit":
		result = t.edit(args)
	case "web":
		result = t.webCall(ctx, args)
	case "recall":
		result = t.recall(args)
	case "generate_image":
		result = t.generateImage(ctx, args)
	case "generate_music":
		result = t.generateMusic(ctx, args)
	case "generate_video":
		result = t.generateVideo(ctx, args)
	case "speak":
		result = t.speak(ctx, args)
	case "view_image":
		result = t.viewImage(ctx, args)
	case "read_document":
		result = t.readDocument(ctx, args)
	case "capabilities":
		result = t.capabilities(args)
	case "share":
		result = t.shareLine(args)
	default:
		available := "sh, job, write, edit, web"
		if t.history != nil {
			available += ", recall"
		}
		// Only what this leaf is actually holding is named, plus the door to
		// the rest. A model told about a tool it does not have is a model that
		// will call it next turn and be told the same thing again.
		for _, family := range []string{FamilyMedia, FamilyDocument} {
			for _, tool := range familyTools[family] {
				if t.isArmed(tool) {
					available += ", " + tool
				}
			}
		}
		if families := t.offered(); len(families) > 0 {
			available += ", capabilities (loads: " + strings.Join(families, ", ") + ")"
		}
		result = errorf("no tool named %q. Available: %s", name, available)
	}
	return t.finishResult(result)
}

// finishResult is the single turn boundary for every tool, including optional
// recall and media tools. With no jobs it returns spill's value untouched.
func (t *Toolbox) finishResult(result Result) Result {
	result = t.spill(result)
	if report := t.jobs.report(); report != "" {
		result.Content = clamp(result.Content + "\n\n" + report)
		result.reportedJobs = true
	}
	return result
}

type recallToolFact struct {
	NodeID   string         `json:"node_id,omitempty"`
	Scope    string         `json:"scope"`
	Kind     store.FactKind `json:"kind"`
	Body     string         `json:"body"`
	Pointers []string       `json:"pointers"`
	Age      string         `json:"age"`
}

type recallToolResponse struct {
	Folds    []store.RecallHit `json:"folds"`
	Notebook []recallToolFact  `json:"notebook"`
}

func (t *Toolbox) recall(args map[string]any) Result {
	if t.history == nil {
		return errorf("recall is not available without an attached store")
	}
	terms := strings.TrimSpace(stringArg(args, "terms"))
	cues := stringsArg(args, "scope_cues")
	if terms == "" && len(cues) == 0 {
		return errorf("recall needs terms or scope_cues")
	}
	limit := intArg(args, "limit", 5)
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	folds, err := t.history.Recall(terms, cues, limit)
	if err != nil {
		return errorf("recall folds: %v", err)
	}
	facts, err := t.history.SearchFacts(store.FactQuery{Cues: recallFactCues(cues), Terms: terms, Limit: limit})
	if err != nil {
		return errorf("recall notebook: %v", err)
	}

	response := recallToolResponse{Folds: make([]store.RecallHit, 0), Notebook: make([]recallToolFact, 0)}
	for _, hit := range folds {
		hit.Intent = recallClip(hit.Intent, 512)
		hit.Digest = recallClip(hit.Digest, 2<<10)
		if len(hit.Pointers) > 8 {
			hit.Pointers = hit.Pointers[:8]
		}
		for index := range hit.Pointers {
			hit.Pointers[index] = recallClip(hit.Pointers[index], 512)
		}
		candidate := response
		candidate.Folds = append(append([]store.RecallHit(nil), response.Folds...), hit)
		if recallJSONFits(candidate) {
			response = candidate
		}
	}
	now := time.Now().UTC()
	for _, fact := range facts {
		item := recallToolFact{NodeID: fact.NodeID, Scope: recallClip(fact.Scope, 256), Kind: fact.Kind,
			Body: recallClip(fact.Body, store.MaxFactBytes), Pointers: t.foldPointers(fact.NodeID),
			Age: store.AgeLabel(fact.Time, now)}
		candidate := response
		candidate.Notebook = append(append([]recallToolFact(nil), response.Notebook...), item)
		if recallJSONFits(candidate) {
			response = candidate
		}
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return errorf("encode recall: %v", err)
	}
	return Result{Content: string(encoded)}
}

func (t *Toolbox) foldPointers(nodeID string) []string {
	for nodeID != "" && nodeID != store.RootID {
		node, ok, err := t.history.Node(nodeID)
		if err != nil || !ok {
			return nil
		}
		if node.FoldRoot {
			pointers := append([]string(nil), node.FoldPointers...)
			if len(pointers) > 8 {
				pointers = pointers[:8]
			}
			for index := range pointers {
				pointers[index] = recallClip(pointers[index], 512)
			}
			return pointers
		}
		nodeID = node.Parent
	}
	return nil
}

func recallFactCues(cues []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(cues)*3)
	add := func(cue string) {
		cue = strings.TrimSpace(cue)
		if cue == "" || seen[cue] {
			return
		}
		seen[cue] = true
		result = append(result, cue)
	}
	for _, cue := range cues {
		add(cue)
		if strings.Contains(cue, ":") {
			continue
		}
		cleaned := filepath.ToSlash(filepath.Clean(cue))
		add("file:" + cleaned)
		add("repo:" + cleaned)
		if parent := filepath.ToSlash(filepath.Dir(cleaned)); parent != "." && parent != cleaned {
			add("repo:" + parent)
		}
	}
	return result
}

func recallJSONFits(response recallToolResponse) bool {
	encoded, err := json.Marshal(response)
	return err == nil && len(encoded) <= maxRecallResultBytes
}

func recallClip(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return wholeRunesHead(value[:limit-len("...")]) + "..."
}

// wholeRunesHead and wholeRunesTail are the two halves of the same rule: a
// window cut at an arbitrary byte offset lands mid-character about half the
// time in any non-English text, and the replacement character it leaves behind
// rides every turn of the leaf that reads it. Every byte budget in this package
// is a budget, not a boundary, so both trim back to the nearest whole
// character rather than refusing to cut.
func wholeRunesHead(window string) string {
	// A head window can only close on an incomplete sequence, which is exactly
	// what DecodeLastRuneInString reports as a one-byte error. A genuine U+FFFD
	// in the text decodes at its true width and is left alone.
	for attempt := 0; attempt < utf8.UTFMax && len(window) > 0; attempt++ {
		if char, size := utf8.DecodeLastRuneInString(window); char != utf8.RuneError || size > 1 {
			break
		}
		window = window[:len(window)-1]
	}
	return window
}

func wholeRunesTail(window string) string {
	// A tail window can only open on a continuation byte, and a character is at
	// most UTFMax bytes long, so at most UTFMax-1 of them can precede the first
	// whole one.
	for attempt := 0; attempt < utf8.UTFMax && len(window) > 0; attempt++ {
		if window[0]&0xC0 != 0x80 {
			break
		}
		window = window[1:]
	}
	return window
}

// spill moves a large result out of context and leaves a pointer to it. Errors
// are never spilled: they are usually short, and the whole value of an error is
// that the model reads it immediately rather than going to fetch it.
func (t *Toolbox) spill(result Result) Result {
	if result.IsError || len(result.Content) <= spillBytes {
		return result
	}
	relative, ok := t.writeObs(fmt.Sprintf("%s-%d.txt", pathSlug(t.leaf), t.spills.Add(1)), result.Content)
	if !ok {
		return result
	}
	return Result{Content: fmt.Sprintf(
		"%s\n\n... [%d of %d bytes shown. Full output saved to %s — read the part you need with sh, for example: sed -n '1,80p' %s]",
		result.Content[:previewBytes], previewBytes, len(result.Content), relative, relative)}
}

// decaySpill preserves a decaying observation's full body under the
// workspace's observation directory. The file is named by tool call id, so
// writing is naturally idempotent — the decayer additionally guarantees it is
// invoked at most once per id.
func (t *Toolbox) decaySpill(toolCallID, body string) (string, bool) {
	return t.writeObs(fmt.Sprintf("%s-decay-%s.txt", pathSlug(t.leaf), safeName(toolCallID)), body)
}

// writeObs writes one observation file and returns the path to name it by. It
// is the one place spilled bytes land, shared by the size-triggered spill and
// the decay pass.
func (t *Toolbox) writeObs(name, content string) (string, bool) {
	full, shown, err := t.workspace.ScratchPath(filepath.Join(obsDir, name))
	if err != nil {
		return "", false
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", false
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "", false
	}
	return shown, true
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// safeName makes a tool call id usable as a file name. Ids differ at the tail,
// so that is the part kept when one is too long.
func safeName(id string) string {
	cleaned := unsafeName.ReplaceAllString(id, "")
	if len(cleaned) > 40 {
		cleaned = cleaned[len(cleaned)-40:]
	}
	if cleaned == "" {
		cleaned = "x"
	}
	return cleaned
}

func (t *Toolbox) sh(ctx context.Context, args map[string]any) Result {
	command := stringArg(args, "cmd")
	if command == "" {
		// An array is an explicit serial script: each step runs only when
		// the one before it succeeded, exactly like hand-written a && b.
		if list, ok := args["cmd"].([]any); ok {
			steps := make([]string, 0, len(list))
			for _, step := range list {
				if text, ok := step.(string); ok && strings.TrimSpace(text) != "" {
					steps = append(steps, strings.TrimSpace(text))
				}
			}
			command = strings.Join(steps, " && ")
		}
	}
	if command == "" {
		return errorf("sh needs cmd")
	}
	if boolArg(args, "bg") {
		// A background job is deliberately never compressed. Its output is
		// watched rather than read — readiness loops grep the log, the job tool
		// tails it, a promoted service keeps writing to it long after the leaf
		// is gone — and all of that is programmatic, which is indistinguishable
		// from parsing. There is also no fallback available once a job has
		// started, and a compressor with no way back is not one we can offer.
		return t.startBackground(ctx, command, args)
	}
	seconds := intArg(args, "t", 60)
	if seconds <= 0 || seconds > maxCommandSeconds {
		seconds = maxCommandSeconds
	}
	// A command writes files, and until this line nothing in the product knew
	// it. The registry behind Outcome.Artifacts was populated by write and edit
	// alone, so a chart rendered by a script under this tool was invisible to
	// the files footer, to the delivery gate and to every later node. The mark
	// is taken here, before anything runs, and read back once the command is
	// done — see produced.go for why one sweep afterwards rather than two.
	mark := producedMark(time.Now())
	defer t.recordProduced(mark)
	// rtk compresses what the command said before the model has to pay for it,
	// on every later turn as well as this one. It only ever stands in for the
	// plain command when it can be trusted to have said the same thing: see
	// trustworthy, which sends anything doubtful back to the shell itself.
	if tool, ok := rtk.Available(); ok {
		if wrapped, class := tool.Wrap(ctx, command); class != rtk.ClassNone {
			run := t.runShell(ctx, wrapped, seconds, tool.Path)
			if run.trustworthy(class) {
				return run.result(seconds)
			}
			if rtk.Failed(run.exitCode, run.body) {
				tool.Ban(command)
			}
		}
	}
	return t.runShell(ctx, command, seconds, "").result(seconds)
}

// shellRun is one command's whole outcome, kept separate from the Result it
// becomes so a wrapped run can be weighed and discarded before it is spoken.
type shellRun struct {
	body     string
	err      error
	exitCode int
	timedOut bool
	detached bool
}

// trustworthy asks whether a wrapped run may stand as the answer.
//
// A timeout stands as it is: it belongs to the command, not to the wrapping,
// and waiting for it a second time would spend the leaf's budget twice to learn
// nothing. rtk failing to run what it was handed never stands. Beyond that a
// check is believed whatever it exits with — a failing test suite is reporting,
// and its compressed failure is the most valuable output rtk produces — while a
// read that fails is asked again plain, because "no such file" has to reach the
// model as the shell's own sentence and asking twice costs nothing.
func (r shellRun) trustworthy(class rtk.Class) bool {
	if r.timedOut || r.detached {
		return true
	}
	if rtk.Failed(r.exitCode, r.body) {
		return false
	}
	return class == rtk.ClassCheck || r.exitCode == 0
}

func (r shellRun) result(seconds int) Result {
	if r.timedOut {
		return errorf("command timed out after %ds. Partial output:\n%s", seconds, r.body)
	}
	if r.detached {
		// The command itself finished; something it started in the background
		// kept the output pipe open until the grace ran out. That is a
		// completed command with a detached child, not a failure.
		return Result{Content: r.body + "\n(a background process the command started was left running detached)"}
	}
	if r.err != nil {
		// The exit status matters less than the output; a build failure's value
		// is entirely in what it printed.
		return Result{Content: fmt.Sprintf("exit: %v\n%s", r.err, r.body), IsError: true}
	}
	if strings.TrimSpace(r.body) == "" {
		return Result{Content: "(no output)"}
	}
	return Result{Content: r.body}
}

// runShell runs one command to completion in the workspace. rtkBin is the
// resolved rtk when this is a wrapped run and empty otherwise; a rewritten line
// calls rtk by bare name, so its shelf goes on PATH here.
func (t *Toolbox) runShell(ctx context.Context, command string, seconds int, rtkBin string) shellRun {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	var environment []string
	// The login shell may rewrite inherited PATH while reading its profile.
	// Export inside that shell so a shelf is added without changing the
	// benchmarked bare command path, which still runs with no Env set at all.
	if t.history != nil {
		if bin, err := store.SkillsBinDir(); err == nil {
			environment = os.Environ()
			environment = replaceEnv(environment, "AFORGE_SKILLS_BIN", bin)
			command = "export PATH=\"${AFORGE_SKILLS_BIN:?}:$PATH\"\n" + command
		}
	}
	if rtkBin != "" {
		if environment == nil {
			environment = os.Environ()
		}
		environment = replaceEnv(environment, "AFORGE_RTK_BIN", filepath.Dir(rtkBin))
		command = "export PATH=\"${AFORGE_RTK_BIN:?}:$PATH\"\n" + command
	}

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = t.workspace.Root()
	if environment != nil {
		cmd.Env = environment
	}
	// A command that leaves a background child sharing its stdout used to hang
	// the whole run: killing bash at the timeout is not enough, because Wait
	// blocks until every inherited pipe writer exits, and a scheduler goroutine
	// stuck there wedges the graph silently and forever. The process group
	// makes the timeout kill reach grandchildren, and WaitDelay force-closes
	// the pipes shortly after bash itself is gone for anything that survives —
	// a stuck tool call must cost its timeout, never the run.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 3 * time.Second
	// Collected rather than read whole. A command inside a fifteen-minute call
	// may print hundreds of megabytes and all but twelve kilobytes of them are
	// discarded a line later; the collector keeps only the part that survives,
	// including the nudge stripping, which it does a line at a time so that the
	// elided-byte count is counted on what a reader would have seen.
	var strip func([]byte) bool
	if rtkBin != "" {
		strip = func(start []byte) bool {
			line := string(start)
			return rtk.StripNudge(line) != line
		}
	}
	collected := newCappedOutput(strip)
	cmd.Stdout, cmd.Stderr = collected, collected
	err := cmd.Run()
	run := shellRun{body: collected.String(), err: err, exitCode: exitCode(cmd, err)}
	run.timedOut = runCtx.Err() == context.DeadlineExceeded
	run.detached = errors.Is(err, exec.ErrWaitDelay)
	return run
}

func exitCode(cmd *exec.Cmd, err error) int {
	if err == nil {
		return 0
	}
	if cmd.ProcessState != nil {
		if code := cmd.ProcessState.ExitCode(); code >= 0 {
			return code
		}
	}
	return -1
}

func (t *Toolbox) write(args map[string]any) Result {
	path, text := stringArg(args, "path"), stringArg(args, "text")
	if path == "" {
		return errorf("write needs path")
	}
	full, err := t.workspace.Resolve(path)
	if err != nil {
		return errorf("%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return errorf("could not create directory: %v", err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		return errorf("could not write %s: %v", path, err)
	}
	t.workspace.Record(t.leaf, full)
	return Result{Content: fmt.Sprintf("wrote %s (%d bytes)", path, len(text))}
}

func (t *Toolbox) edit(args map[string]any) Result {
	path, old, replacement := stringArg(args, "path"), stringArg(args, "old"), stringArg(args, "new")
	if path == "" || old == "" {
		return errorf("edit needs path and old")
	}
	full, err := t.workspace.Resolve(path)
	if err != nil {
		return errorf("%v", err)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return errorf("could not read %s: %v", path, err)
	}
	body := string(data)
	// Ambiguity is reported rather than resolved. Replacing the first of three
	// matches silently is the kind of edit that looks like it worked.
	switch strings.Count(body, old) {
	case 0:
		return errorf("that exact text is not in %s. Read the file and match it byte for byte.", path)
	case 1:
	default:
		return errorf("that text appears %d times in %s. Include more surrounding context so it matches once.", strings.Count(body, old), path)
	}
	updated := strings.Replace(body, old, replacement, 1)
	if err := os.WriteFile(full, []byte(updated), 0o644); err != nil {
		return errorf("could not write %s: %v", path, err)
	}
	t.workspace.Record(t.leaf, full)
	return Result{Content: fmt.Sprintf("edited %s (%d bytes)", path, len(updated))}
}

func (t *Toolbox) webCall(ctx context.Context, args map[string]any) Result {
	if t.web == nil {
		return errorf("web is not configured (set EXA_API_KEY)")
	}
	query := stringArg(args, "q")
	urls := stringsArg(args, "urls")
	if query == "" && len(urls) == 0 {
		return errorf("web needs q or urls")
	}
	var sections []string
	if query != "" {
		found, err := t.web.Search(ctx, query, intArg(args, "n", 6))
		if err != nil {
			return errorf("search failed: %v", err)
		}
		sections = append(sections, found)
	}
	if len(urls) > 0 {
		sections = append(sections, t.web.Fetch(ctx, urls))
	}
	return Result{Content: clamp(strings.Join(sections, "\n\n"))}
}

// clamp bounds a result at both ends.
//
// Keeping only the head is the obvious implementation and the wrong one: a
// command's most valuable line is usually its last, because that is where the
// error is. Cutting the middle keeps the shape of the output and the verdict at
// the end, and says plainly how much went missing so the model can go looking
// for it if it matters.
func clamp(text string) string {
	if len(text) <= maxToolResultBytes {
		return text
	}
	head := wholeRunesHead(text[:maxToolResultBytes*2/3])
	tail := wholeRunesTail(text[len(text)-(maxToolResultBytes-maxToolResultBytes*2/3):])
	return head +
		fmt.Sprintf("\n\n... [%d bytes elided] ...\n\n", len(text)-len(head)-len(tail)) +
		tail
}

func define(name, description string, properties map[string]any, required ...string) ai.ToolDefinition {
	parameters := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		parameters["required"] = required
	}
	return ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{
		Name: name, Description: description, Parameters: parameters,
	}}
}

func prop(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

func stringArg(args map[string]any, key string) string {
	if value, ok := args[key].(string); ok {
		return value
	}
	return ""
}

func boolArg(args map[string]any, key string) bool {
	value, _ := args[key].(bool)
	return value
}

func replaceEnv(environment []string, key, value string) []string {
	prefix := key + "="
	replaced := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			replaced = append(replaced, entry)
		}
	}
	return append(replaced, prefix+value)
}

func intArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	}
	return fallback
}

func stringsArg(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			values = append(values, text)
		}
	}
	return values
}
