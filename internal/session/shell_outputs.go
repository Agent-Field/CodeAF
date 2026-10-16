package session

// shell_outputs.go lets a shell command PUBLISH a picture it made: the model
// lists the path in bash's optional `outputs` argument, the engine looks at that
// path before the command runs and again after it ended, and a file it can
// vouch for becomes a media source tied to this very run and tool call.
//
// ── WHAT IS NEVER DONE ──
//
// Nothing here reads the command, its output, a file name or the answer text to
// decide that something was produced. There is no scan of the workspace and no
// "nearest" or "latest" file. The only inputs are the paths the model declared,
// two looks at each, and the exit status bare reported through a structured
// door ([bare.WithBashExit]). A picture the agent merely LOOKED at stays an
// evidence row and is never promoted to anything made.
//
// ── WHAT IS CLAIMED ──
//
// `created` means exactly: absent before this command, a regular file after it.
// An existing file is published as found (`unchanged`, or `modified` when its
// identity moved); it is never a Created deliverable. Presence says nothing
// about a paid generation, so the charge stays unknown. A command that did not
// end in this call (background, handed to a job, timed out, cancelled) claims
// nothing: the file it may write later is not this call's to promise.
//
// ── THE COST IS BOUNDED ──
//
// At most [shellOutputsMax] paths, each under the real image limit, one total
// read budget and ONE deadline for the whole inspection. It is sequential in the
// calling goroutine: no goroutine per file, and no lock is held while a file is
// read (the index is touched once, after every byte was hashed).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// MediaPublished is the origin of a file a shell command declared as an output.
// It is not generated (nothing here proves a tool made it, still less a paid
// generation) and not evidence (nobody looked at it to answer a question).
const MediaPublished = "published"

// What the engine observed about a published file. Each is a statement about
// the file around ONE command, never about who wrote it.
const (
	ShellCreated   = "created"   // absent before the command, a regular file after
	ShellUnchanged = "unchanged" // there before and the same file after
	ShellModified  = "modified"  // there before and a different version after
	ShellUnknown   = "unknown"   // the before-state or the writers cannot be vouched for
)

const (
	shellOutputsMax    = 8
	shellOutputPathMax = 4096
)

// The bounds of one inspection. They are variables only so a test can make a
// small file look large or a clock run out.
var (
	shellOutputsDeadline       = 10 * time.Second
	shellOutputsHashBudget     = int64(64 << 20)
	shellOutputsChunk          = int64(64 << 10)
	shellOutputsAfterHash      = func() {}
	shellOutputsBeforeSnapshot = func() {}
)

type shellState int

const (
	shellAbsent shellState = iota
	shellRegular
	shellOther
	shellUnreadable
)

// shellBefore is what one declared path was just before the command ran.
type shellBefore struct {
	declared string
	path     string
	state    shellState
	info     os.FileInfo
}

// shellOutputsArgs reads the one argument this file owns. A call that does not
// decode, or names none, is not this file's business and is handed on intact.
func shellOutputsArgs(args json.RawMessage) (outputs []string, declared bool) {
	var parsed struct {
		Command string   `json:"command"`
		Outputs []string `json:"outputs"`
	}
	if err := decodeToolArguments(args, &parsed); err != nil || len(parsed.Outputs) == 0 {
		return nil, false
	}
	return parsed.Outputs, true
}

// shellOutputsProperty is the property added to bash's schema.
var shellOutputsProperty = fmt.Sprintf(`{"type":"array","maxItems":%d,"items":{"type":"string"},"description":"Paths of pictures this command makes for your answer, declared BEFORE it runs (at most %d, under the working folder). Registered only when the command ends in this call."}`, shellOutputsMax, shellOutputsMax)

// shellOutputsSentence is the model's whole guidance, written once.
var shellOutputsSentence = fmt.Sprintf(" To show a picture this command makes (a chart, a plot, a rendering) in your answer, list its path in outputs (at most %d, png, jpeg, gif or webp under the working folder). It is recorded only if the command finishes in this call: not with background:true and not when the call is handed to a job. A file that was already there is shown as it was found, not as made now.", shellOutputsMax)

// mayPublishShellOutputs says whether this agent can record a published file at
// all. A capability that cannot work is absent, not broken. THE BELT IS COMPOSED
// BEFORE THE JOURNAL EXISTS, so the session's id cannot be part of this test; a
// call made while there is still none says so and records nothing.
func (a *Agent) mayPublishShellOutputs() bool {
	return !a.config.InTask && a.config.ArtifactsIndex != ""
}

// schemaWithShellOutputs adds the outputs property to bash's schema, or leaves
// the schema alone when it will not decode.
func schemaWithShellOutputs(schema json.RawMessage) json.RawMessage {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(schema, &root); err != nil {
		return schema
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(root["properties"], &properties); err != nil {
		return schema
	}
	properties["outputs"] = json.RawMessage(shellOutputsProperty)
	encodedProperties, err := json.Marshal(properties)
	if err != nil {
		return schema
	}
	root["properties"] = encodedProperties
	encoded, err := json.Marshal(root)
	if err != nil {
		return schema
	}
	return encoded
}

// publishingBash is bash with outputs. Without outputs it IS the inner tool for
// that call, arguments and result untouched.
func (a *Agent) publishingBash(inner bare.Tool) bare.Tool {
	if !a.mayPublishShellOutputs() {
		return inner
	}
	wrapped := inner
	wrapped.Description = inner.Description + shellOutputsSentence
	wrapped.Schema = schemaWithShellOutputs(inner.Schema)
	wrapped.Execute = func(ctx context.Context, args json.RawMessage) (string, bool, error) {
		outputs, declared := shellOutputsArgs(args)
		if !declared {
			return inner.Execute(ctx, args)
		}
		if a.journalID() == "" {
			text, isError, err := inner.Execute(ctx, args)
			return text + "\n\nOutputs not registered: this conversation has no record to keep them in.", isError, err
		}
		befores, refusal := a.snapshotShellOutputs(outputs)
		if refusal != "" {
			return rejectToolArguments(ctx, "Invalid arguments: "+refusal), true, nil
		}
		busy := a.otherProcessesRunning()
		exited := false
		observed := bare.WithBashExit(ctx, func(code int) { exited = code == 0 })
		text, isError, err := inner.Execute(observed, args)
		if err != nil {
			return text, isError, err
		}
		if !exited || ctx.Err() != nil {
			return text + "\n\nOutputs not registered: the command did not finish with exit status 0 in this call (it failed, timed out, was interrupted, or was handed to a job).", isError, nil
		}
		busy = busy || a.otherProcessesRunning()
		return text + "\n\n" + a.publishShellOutputs(ctx, befores, busy), isError, nil
	}
	return wrapped
}

// otherProcessesRunning says some background process of this session was
// running: it could have written a declared path, so absence-before is no
// longer proof that THIS command made it.
func (a *Agent) otherProcessesRunning() bool {
	return a.jobs != nil && a.jobs.runningProcesses() > 0
}

// shellOutputPath is the absolute, cleaned spelling of a declared path.
func (a *Agent) shellOutputPath(declared string) string {
	if !filepath.IsAbs(declared) {
		declared = filepath.Join(a.config.Workspace, declared)
	}
	return filepath.Clean(declared)
}

// snapshotShellOutputs validates the declaration and looks at every path ONCE,
// immediately before the command runs. It never opens a file: a path that is a
// FIFO must not be able to hold a tool call.
func (a *Agent) snapshotShellOutputs(declared []string) ([]shellBefore, string) {
	if len(declared) > shellOutputsMax {
		return nil, fmt.Sprintf("outputs takes at most %d paths", shellOutputsMax)
	}
	shellOutputsBeforeSnapshot()
	seen := map[string]bool{}
	out := make([]shellBefore, 0, len(declared))
	for _, raw := range declared {
		switch {
		case strings.TrimSpace(raw) == "":
			return nil, "outputs holds an empty path"
		case len(raw) > shellOutputPathMax:
			return nil, fmt.Sprintf("an outputs path is over %d bytes", shellOutputPathMax)
		case strings.ContainsRune(raw, 0):
			return nil, "an outputs path holds a NUL byte"
		}
		path := a.shellOutputPath(raw)
		if base := filepath.Base(filepath.Clean(raw)); base == "." || base == string(filepath.Separator) || base == ".." {
			return nil, "an outputs path must name a file"
		}
		if seen[path] {
			return nil, "outputs names the same path twice"
		}
		seen[path] = true
		before := shellBefore{declared: raw, path: path}
		info, err := os.Lstat(path)
		switch {
		case err == nil && info.Mode().IsRegular():
			before.state, before.info = shellRegular, info
		case err == nil:
			before.state = shellOther
		case errors.Is(err, fs.ErrNotExist) || isNotDir(err):
			before.state = shellAbsent
		default:
			before.state = shellUnreadable
		}
		out = append(out, before)
	}
	return out, ""
}

func isNotDir(err error) bool {
	var pathErr *fs.PathError
	return errors.As(err, &pathErr) && strings.Contains(pathErr.Err.Error(), "not a directory")
}

// shellPublication is one inspected output ready to be written, or the reason
// it is not.
type shellPublication struct {
	declared string
	fact     MediaFact
	title    string
	kind     string
	note     string
}

// publishShellOutputs inspects every declared path and records what it can
// vouch for. It answers the lines the model reads. All hashing is done before
// the first index write.
func (a *Agent) publishShellOutputs(ctx context.Context, befores []shellBefore, busy bool) string {
	deadline, cancel := context.WithTimeout(ctx, shellOutputsDeadline)
	defer cancel()
	budget := shellOutputsHashBudget
	seen := map[string]bool{}
	results := make([]shellPublication, 0, len(befores))
	for _, before := range befores {
		result := a.inspectShellOutput(deadline, before, &budget, seen, busy)
		if ctx.Err() != nil {
			return "Outputs not registered: the call was interrupted."
		}
		results = append(results, result)
	}
	lines := make([]string, 0, len(results))
	// The call's identity is captured HERE, on the call's own context, and not
	// the call's inputs: a shell has no reference pictures or input messages.
	call := madeBy(ctx, nil)
	made := mediaMade{Run: call.Run, Tool: call.Tool, Asked: call.Asked}
	for _, result := range results {
		if result.note != "" {
			lines = append(lines, fmt.Sprintf("Output %s not registered: %s.", result.declared, result.note))
			continue
		}
		if !a.recordShellOutput(result, made) {
			lines = append(lines, fmt.Sprintf("Output %s: whether it was recorded is unknown.", result.declared))
			continue
		}
		lines = append(lines, fmt.Sprintf("Output %s registered (%s).", result.declared, result.fact.Change))
	}
	return strings.Join(lines, "\n")
}

// inspectShellOutput is the after-look at one path. A refusal is a note, never
// a guess.
func (a *Agent) inspectShellOutput(ctx context.Context, before shellBefore, budget *int64, seen map[string]bool, busy bool) shellPublication {
	out := shellPublication{declared: before.declared}
	refuse := func(note string) shellPublication { out.note = note; return out }
	if ctx.Err() != nil {
		return refuse("the inspection ran out of time")
	}
	info, err := os.Lstat(before.path)
	switch {
	case errors.Is(err, fs.ErrNotExist) || isNotDir(err):
		return refuse("the command did not leave a file there")
	case err != nil:
		return refuse("it could not be read")
	case !info.Mode().IsRegular():
		return refuse("it is not a regular file")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(before.path))
	if err != nil {
		return refuse("its folder could not be resolved")
	}
	canonical := filepath.Join(parent, filepath.Base(before.path))
	if !withinAny(a.deliverableRoots(), canonical) {
		return refuse("it is outside the working folder")
	}
	if seen[canonical] {
		return refuse("it is the same file as another output")
	}
	seen[canonical] = true
	size := info.Size()
	switch {
	case size <= 0:
		return refuse("it is empty")
	case size > maxImageBytes:
		return refuse("it is over the " + byteLimit(maxImageBytes) + " image limit")
	case size > *budget:
		return refuse("the read budget for this call is spent")
	}
	live := MediaLive{State: MediaAvailable, Path: canonical, Version: referenceIdentity(info)}
	file, opened, err := openLiveMedia(ctx, live, maxImageBytes, func(head []byte) bool { return sniffedImageTypes[sniffMedia(head)] }, func() error { return ErrMediaReference })
	if err != nil {
		if ctx.Err() != nil {
			return refuse("the inspection ran out of time")
		}
		return refuse("it is not a picture it can vouch for")
	}
	defer file.Close()
	*budget -= size
	sum := sha256.New()
	read, err := io.CopyBuffer(sum, referenceReader{ctx: ctx, reader: io.LimitReader(file, size+1)}, make([]byte, shellOutputsChunk))
	if err != nil {
		if ctx.Err() != nil {
			return refuse("the inspection ran out of time")
		}
		return refuse("it could not be read")
	}
	shellOutputsAfterHash()
	headLen := int64(mediaProbeBytes)
	if size < headLen {
		headLen = size
	}
	head := make([]byte, headLen)
	if _, err := file.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		return refuse("it could not be read")
	}
	after, err := file.Stat()
	if err != nil || read != size || referenceIdentity(after) != live.Version {
		return refuse("it changed while it was being read")
	}
	if now, err := os.Lstat(before.path); err != nil || !now.Mode().IsRegular() || referenceIdentity(now) != live.Version {
		return refuse("it changed while it was being read")
	}
	mime := sniffMedia(head)
	fact := MediaFact{Origin: MediaPublished, MIME: mime, Canonical: canonical, Bytes: size, Declared: before.declared}
	fact.SHA256 = hex.EncodeToString(sum.Sum(nil))
	fact.ModNS = opened.ModTime().UnixNano()
	fact.CtimeNS, fact.Dev, fact.Ino = fileIdentity(opened)
	if w, h, ok := imageSize(head); ok {
		fact.Width, fact.Height = w, h
	}
	fact.Change = classifyShellOutput(before, opened, busy)
	out.fact, out.kind, out.title = fact, "image", filepath.Base(canonical)
	return out
}

// classifyShellOutput is the only claim made about production. It compares the
// identity the file had before the command with the one it has now.
func classifyShellOutput(before shellBefore, after os.FileInfo, busy bool) string {
	switch before.state {
	case shellAbsent:
		if busy {
			return ShellUnknown
		}
		return ShellCreated
	case shellRegular:
		if referenceIdentity(before.info) == referenceIdentity(after) {
			return ShellUnchanged
		}
		return ShellModified
	}
	return ShellUnknown
}

// recordShellOutput writes the row, after every byte was hashed. Only a file
// observed to be created also gets a deliverable fact, and it is the same fact
// the write tool leaves.
func (a *Agent) recordShellOutput(result shellPublication, made mediaMade) bool {
	fact := result.fact.WithMade(made)
	artifact := Artifact{Path: fact.Canonical, Session: a.journalID(), Title: result.title, Kind: result.kind, Created: time.Now()}
	if fact.Change == ShellCreated {
		artifact.Deliver = a.generatedDeliverable(fact.Canonical, fact)
	}
	id, written := recordMediaRow(a.config.ArtifactsIndex, artifact, fact)
	if id == "" {
		return false
	}
	a.emitMediaChange(id, fact)
	a.emitArtifactChange(written)
	return true
}
