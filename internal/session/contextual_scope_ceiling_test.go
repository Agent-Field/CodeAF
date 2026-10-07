package session

// SCOPE CEILING: the admission read ceiling, intersected with the live policy.
//
// An independent review reproduced a verified finding at the REAL production
// boundary (Agent.recordOutcome -> outcomeCollector.observeRead ->
// Agent.observeContextualDependencies -> Agent.contextualProducerReadAllowed):
// the producer-read consent gate was judged against the LIVE root policy at
// OBSERVATION time, with no frozen per-admission ceiling. So after the root
// anchored a NEW repository and gained a NEW blanket allow, an OLD delegated
// worker's already-admitted consumer receipt was re-observed through
// observeContextualDependencies and the framework then READ, HASHED and
// JOURNALED the OLD private producer that the admission policy DENIED.
//
// THE FIX: [delegatedOrigin.ReadCeiling] freezes the admission-time consent
// policy beside the rest of the origin, and every delegated producer re-read is
// judged by [Agent.contextualProducerReadAllowedUnder] -- the INTERSECTION of
// that ceiling and the live policy. An old admission that denied the producer
// can never widen (ceiling says no); a later deny still revokes (live says no);
// and an admission both halves allow is unchanged. The root path, which has no
// admission of its own, keeps answering to the live policy alone.
//
// These tests drive the real seams: [Agent.newTaskAgent] for the frozen-origin
// worker (a real task worker, and a nested one built from it), and
// [Agent.recordOutcome] for the worker's read at its real boundary. They then
// ask the CANONICAL store the only question that matters.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/gitidentity"
)

// anchorToNewRepo re-homes the conversation to a fresh repository and installs
// the given policy, so the worker's admission and the live gate can be made to
// disagree across an anchor exactly as the reviewer reproduced.
func anchorToNewRepo(t *testing.T, root *Agent, policy *approval.Policy) {
	t.Helper()
	otherRoot := t.TempDir()
	otherConsumerDir := filepath.Join(otherRoot, "other-report")
	otherProducerDir := filepath.Join(otherRoot, "other-core")
	for _, dir := range []string{otherConsumerDir, otherProducerDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("init: %s %v", out, err)
		}
	}
	key, err := gitidentity.ProjectKey(otherConsumerDir)
	if err != nil {
		t.Fatal(err)
	}
	root.config.Workspace = otherConsumerDir
	root.config.MemoryProjectKey = key
	root.config.ApprovalPolicy = policy
}

// A worker's late read re-forwards its WHOLE accumulated origin receipt set
// (contextual_delegated.go's observeRead), so an old consumer receipt is
// re-observed after the anchor. A worker admitted under a read:deny that denied
// the private sibling producer must NOT have that producer read merely because
// the root's NEW anchor brought a blanket allow.
func TestContextualScopeCeilingOldDenialDoesNotWidenUnderNewAllow(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	ctx := context.Background()

	root.config.ApprovalPolicy = &approval.Policy{Tools: map[string]approval.Action{"read": approval.ActionDeny}}
	root.prepareBindingContext(ctx, "wire the report to its producer")
	worker := spawnTaskWorker(t, root, filepath.Dir(consumer))
	if worker.origin.ReadCeiling == nil {
		t.Fatal("the admission ceiling was not frozen on the worker origin")
	}
	consumerBody, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatal(err)
	}
	worker.recordOutcome(ctx, 0, delegatedReadCall(t, consumer, "old-consumer"), toolResult{text: string(consumerBody)}, worker.captureSourceSnapshot(ctx).Identity)
	// AT ADMISSION the denied producer is not read, hashed or journaled.
	contextualForwardExpectNone(t, s, producer)

	// The root anchors a NEW repository and gains a NEW blanket allow.
	anchorToNewRepo(t, root, &approval.Policy{Default: approval.ActionAllow})

	late := filepath.Join(filepath.Dir(consumer), "late-note.txt")
	if err := os.WriteFile(late, []byte("hello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	worker.recordOutcome(ctx, 0, delegatedReadCall(t, late, "late-read"), toolResult{text: "hello\n"}, worker.captureSourceSnapshot(ctx).Identity)
	// The OLD producer stays refused: the ceiling, not the new grant, decides.
	contextualForwardExpectNone(t, s, producer)
}

// The revocation direction across an anchor: a worker admitted while the read
// was allowed does not keep reading its producer after the live policy denies
// it, because the intersection takes the narrow half.
func TestContextualScopeCeilingOldAllowRevokedByNewDeny(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	ctx := context.Background()

	root.config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	root.prepareBindingContext(ctx, "wire the report to its producer")
	worker := spawnTaskWorker(t, root, filepath.Dir(consumer))

	// The live policy is revoked across the anchor.
	anchorToNewRepo(t, root, &approval.Policy{Tools: map[string]approval.Action{"read": approval.ActionDeny}})

	consumerBody, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatal(err)
	}
	worker.recordOutcome(ctx, 0, delegatedReadCall(t, consumer, "late-consumer"), toolResult{text: string(consumerBody)}, worker.captureSourceSnapshot(ctx).Identity)
	contextualForwardExpectNone(t, s, producer)
}

// An admission both halves allow is UNCHANGED and permitted: the ceiling adds no
// new refusal to the ordinary allow, including across a root anchor.
func TestContextualScopeCeilingUnchangedAllowStillFormsEdge(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	ctx := context.Background()

	root.config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	root.prepareBindingContext(ctx, "wire the report to its producer")
	worker := spawnTaskWorker(t, root, filepath.Dir(consumer))

	anchorToNewRepo(t, root, &approval.Policy{Default: approval.ActionAllow})

	consumerBody, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatal(err)
	}
	worker.recordOutcome(ctx, 0, delegatedReadCall(t, consumer, "late-consumer"), toolResult{text: string(consumerBody)}, worker.captureSourceSnapshot(ctx).Identity)
	contextualForwardExpectEdge(t, s, producer, consumer)
}

// A NESTED worker inherits its parent's frozen admission ceiling, so a tree
// launched under a deny cannot widen any of its children merely because the root
// later pushes a blanket allow.
func TestContextualScopeCeilingNestedInheritsAdmissionCeiling(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	ctx := context.Background()

	root.config.ApprovalPolicy = &approval.Policy{Tools: map[string]approval.Action{"read": approval.ActionDeny}}
	root.prepareBindingContext(ctx, "wire the report to its producer")
	worker := spawnTaskWorker(t, root, filepath.Dir(consumer))
	nested := spawnTaskWorker(t, worker, filepath.Dir(consumer))
	if nested.origin.ReadCeiling == nil {
		t.Fatal("the nested child did not inherit an admission ceiling")
	}

	// The root gains a blanket allow after the whole tree was admitted.
	anchorToNewRepo(t, root, &approval.Policy{Default: approval.ActionAllow})

	consumerBody, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatal(err)
	}
	nested.recordOutcome(ctx, 0, delegatedReadCall(t, consumer, "nested-consumer"), toolResult{text: string(consumerBody)}, nested.captureSourceSnapshot(ctx).Identity)
	contextualForwardExpectNone(t, s, producer)
}
