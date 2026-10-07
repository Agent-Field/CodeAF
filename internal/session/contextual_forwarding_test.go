package session

// PRODUCTION REGRESSION: A5 / live f06.
//
// In the live failure a person asked to inspect a report utility, run it and
// explain how it gets its amount. The root session made its OWN full `read` of
// report.py at the real execution boundary; the next call was handed to a quick
// task, and the child read the real producer (expense-core/export.py) with a
// compound shell line:
//
//	cd expense-report && python3 report.py; echo "exit=$?"; \
//	  echo ---EXPORT---; cat ../expense-core/export.py; echo ---LS-CORE---; ls ../expense-core
//
// The canonical journal ended with ZERO contextual_dependency rows.
//
// THE SHAPE OF THE FIX. The root's own read is captured at the execution
// boundary and forwarded into the frozen origin's observation before the
// condenser's stub can stand in for it. The link itself is then established
// from what the CONSUMER'S SOURCE LITERALLY NAMES plus a bounded framework
// re-read of that exact file. It is never inferred from a compound shell line:
// a global zero exit does not prove any one `cat` ran, and a substring of some
// command's output proves the file's current contents, not that the agent read
// it. The framework read is recorded with its own `framework-verify:` label so
// nobody can mistake it for an actual tool receipt.
//
// These tests drive the REAL seams \u2014 [Agent.recordMemoryTool] for the root's
// boundary read, [Agent.newTaskAgent] for the frozen-origin worker, and
// [Agent.recordOutcome] for the child read \u2014 and then ask the CANONICAL store
// the only question that matters: is there an observed dependency row, and does
// it name the exact two paths?

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

const contextualForwardProducerBody = "#!/usr/bin/env python3\nimport json\n\ndef receipt(amount=None):\n    return {\"merchant\": \"Stationery\", \"amount\": amount, \"currency\": \"USD\"}\n\nif __name__ == \"__main__\":\n    print(json.dumps(receipt()))\n"

const contextualForwardConsumerBody = "#!/usr/bin/env python3\nimport json\nimport pathlib\nimport subprocess\nimport sys\n\ndef summary():\n    producer = pathlib.Path(__file__).resolve().parent.parent / \"expense-core\" / \"export.py\"\n    data = json.loads(subprocess.check_output([sys.executable, str(producer)], text=True))\n    return \"{}: {} {:.2f}\".format(data.get(\"merchant\"), data.get(\"currency\"), data.get(\"amount\") / 100)\n\nif __name__ == \"__main__\":\n    print(summary())\n"

// contextualForwardingFixture lays two SEPARATE repositories with the exact
// producer/consumer shape the live case used: report.py resolves
// `Path(__file__).resolve().parent.parent / "expense-core" / "export.py"`.
func contextualForwardingFixture(t *testing.T) (*Agent, *store.Store, string, string) {
	t.Helper()
	root := t.TempDir()
	producerDir := filepath.Join(root, "expense-core")
	consumerDir := filepath.Join(root, "expense-report")
	for _, dir := range []string{producerDir, consumerDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("init: %s %v", out, err)
		}
	}
	producer := filepath.Join(producerDir, "export.py")
	consumer := filepath.Join(consumerDir, "report.py")
	if err := os.WriteFile(producer, []byte(contextualForwardProducerBody), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(consumer, []byte(contextualForwardConsumerBody), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := gitidentity.ProjectKey(consumerDir)
	if err != nil {
		t.Fatal(err)
	}
	a, s := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = consumerDir; c.MemoryProjectKey = key })
	return a, s, producer, consumer
}

// contextualForwardBoundaryRead records a full read the way [Agent.executeTool]
// does: through the real per-turn receipt store.
func contextualForwardBoundaryRead(t *testing.T, a *Agent, path, id string) {
	t.Helper()
	a.mu.Lock()
	turn := a.turnSeq
	a.mu.Unlock()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a.recordMemoryTool(context.Background(), turn, delegatedReadCall(t, path, id), toolResult{text: string(body)})
}

func contextualForwardPostTurn(t *testing.T, a *Agent, userMsg string) {
	t.Helper()
	a.observeContextualDependencies(a.memoryTurnSource(userMsg))
}

func contextualForwardEdges(t *testing.T, s *store.Store, producer string) []store.ContextualDependencyObservation {
	t.Helper()
	links, err := s.DependenciesForProducer(contextualPathOwner(producer), 8)
	if err != nil {
		t.Fatal(err)
	}
	return links
}

func contextualForwardExpectEdge(t *testing.T, s *store.Store, producer, consumer string) store.ContextualDependencyObservation {
	t.Helper()
	links := contextualForwardEdges(t, s, producer)
	if len(links) != 1 || links[0].ProducerPath != producer || links[0].ConsumerPath != consumer {
		t.Fatalf("canonical edge not observed: links=%+v", links)
	}
	return links[0]
}

func contextualForwardExpectNone(t *testing.T, s *store.Store, producer string) {
	t.Helper()
	if links := contextualForwardEdges(t, s, producer); len(links) != 0 {
		t.Fatalf("edge was fabricated: %+v", links)
	}
}

// ── the production regression ──────────────────────────────────────────────

// The root reads the consumer at its own boundary; the rest is condensed. The
// link is established from the consumer's literal reference plus a framework
// recheck, with no shell parsing anywhere.
func TestContextualForwardingRootReadVerifiesNamedProducer(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	ctx := context.Background()
	root.prepareBindingContext(ctx, "inspect this report utility, run it normally, and explain how it gets the amount")
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	contextualForwardPostTurn(t, root, "inspect this report utility, run it normally, and explain how it gets the amount")
	edge := contextualForwardExpectEdge(t, s, producer, consumer)
	// The producer side is explicitly a FRAMEWORK RECHECK, never a tool read.
	var framework bool
	for _, id := range edge.ReceiptIDs {
		if strings.HasPrefix(id, "framework-verify:") {
			framework = true
		}
	}
	if !framework {
		t.Fatalf("framework recheck was not labelled: %+v", edge.ReceiptIDs)
	}
	if !strings.Contains(edge.Assumption, "../expense-core/export.py") {
		t.Fatalf("assumption does not name the exact resolved path: %q", edge.Assumption)
	}
}

// A delegated worker's ACTUAL tool read of the producer, forwarded under the
// frozen origin, pairs with the root's own consumer read and keeps its own
// receipt id \u2014 an actual tool read, not a framework recheck.
func TestContextualForwardingChildToolReadPairsWithRootConsumerRead(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	ctx := context.Background()
	root.prepareBindingContext(ctx, "wire the report to its producer")
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	worker := spawnTaskWorker(t, root, filepath.Dir(consumer))
	producerBody, err := os.ReadFile(producer)
	if err != nil {
		t.Fatal(err)
	}
	call := delegatedReadCall(t, producer, "child-producer")
	worker.recordOutcome(ctx, 0, call, toolResult{text: string(producerBody)}, worker.captureSourceSnapshot(ctx).Identity)
	edge := contextualForwardExpectEdge(t, s, producer, consumer)
	if !strings.Contains(strings.Join(edge.ReceiptIDs, ","), "child-producer") {
		t.Fatalf("producer receipt was not the actual tool read: %+v", edge.ReceiptIDs)
	}
}

// Without the root's own read there is no consumer, and the child alone is not a
// dependency: the negative half of the regression.
func TestContextualForwardingChildProducerReadWithoutRootReadIsNotAnEdge(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	ctx := context.Background()
	root.prepareBindingContext(ctx, "inspect this report utility")
	worker := spawnTaskWorker(t, root, filepath.Dir(consumer))
	producerBody, err := os.ReadFile(producer)
	if err != nil {
		t.Fatal(err)
	}
	worker.recordOutcome(ctx, 0, delegatedReadCall(t, producer, "child-producer"), toolResult{text: string(producerBody)}, worker.captureSourceSnapshot(ctx).Identity)
	contextualForwardExpectNone(t, s, producer)
}

// ── negative controls ──────────────────────────────────────────────────────

// A stub the condenser filed is a COPY of a real file and may name the producer
// by its exact absolute path. It is still not source evidence.
func TestContextualForwardingStubReadIsNeverProvenance(t *testing.T) {
	root, s, producer, _ := contextualForwardingFixture(t)
	place := t.TempDir()
	root.config.Place = Place{Dir: place}
	stubDir := filepath.Join(place, "logs", "stubs")
	if err := os.MkdirAll(stubDir, 0700); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(stubDir, "26a82f9920bde5dc.txt")
	if err := os.WriteFile(stub, []byte("PRODUCER = \""+producer+"\"\n"+contextualForwardConsumerBody), 0600); err != nil {
		t.Fatal(err)
	}
	contextualForwardBoundaryRead(t, root, stub, "stub-read")
	contextualForwardPostTurn(t, root, "inspect the report utility")
	contextualForwardExpectNone(t, s, producer)
}

// A refused read, a partial result and prose are never receipts.
func TestContextualForwardingRefusedPartialAndProseAreNotReceipts(t *testing.T) {
	root, _, _, consumer := contextualForwardingFixture(t)
	ctx := context.Background()
	body, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatal(err)
	}
	call := delegatedReadCall(t, consumer, "refused")
	if r := root.contextualReadReceipt(ctx, call, toolResult{text: string(body), refusedBy: "policy", harness: true}, memoryToolReceipt{ID: "refused", Tool: "read", Status: "refused"}); r.Path != "" {
		t.Fatalf("refused read became a receipt: %+v", r)
	}
	if r := root.contextualReadReceipt(ctx, call, toolResult{text: string(body[:30])}, memoryToolReceipt{ID: "partial", Tool: "read", Status: "ok"}); r.Path != "" {
		t.Fatalf("partial read became a receipt: %+v", r)
	}
	prose := delegatedBashCall("prose", "echo PRODUCER="+consumer)
	if r := root.contextualReadReceipt(ctx, prose, toolResult{text: "PRODUCER=" + consumer}, memoryToolReceipt{ID: "prose", Tool: "bash", Status: "ok"}); r.Path != "" {
		t.Fatalf("prose became a receipt: %+v", r)
	}
	// A bare `cat` is still the one recognized shell read; a compound line is not.
	if r := root.contextualReadReceipt(ctx, delegatedBashCall("compound", "cd x && python3 r.py; cat "+consumer), toolResult{text: string(body)}, memoryToolReceipt{ID: "compound", Tool: "bash", Status: "ok"}); r.Path != "" {
		t.Fatalf("a compound shell line became a receipt: %+v", r)
	}
}

// The consumer changed after its read and no longer names the producer.
func TestContextualForwardingStaleConsumerReferenceDropsTheEdge(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	if err := os.WriteFile(consumer, []byte("print('no producer reference here')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	contextualForwardPostTurn(t, root, "inspect the report utility")
	contextualForwardExpectNone(t, s, producer)
}

// Two files in ONE repository are the same owner; an observed dependency
// requires two distinct proven project owners.
func TestContextualForwardingSameProjectOwnerIsNotADependency(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("init: %s %v", out, err)
	}
	producer := filepath.Join(dir, "export.py")
	consumer := filepath.Join(dir, "report.py")
	if err := os.WriteFile(producer, []byte(contextualForwardProducerBody), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(consumer, []byte("import pathlib\nP = pathlib.Path(__file__).resolve().parent / \"export.py\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := gitidentity.ProjectKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, s := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = key })
	a.observeContextualDependencies(memoryTurnEvidence{Receipts: []memoryToolReceipt{contextualReviewFullRead(t, a, consumer, "c")}})
	contextualForwardExpectNone(t, s, producer)
}

// An unreadable named path proves nothing, and a symlink whose target lives in
// the CONSUMER'S OWN project is not a cross-project dependency.
func TestContextualForwardingUnreadableAndOwnProjectSymlinkAreNotDependencies(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	// A consumed path that does not exist on disk.
	if err := os.WriteFile(consumer, []byte("data = open(\""+filepath.Join(filepath.Dir(consumer), "missing", "export.py")+"\").read()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root.observeContextualDependencies(memoryTurnEvidence{Receipts: []memoryToolReceipt{contextualReviewFullRead(t, root, consumer, "c")}})
	contextualForwardExpectNone(t, s, producer)

	// A symlink in the consumer's own project pointing at a local helper.
	target := filepath.Join(filepath.Dir(consumer), "helper.py")
	if err := os.WriteFile(target, []byte("def helper(): return 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(consumer), "link.py")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(consumer, []byte("data = open(\""+link+"\").read()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root.observeContextualDependencies(memoryTurnEvidence{Receipts: []memoryToolReceipt{contextualReviewFullRead(t, root, consumer, "c2")}})
	links, err := s.DependenciesForProducer(contextualPathOwner(target), 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("an own-project symlink became a dependency: %+v", links)
	}
}

// A consumer that CONSUMES its producer by an exact absolute path is enough: the
// framework resolves the literal and rechecks the file, no chain required. A
// bare assignment or a printed string is only a mention and is refused — see
// [TestContextualForwardingCommentAndPrintMentionIsNoEdge].
func TestContextualForwardingAbsoluteLiteralReferenceIsVerified(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	if err := os.WriteFile(consumer, []byte("data = open(\""+producer+"\").read()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	contextualForwardPostTurn(t, root, "inspect the report utility")
	contextualForwardExpectEdge(t, s, producer, consumer)
}

// ── the pure forwarding helper ─────────────────────────────────────────────

// Only a trusted full read is forwardable; prose, a stub pointer and a bare
// successful shell text all carry no Path/Hash/Body and are dropped.
func TestContextualForwardedReadsCarriesOnlyFullReads(t *testing.T) {
	got := contextualForwardedReads([]memoryToolReceipt{
		{ID: "read", Path: "/p/a.py", Hash: strings.Repeat("a", 64), Body: "body"},
		{ID: "prose", Text: "I read a.py and it looks fine"},
		{ID: "stub", Path: "/state/logs/stubs/x.txt", Text: "pointer"},
		{ID: "partial", Path: "/p/b.py", Hash: strings.Repeat("b", 64)},
	})
	if len(got) != 1 || got[0].ID != "read" || got[0].Path != "/p/a.py" {
		t.Fatalf("forwarded reads = %+v", got)
	}
}

// ── the independent review's reproduced blockers ────────────────────────────

// A path merely MENTIONED — in a comment or a print/log string — is not a
// consumption. No canonical row is written and the framework reads nothing.
func TestContextualForwardingCommentAndPrintMentionIsNoEdge(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	mention := "#!/usr/bin/env python3\n" +
		"# unrelated note about \"" + producer + "\"\n" +
		"print(\"" + producer + "\")\n"
	if err := os.WriteFile(consumer, []byte(mention), 0600); err != nil {
		t.Fatal(err)
	}
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	contextualForwardPostTurn(t, root, "inspect the report utility")
	contextualForwardExpectNone(t, s, producer)
}

// A path in a THIRD, unrelated project is outside the consumer's authorized
// source scope. The framework neither reads it nor mints a cross-owner row, even
// though the consumer really does open it: a directory hash is not a grant.
func TestContextualForwardingThirdProjectQuotedPathIsRefused(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	third := filepath.Join(t.TempDir(), "third")
	if err := os.MkdirAll(third, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", third, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("init: %s %v", out, err)
	}
	foreign := filepath.Join(third, "secret.py")
	if err := os.WriteFile(foreign, []byte("SECRET = 42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(consumer, []byte("data = open(\""+foreign+"\").read()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	contextualForwardPostTurn(t, root, "inspect the report utility")
	contextualForwardExpectNone(t, s, foreign)
	contextualForwardExpectNone(t, s, producer)
}

// EIGHT quoted candidates ahead of the genuine pathlib chain do not starve it:
// the code-level chain is read first and the real edge is written.
func TestContextualForwardingDecoysDoNotStarveRealChain(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	// A SIBLING repository: its files are inside the consumer's neighbourhood, so
	// each one is a legitimate candidate that could consume the write cap.
	decoyDir := filepath.Join(filepath.Dir(filepath.Dir(consumer)), "decoy-core")
	if err := os.MkdirAll(decoyDir, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", decoyDir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("init: %s %v", out, err)
	}
	body := "files = [\n"
	for i := 0; i < contextualContextLimit; i++ {
		decoy := filepath.Join(decoyDir, "decoy"+string(rune('a'+i))+".py")
		if err := os.WriteFile(decoy, []byte("D = 1\n"), 0600); err != nil {
			t.Fatal(err)
		}
		body += "  open(\"" + decoy + "\").read(),\n"
	}
	body += "]\nimport pathlib\nP = pathlib.Path(__file__).resolve().parent.parent / \"expense-core\" / \"export.py\"\n"
	if err := os.WriteFile(consumer, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	contextualForwardPostTurn(t, root, "inspect the report utility")
	contextualForwardExpectEdge(t, s, producer, consumer)
}

// A source naming a DIFFERENT session cannot borrow this agent's live receipts,
// even at a colliding numeric turn.
func TestContextualForwardingWrongSessionDoesNotBorrowRootReceipts(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	root.observeContextualDependencies(memoryTurnEvidence{Session: "other-session", Turn: "other-session:0:deadbeef"})
	contextualForwardExpectNone(t, s, producer)
}

// A frozen origin admits only root reads that resolve to ITS admitted owner. A
// root anchor that re-homes the conversation to another repository mid-turn must
// not let the already-admitted worker borrow the post-anchor read.
func TestContextualForwardingFrozenOwnerDoesNotBorrowPostAnchorReads(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	ctx := context.Background()
	root.prepareBindingContext(ctx, "wire the report to its producer")
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	// The worker is admitted under the FIRST repository, so its origin freezes
	// that owner before the anchor below moves the conversation.
	worker := spawnTaskWorker(t, root, filepath.Dir(consumer))

	// A later anchor re-homes the conversation; the root's own read of the new
	// repository lands live under the SAME numeric turn.
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
	otherProducer := filepath.Join(otherProducerDir, "other_export.py")
	if err := os.WriteFile(otherProducer, []byte("def export(): return 7\n"), 0600); err != nil {
		t.Fatal(err)
	}
	otherConsumer := filepath.Join(otherConsumerDir, "other_report.py")
	if err := os.WriteFile(otherConsumer, []byte("data = open(\""+otherProducer+"\").read()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	otherKey, err := gitidentity.ProjectKey(otherConsumerDir)
	if err != nil {
		t.Fatal(err)
	}
	root.config.Workspace = otherConsumerDir
	root.config.MemoryProjectKey = otherKey
	contextualForwardBoundaryRead(t, root, otherConsumer, "anchor-read")

	// The worker finally reads its own producer at its real boundary.
	producerBody, err := os.ReadFile(producer)
	if err != nil {
		t.Fatal(err)
	}
	worker.recordOutcome(ctx, 0, delegatedReadCall(t, producer, "child-producer"), toolResult{text: string(producerBody)}, worker.captureSourceSnapshot(ctx).Identity)
	contextualForwardExpectEdge(t, s, producer, consumer)
	if links := contextualForwardEdges(t, s, otherProducer); len(links) != 0 {
		t.Fatalf("a post-anchor read of another project was borrowed into the frozen origin: %+v", links)
	}
}

// ── A4 RECEIPT AUTHORITY: the neighbourhood discovers, consent authorizes ─────
//
// The independent review of 34b4ecfa1 showed that contextualConsumerNeighborhood
// (the consumer repository's parent) is NOT an authorization grant: a source that
// genuinely LOOKS like it consumes a private sibling repository was read anyway.
// The authority to read the resolved producer now comes from the EXISTING consent
// policy (a.decide), and only an explicit ALLOW admits it.

// A policy that DENIES reading the producer path is sufficient: the framework
// mints no owner, reads and hashes nothing, and journals no row — for a direct
// literal AND a pathlib chain.
func TestContextualForwardingDeniedProducerPolicyIsNotRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		body func(producer string) string
	}{
		{"direct literal", func(producer string) string {
			return "data = open(\"" + producer + "\").read()\n"
		}},
		{"pathlib chain", func(string) string { return contextualForwardConsumerBody }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, s, producer, consumer := contextualForwardingFixture(t)
			root.config.ApprovalPolicy = &approval.Policy{Tools: map[string]approval.Action{"read": approval.ActionDeny}}
			if err := os.WriteFile(consumer, []byte(tc.body(producer)), 0600); err != nil {
				t.Fatal(err)
			}
			if root.contextualProducerReadAllowed(producer) {
				t.Fatal("a denied producer read was authorized")
			}
			contextualForwardBoundaryRead(t, root, consumer, "root-read")
			contextualForwardPostTurn(t, root, "inspect the report utility")
			contextualForwardExpectNone(t, s, producer)
		})
	}
}

// An ASK is a silent refusal during maintenance: no edge, and no consent prompt
// is opened (a.decide is pure policy, it never asks a person or a model).
func TestContextualForwardingAskedProducerPolicyDoesNotPrompt(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	root.config.ApprovalPolicy = &approval.Policy{Tools: map[string]approval.Action{"read": approval.ActionPrompt}}
	if root.contextualProducerReadAllowed(producer) {
		t.Fatal("an asked producer read was authorized")
	}
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	contextualForwardPostTurn(t, root, "inspect the report utility")
	contextualForwardExpectNone(t, s, producer)
}

// An explicit ALLOW (the ordinary configuration) still forms the canonical edge,
// and so does the configured-nothing build the rest of the suite runs with.
func TestContextualForwardingAllowedProducerPolicyStillFormsEdge(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	root.config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	if !root.contextualProducerReadAllowed(producer) {
		t.Fatal("an allowed producer read was refused")
	}
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	contextualForwardPostTurn(t, root, "inspect the report utility")
	contextualForwardExpectEdge(t, s, producer, consumer)
}

// A REVOCATION lands on the next observation: a worker admitted while the read
// was allowed does not keep a producer edge after the policy denies it.
func TestContextualForwardingRevokedProducerPolicyDropsEdge(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	root.prepareBindingContext(context.Background(), "wire the report to its producer")
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	worker := spawnTaskWorker(t, root, filepath.Dir(consumer))
	producerBody, err := os.ReadFile(producer)
	if err != nil {
		t.Fatal(err)
	}
	// The revocation is in force before the worker's read reaches the framework.
	root.config.ApprovalPolicy = &approval.Policy{Tools: map[string]approval.Action{"read": approval.ActionDeny}}
	worker.recordOutcome(context.Background(), 0, delegatedReadCall(t, producer, "child-producer"), toolResult{text: string(producerBody)}, worker.captureSourceSnapshot(context.Background()).Identity)
	contextualForwardExpectNone(t, s, producer)
}

// A CROSS-ANCHORED old job does not borrow a NEW root read permission: the root
// anchor re-homes the conversation and gains a blanket allow, but the worker's
// frozen origin only ever covers its own repository, never the post-anchor read
// of another project.
func TestContextualForwardingCrossAnchorDoesNotBorrowNewPermission(t *testing.T) {
	root, s, producer, consumer := contextualForwardingFixture(t)
	ctx := context.Background()
	root.prepareBindingContext(ctx, "wire the report to its producer")
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	worker := spawnTaskWorker(t, root, filepath.Dir(consumer))

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
	otherProducer := filepath.Join(otherProducerDir, "other_export.py")
	if err := os.WriteFile(otherProducer, []byte("def export(): return 7\n"), 0600); err != nil {
		t.Fatal(err)
	}
	otherConsumer := filepath.Join(otherConsumerDir, "other_report.py")
	if err := os.WriteFile(otherConsumer, []byte("data = open(\""+otherProducer+"\").read()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	otherKey, err := gitidentity.ProjectKey(otherConsumerDir)
	if err != nil {
		t.Fatal(err)
	}
	// The anchor moves the conversation and grants a NEW blanket allow.
	root.config.Workspace = otherConsumerDir
	root.config.MemoryProjectKey = otherKey
	root.config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	contextualForwardBoundaryRead(t, root, otherConsumer, "anchor-read")

	producerBody, err := os.ReadFile(producer)
	if err != nil {
		t.Fatal(err)
	}
	worker.recordOutcome(ctx, 0, delegatedReadCall(t, producer, "child-producer"), toolResult{text: string(producerBody)}, worker.captureSourceSnapshot(ctx).Identity)
	contextualForwardExpectEdge(t, s, producer, consumer)
	if links := contextualForwardEdges(t, s, otherProducer); len(links) != 0 {
		t.Fatalf("a cross-anchored job borrowed the new root read permission: %+v", links)
	}
}

// ── UNSUPPORTED COMMENT/STRING FORMS FAIL CLOSED ─────────────────────────────
//
// A supporting consume construct counts only OUTSIDE comments and strings. A
// JavaScript `//` comment and a Python triple-quoted block (a docstring or a
// printed block) are unsupported forms, so a path inside them names no producer
// and forms no edge. A genuine consuming call beside them still does.
func TestContextualForwardingUnsupportedMentionFormsFailClosed(t *testing.T) {
	mention := []struct {
		name string
		body string
	}{
		{"javascript line comment", "// open(\"PRODUCER\").read()\n"},
		{"python triple-quoted docstring", "x = \"\"\"\nopen(\"PRODUCER\")\n\"\"\"\n"},
		{"printed triple-quoted block", "print(\"\"\"open(\"PRODUCER\")\"\"\")\n"},
	}
	for _, tc := range mention {
		t.Run(tc.name, func(t *testing.T) {
			root, s, producer, consumer := contextualForwardingFixture(t)
			body := strings.ReplaceAll(tc.body, "PRODUCER", producer)
			if err := os.WriteFile(consumer, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			contextualForwardBoundaryRead(t, root, consumer, "root-read")
			contextualForwardPostTurn(t, root, "inspect the report utility")
			contextualForwardExpectNone(t, s, producer)
		})
	}
	// THE GENUINE CONSUME BESIDE THE UNSUPPORTED FORM STILL FORMS THE EDGE.
	t.Run("genuine consume still forms the edge", func(t *testing.T) {
		root, s, producer, consumer := contextualForwardingFixture(t)
		body := "// unrelated note about " + producer + "\ndata = open(\"" + producer + "\").read()\n"
		if err := os.WriteFile(consumer, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		contextualForwardBoundaryRead(t, root, consumer, "root-read")
		contextualForwardPostTurn(t, root, "inspect the report utility")
		contextualForwardExpectEdge(t, s, producer, consumer)
	})
}
