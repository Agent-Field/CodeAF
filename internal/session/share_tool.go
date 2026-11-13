package session

// share_tool.go is the share_deliverable verb: the agent's one way to hand the
// person a file it means as a result, and nothing else.
//
// ── WHO IS RECORDED IS DECIDED BY CONSTRUCTION ──
//
// The conversation records under its own index and session. A task worker has
// none of its own (deliverable_task.go): it records ONLY through the sink its
// owning conversation built it with, so every row is the conversation's,
// carrying the node's id and the node's captured asking message. Run and call
// are read off the dispatch itself (runIdentity, callIDFrom) — never the latest
// message, never the nearest reply, never a path, a clock or a view.
//
// ── ABSENT WHERE IT CANNOT WORK ──
//
// A conversation with no index, or a worker built with no sink, has nowhere a
// shared row could land, and is not given the verb at all.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// shareSharedFolder is the folder, inside the owning conversation's own
// artifacts folder, that a worker's file is copied into when the worker's tree
// is not the owner's to keep.
const shareSharedFolder = "shared"

// shareDeliverableDescription is general on purpose: it is about returning a
// FILE as a result, whatever the file is. A chart, a report, a recording, a
// spreadsheet and an archive are all shared the same way.
var shareDeliverableDescription = "Hand the person files you produced or prepared as results, listed as shared by you in this conversation's deliverables. Skip intermediates and files already recorded by `write` or generators. Accepts any regular file kind, up to " + strconv.Itoa(shareOpPathsMax) + " files and " + byteLimit(int(shareFileBytes)) + " in all per call. Paths: workspace-relative or absolute, inside this conversation's folders. Use a NEW operation_id per deliberate share; reuse ONLY to retry it after busy, pending or unknown. Retries never record twice. Reads and records the file once as it is then; claims no authorship and charges nothing."

const shareDeliverableSchemaJSON = `{"type":"object","properties":{"operation_id":{"type":"string","description":"Your id for this share (1-128 characters)."},"paths":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":8}},"required":["operation_id","paths"],"additionalProperties":false}`

// shareTools is the verb, where a shared row has somewhere real to land.
func (a *Agent) shareTools() []bare.Tool {
	if !a.mayShare() {
		return nil
	}
	return []bare.Tool{{
		Name:        "share_deliverable",
		Description: shareDeliverableDescription,
		Schema:      json.RawMessage(shareDeliverableSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				OperationID string   `json:"operation_id"`
				Paths       []string `json:"paths"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return rejectToolArguments(ctx, "Invalid arguments: "+err.Error()), true, nil
			}
			return a.shareDeliverable(ctx, parsed.OperationID, parsed.Paths)
		},
	}}
}

// mayShare is the presence predicate: an index and a session to record under.
func (a *Agent) mayShare() bool {
	if a.config.InTask {
		sink := a.config.deliverSink
		return sink != nil && sink.node != nil && sink.index != "" && sink.session != ""
	}
	return strings.TrimSpace(a.config.ArtifactsIndex) != ""
}

// shareDeliverable runs one share and answers its receipt as JSON. A refusal is
// a tool error the model can act on, never a Go error that ends the turn.
func (a *Agent) shareDeliverable(ctx context.Context, operationID string, paths []string) (string, bool, error) {
	target, ok := a.shareTarget(ctx)
	if !ok {
		return "share_deliverable is not available here: there is no conversation to record under.", true, nil
	}
	result := publishShare(ctx, target, operationID, paths)
	raw, err := json.Marshal(result)
	if err != nil {
		return "share_deliverable could not describe its receipt.", true, nil
	}
	return string(raw), result.Status != ShareOpAccepted, nil
}

// shareTarget is where this call records, from the actual dispatch authority.
func (a *Agent) shareTarget(ctx context.Context) (shareTarget, bool) {
	prov := ShareProvenance{Run: runIdentity(ctx), Call: callIDFrom(ctx)}
	if a.config.InTask {
		return a.taskShareTarget(prov)
	}
	session := a.journalID()
	if a.config.ArtifactsIndex == "" || session == "" {
		return shareTarget{}, false
	}
	prov.Turn = askedMessageID(ctx)
	return shareTarget{
		index: a.config.ArtifactsIndex, session: session, base: a.config.Workspace,
		roots: a.deliverableRoots(), prov: prov, emit: a.emitShared,
	}, true
}

// taskShareTarget is a worker's target: the owning conversation's index,
// session and folders through its sink, the node's id and captured asking
// message, and the worker's own tree as a folder that may only be read from.
func (a *Agent) taskShareTarget(prov ShareProvenance) (shareTarget, bool) {
	sink := a.config.deliverSink
	if sink == nil || sink.node == nil || sink.index == "" || sink.session == "" || sink.owner == nil {
		return shareTarget{}, false
	}
	if source := sink.node.source(); source.valid() && source.Kind == TaskSourceMessage {
		prov.Turn = source.ID
	}
	owner := sink.owner
	target := shareTarget{
		index: sink.index, session: sink.session, base: sink.dir, roots: owner.deliverableRoots(),
		prov: prov, task: strconv.FormatUint(sink.node.id, 10), emit: owner.emitShared,
	}
	if !sink.inPlace() && strings.TrimSpace(owner.config.Place.Dir) != "" {
		// The worker's tree is not the owner's to keep: a file there is copied
		// into the owner's own artifacts folder, where it is recorded and opened.
		target.reach = []string{sink.dir}
		target.copies = filepath.Join(canonicalFile(owner.config.Place.Artifacts()), shareSharedFolder)
	}
	return target, true
}

// emitShared tells the conversation's watchers of one row found on the index:
// the deliverable, and the media source when the row has one.
func (a *Agent) emitShared(row Artifact) {
	if row.Media != nil {
		a.emitMediaChange(row.Media.ID, *row.Media)
	}
	a.emitArtifactChange(row)
}
