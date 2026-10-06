package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

const contextualFileBytes = 64 << 10

// contextualReadReceipt accepts only a successful full read the tool actually
// made. Partial output, refused reads and arbitrary shell programs prove no link.
func (a *Agent) contextualReadReceipt(ctx context.Context, call ai.ToolCall, result toolResult, r memoryToolReceipt) memoryToolReceipt {
	if result.isError || result.harness {
		return r
	}
	var path string
	if call.Function.Name == "read" {
		var args struct {
			Path   string `json:"path"`
			Offset *int   `json:"offset"`
			Limit  *int   `json:"limit"`
		}
		if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args.Offset != nil || args.Limit != nil {
			return r
		}
		path = args.Path
	} else if call.Function.Name == "bash" {
		var args struct {
			Command string `json:"command"`
			Cmd     string `json:"cmd"`
		}
		if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
			return r
		}
		command := args.Command
		if command == "" {
			command = args.Cmd
		}
		words := strings.Fields(command)
		if len(words) != 2 || words[0] != "cat" || strings.ContainsAny(words[1], ";&|><$`*?\n") {
			return r
		}
		path = strings.Trim(words[1], "\"'")
	} else {
		return r
	}
	if path == "" {
		return r
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.config.Workspace, path)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return r
	}
	body, err := contextualReadFile(canonical)
	if err != nil || body == "" || !strings.Contains(result.text, body) {
		return r
	}
	r.Path = canonical
	r.Body = body
	r.Hash = contextualHash(body)
	// The receipt's OWN snapshot: the evidence row is earned under the state
	// the read saw, not the turn's arbitrary start-of-turn identity.
	r.Snapshot = a.captureSourceSnapshot(ctx).Identity
	return r
}

// contextualDropping answers whether a canonical path is one of THIS agent's own
// session droppings — a stub or job log the session wrote for itself — which is
// never independent source evidence about anybody's project. Both layouts are
// covered: the session folder's logs/ and, for a caller with no Place, the
// workspace's legacy .codeaf/. A condenser's stub is a COPY of a real file, so
// byte identity cannot tell them apart; the folder is what refuses it.
func (a *Agent) contextualDropping(path string) bool {
	roots := []string{}
	if logs := a.config.droppingsPlace().Logs(); logs != "" {
		roots = append(roots, logs)
	}
	if workspace := strings.TrimSpace(a.config.Workspace); workspace != "" {
		roots = append(roots, filepath.Join(workspace, flatDroppingsDir))
	}
	for _, root := range roots {
		if canonical := canonicalPath(root); canonical != "" && contextualPathWithin(path, canonical) {
			return true
		}
	}
	return false
}

// contextualPathWithin answers whether path is root itself or a descendant.
func contextualPathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// contextualForwardedReads keeps only the receipts that carry a trusted full
// read — the canonical Path, the content Hash and the Body the tool returned.
// It is the ONE filter a caller uses to forward receipts from an original
// execution boundary into a frozen origin BEFORE a condenser replaces the
// result with a stub. Worker prose, stub text and arbitrary successful shell
// output carry no Path/Hash/Body and are dropped. The helper is pure; the seam
// that calls it decides when a worker is created.
func contextualForwardedReads(rows []memoryToolReceipt) []memoryToolReceipt {
	out := make([]memoryToolReceipt, 0, len(rows))
	for _, r := range rows {
		if r.Path == "" || r.Hash == "" || r.Body == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// contextualTurnReceipts makes the delegated and the root paths ONE evidence set
// for the frozen turn they share. A worker forwards its own full reads under the
// origin turn captured at its creation; the root session's OWN receipts for that
// same turn — a `read` it actually made at its execution boundary — are still
// live while the delegated call runs, so they are merged in BEFORE the condenser
// replaces that result with a stub. Only receipts that are a trusted full read
// are carried across, so an unrelated observation cannot ride the turn either.
func (a *Agent) contextualTurnReceipts(source memoryTurnEvidence) []memoryToolReceipt {
	rows := append([]memoryToolReceipt(nil), source.Receipts...)
	seq, ok := contextualTurnSeq(source.Turn)
	if !ok {
		return rows
	}
	a.memory.mu.Lock()
	own := append([]memoryToolReceipt(nil), a.memory.receipts[seq]...)
	a.memory.mu.Unlock()
	if len(own) == 0 {
		return rows
	}
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		seen[r.ID] = true
	}
	for _, r := range contextualForwardedReads(own) {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		rows = append(rows, r)
	}
	return rows
}

// contextualTurnSeq reads the turn number from either spelling of a turn id:
// the root's own "seq:hash" and the frozen origin's "session:seq:hash".
func contextualTurnSeq(turn string) (uint64, bool) {
	parts := strings.Split(strings.TrimSpace(turn), ":")
	if len(parts) == 3 {
		parts = parts[1:]
	}
	if len(parts) != 2 {
		return 0, false
	}
	n, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// contextualPathLiteral is a quoted path-like literal: it must carry a directory
// separator, so an ordinary quoted word ("export.py", "amount") is never read as
// a path. The literal is only ever a CANDIDATE; it must still resolve to a real
// regular file before it means anything.
var contextualPathLiteral = regexp.MustCompile(`["']([^"'\n]*[/\\][^"'\n]*)["']`)

// contextualReferencedProducers resolves the producer paths a consumer's source
// NAMES. It reads the reference from the source text and demands that the exact
// resolved path is a real regular file on disk; a name, a prefix or a
// same-basename match proves nothing. Only the literal pathlib chain and quoted
// path literals are read; module discovery is left to an actual read.
func contextualReferencedProducers(consumerPath, body string) []string {
	if strings.TrimSpace(consumerPath) == "" || strings.TrimSpace(body) == "" {
		return nil
	}
	directory := filepath.Dir(consumerPath)
	found := map[string]bool{}
	var out []string
	add := func(candidate string) {
		if strings.TrimSpace(candidate) == "" {
			return
		}
		path := candidate
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil || canonical == consumerPath {
			return
		}
		info, err := os.Stat(canonical)
		if err != nil || !info.Mode().IsRegular() {
			return
		}
		if !found[canonical] {
			found[canonical] = true
			out = append(out, canonical)
		}
	}
	for _, literal := range contextualPathLiteral.FindAllStringSubmatch(body, contextualContextLimit) {
		add(literal[1])
	}
	for _, chain := range contextualPathChain.FindAllStringSubmatch(body, contextualContextLimit) {
		resolved := consumerPath
		for i := 0; i < strings.Count(chain[1], ".parent"); i++ {
			resolved = filepath.Dir(resolved)
		}
		for _, part := range contextualPathPart.FindAllStringSubmatch(chain[2], contextualContextLimit) {
			resolved = filepath.Join(resolved, part[1])
		}
		add(resolved)
	}
	return out
}

// contextualNamesProducer answers whether the current consumer source still
// names the exact producer path, for the post-turn recheck.
func contextualNamesProducer(consumerPath, body, producer string) bool {
	for _, candidate := range contextualReferencedProducers(consumerPath, body) {
		if candidate == producer {
			return true
		}
	}
	return false
}

func contextualReadFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, contextualFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > contextualFileBytes {
		return "", fmt.Errorf("source exceeds contextual read bound")
	}
	return string(body), nil
}

func contextualPathOwner(path string) string {
	directory := filepath.Dir(path)
	// A previously read file grants an observed link to its actual repository,
	// not to every workspace sharing one basename or a vocabulary word.
	if root, ok := repositoryRoot(directory); ok {
		directory = root
	}
	key, err := gitidentity.ProjectKey(directory)
	if err != nil || key == "" {
		return ""
	}
	return store.OwnerProject(key)
}

func (a *Agent) observeContextualDependencies(source memoryTurnEvidence) {
	if !a.remembers() {
		return
	}
	// THE ROOT'S OWN BOUNDARY READS JOIN THE ORIGIN'S. A turn handed to a quick
	// task still made its earlier reads itself, at the real executeTool
	// boundary; those receipts are merged in here, under the frozen turn both
	// sides share, before the condenser's stub can stand in for them.
	source.Receipts = a.contextualTurnReceipts(source)
	reads := contextualForwardedReads(source.Receipts)
	byPath := map[string]memoryToolReceipt{}
	for _, r := range reads {
		if _, known := byPath[r.Path]; !known {
			byPath[r.Path] = r
		}
	}
	written := 0
	for _, consumer := range reads {
		consumerOwner := contextualPathOwner(consumer.Path)
		if consumerOwner == "" {
			continue
		}
		if a.contextualDropping(consumer.Path) {
			continue
		}
		for _, producer := range contextualReferencedProducers(consumer.Path, consumer.Body) {
			if producer == consumer.Path || a.contextualDropping(producer) {
				continue
			}
			producerOwner := contextualPathOwner(producer)
			if producerOwner == "" || producerOwner == consumerOwner {
				continue
			}
			// THE PRODUCER SIDE IS EITHER A REAL TOOL READ OR A FRAMEWORK
			// RECHECK, AND THE RECORD SAYS WHICH. When the agent actually read
			// the producer, its own receipt (and hash) is used. When it did not
			// — the ordinary case where the consumer's source simply names the
			// producer and the agent only read the consumer — the framework
			// reads the exact named file under the same bound a tool read uses
			// and records THAT, labelled `framework-verify:`, so a reader can
			// never mistake it for a `cat` the agent ran. No shell exit code and
			// no substring of some command's output is ever proof here.
			var producerHash, producerReceipt string
			if held, ok := byPath[producer]; ok {
				producerHash, producerReceipt = held.Hash, held.ID
			} else {
				body, err := contextualReadFile(producer)
				if err != nil || body == "" {
					continue
				}
				producerHash = contextualHash(body)
				producerReceipt = "framework-verify:" + producerHash
			}
			// Work can change the consumer after its first read. Recheck its
			// exact reference after the turn, so an unrelated formatting edit
			// does not leave the link attached to an obsolete whole-file
			// snapshot.
			consumerHash, consumerReceipt := consumer.Hash, consumer.ID
			current, readErr := contextualReadFile(consumer.Path)
			if readErr != nil || !contextualNamesProducer(consumer.Path, current, producer) {
				continue
			}
			if hash := contextualHash(current); hash != consumer.Hash {
				consumerHash = hash
				consumerReceipt = "contextual-recheck:" + hash
			}
			relative, err := filepath.Rel(filepath.Dir(consumer.Path), producer)
			if err != nil {
				continue
			}
			d := store.ContextualDependencyObservation{ID: "", ProducerOwner: producerOwner, ConsumerOwner: consumerOwner, EntityID: producer, ProducerPath: producer, ConsumerPath: consumer.Path, ProducerHash: producerHash, ConsumerHash: consumerHash, ReceiptIDs: []string{consumerReceipt, producerReceipt}, Assumption: "Consumer source names the exact producer path " + relative}
			if _, err := a.memory.store.ObserveContextualDependency([]string{producerOwner, consumerOwner}, d); err != nil {
				a.journalMemoryFailure("dependency", err)
			}
			written++
			if written >= contextualContextLimit {
				return
			}
		}
	}
}

// contextualImpactContext re-reads the consumer assumption before suggesting a
// consequence. It never schedules work or edits a consumer in another project.
func (a *Agent) contextualImpactContext(cue string) string {
	if !a.remembers() {
		return ""
	}
	a.mu.Lock()
	turn := a.turnSeq
	a.mu.Unlock()
	a.memory.mu.Lock()
	if a.memory.impactTurn == turn && a.memory.impactPrepared {
		block := a.memory.impactBlock
		a.memory.mu.Unlock()
		return block
	}
	a.memory.impactTurn = turn
	a.memory.impactPrepared = true
	a.memory.impactCue = cue
	a.memory.impactBlock = ""
	a.memory.mu.Unlock()
	// THE EVIDENCE DECIDES, NOT A KEYWORD LIST. A hardcoded vocabulary was the
	// old gate, and a person asking to "make the amount optional" — words none
	// of it contained — would not hear about a consequence their own edit just
	// produced. What runs now is the actual observed state: an edge whose
	// producer source really changed and whose consumer assumption still holds.
	// A turn that changed nothing reads no different files and says nothing, so
	// ordinary and irrelevant requests stay quiet without a word list.
	if a.config.MemoryProjectKey == "" {
		return ""
	}
	owner := store.OwnerProject(a.config.MemoryProjectKey)
	links, err := a.memory.store.DependenciesForProducer(owner, contextualContextLimit)
	if err != nil {
		return ""
	}
	var lines []string
	for _, d := range links {
		producer, err := contextualReadFile(d.ProducerPath)
		if err != nil {
			continue
		}
		hash := contextualHash(producer)
		if hash == d.ProducerHash {
			continue
		}
		consumer, err := contextualReadFile(d.ConsumerPath)
		if err != nil {
			continue
		}
		consumerHash := contextualHash(consumer)
		notices, err := a.memory.store.ContextualImpacts([]string{d.ProducerOwner, d.ConsumerOwner}, owner, d.EntityID, hash, map[string]string{d.ConsumerPath: consumerHash})
		if err != nil {
			continue
		}
		for _, notice := range notices {
			a.memory.mu.Lock()
			if a.memory.impactNotices == nil {
				a.memory.impactNotices = map[string]store.ContextualImpactNotice{}
			}
			_, said := a.memory.impactNotices[notice.EvidenceHash]
			if !said {
				// THE HELD SET IS BOUNDED AND EVICTABLE. A session that has
				// already offered its eight notices must still be able to offer
				// genuinely new material: the oldest offer is dropped rather
				// than permanently blocking the ninth. Notice identity is the
				// content hash, so the same change never repeats; a later change
				// is a different notice and can surface.
				for len(a.memory.impactOrder) >= contextualContextLimit {
					oldest := a.memory.impactOrder[0]
					a.memory.impactOrder = a.memory.impactOrder[1:]
					delete(a.memory.impactNotices, oldest)
				}
				a.memory.impactNotices[notice.EvidenceHash] = notice
				a.memory.impactOrder = append(a.memory.impactOrder, notice.EvidenceHash)
			}
			a.memory.mu.Unlock()
			if said {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s changed since %s was observed consuming it. Consumer source was re-read and its recorded assumption is unchanged: %s. Inspect that assumption before asserting a break; offer the relevant follow-up, without editing another project. notice=%s", d.ProducerPath, d.ConsumerPath, d.Assumption, notice.EvidenceHash))
		}
		if len(lines) >= contextualContextLimit {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	block := "\n<contextual_impacts>\nMention only a useful supported consequence for the current work; batch related consequences. File change alone does not prove breakage.\n" + strings.Join(lines, "\n") + "\n</contextual_impacts>\n"
	a.memory.mu.Lock()
	a.memory.impactBlock = block
	a.memory.mu.Unlock()
	return block
}

func (a *Agent) dismissContextualNotices(user string) {
	if !a.remembers() {
		return
	}
	lower := strings.ToLower(user)
	// A NEGATED DISMISSAL IS NOT A DISMISSAL. "do not dismiss that" and its
	// friends carry the dismiss word and mean the opposite; reading them as the
	// instruction would throw away the very notice the person asked to keep.
	if !contextualDismissCue(lower) || contextualDismissNegated(lower) {
		return
	}
	a.memory.mu.Lock()
	notices := make([]store.ContextualImpactNotice, 0, len(a.memory.impactNotices))
	for _, n := range a.memory.impactNotices {
		notices = append(notices, n)
	}
	a.memory.mu.Unlock()
	if len(notices) == 0 {
		return
	}
	// DISMISS WHAT THE PERSON NAMED, OR AN UNAMBIGUOUS SINGLETON/BATCH. A bare
	// "dismiss" with several notices held names nothing, so it disappears
	// nothing rather than silencing the whole history on a substring.
	targets := make([]store.ContextualImpactNotice, 0, len(notices))
	for _, n := range notices {
		if contextualNoticeNamed(lower, n) {
			targets = append(targets, n)
		}
	}
	if len(targets) == 0 && (len(notices) == 1 || contextualDismissBatch(lower)) {
		targets = notices
	}
	if len(targets) == 0 {
		return
	}
	for _, n := range targets {
		if err := a.memory.store.DismissContextualImpact([]string{n.Dependency.ProducerOwner, n.Dependency.ConsumerOwner}, n); err != nil {
			a.journalMemoryFailure("impact-dismissal", err)
		}
	}
	a.memory.mu.Lock()
	for _, n := range targets {
		delete(a.memory.impactNotices, n.EvidenceHash)
	}
	kept := a.memory.impactOrder[:0]
	for _, hash := range a.memory.impactOrder {
		if _, held := a.memory.impactNotices[hash]; held {
			kept = append(kept, hash)
		}
	}
	a.memory.impactOrder = kept
	a.memory.mu.Unlock()
}

// contextualDismissCue answers whether the person is asking for a notice to be
// dropped at all.
func contextualDismissCue(lower string) bool {
	for _, cue := range []string{"dismiss", "don't bring that up", "do not bring that up", "don't bring it up", "do not bring it up", "stop bringing that up"} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// contextualDismissNegated answers whether that request is negated. It is
// checked before any notice is touched.
func contextualDismissNegated(lower string) bool {
	for _, neg := range []string{"don't dismiss", "do not dismiss", "not dismiss", "never dismiss", "don't drop", "do not drop"} {
		if strings.Contains(lower, neg) {
			return true
		}
	}
	return false
}

// contextualNoticeNamed answers whether the person named this notice's producer
// or consumer, by path or by file name.
func contextualNoticeNamed(lower string, n store.ContextualImpactNotice) bool {
	for _, path := range []string{n.Dependency.ProducerPath, n.Dependency.ConsumerPath} {
		path = strings.ToLower(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		if strings.Contains(lower, path) || strings.Contains(lower, strings.ToLower(filepath.Base(path))) {
			return true
		}
	}
	return false
}

// contextualDismissBatch answers whether an explicit plural or batch reference
// makes dismissing every held notice unambiguous.
func contextualDismissBatch(lower string) bool {
	for _, word := range []string{"those", "them", "both", "all", "batch", "notices"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// The small static resolver accepts direct paths and a literal pathlib chain.
// Dynamic module discovery is left to a later observed receipt, never guessed
// from matching names. Every accepted chain resolves to the complete producer.
var contextualPathChain = regexp.MustCompile(`(?:pathlib\.)?Path\(__file__\)\.resolve\(\)((?:\.parent)+)((?:\s*/\s*["'][^"'\n]+["'])+)`)
var contextualPathPart = regexp.MustCompile(`["']([^"']+)["']`)

func contextualResolvedReference(consumerPath, body, producerPath, relative string) bool {
	if strings.Contains(body, "\""+producerPath+"\"") || strings.Contains(body, "'"+producerPath+"'") {
		return true
	}
	if strings.HasPrefix(relative, ".."+string(filepath.Separator)) && (strings.Contains(body, "\""+relative+"\"") || strings.Contains(body, "'"+relative+"'")) {
		return true
	}
	for _, chain := range contextualPathChain.FindAllStringSubmatch(body, contextualContextLimit) {
		resolved := consumerPath
		for i := 0; i < strings.Count(chain[1], ".parent"); i++ {
			resolved = filepath.Dir(resolved)
		}
		for _, part := range contextualPathPart.FindAllStringSubmatch(chain[2], contextualContextLimit) {
			resolved = filepath.Join(resolved, part[1])
		}
		canonical, err := filepath.EvalSymlinks(resolved)
		if err == nil && canonical == producerPath {
			return true
		}
	}
	return false
}

// A successful action may change a known producer during this very turn. The
// next request receives the consequence before its reply, not a turn later.
func (a *Agent) refreshContextualImpactsAfterAction(result toolResult, tool string) {
	if !a.remembers() || result.isError || result.harness || tool == "read" {
		return
	}
	a.memory.mu.Lock()
	cue := a.memory.impactCue
	a.memory.impactPrepared = false
	a.memory.mu.Unlock()
	block := a.contextualImpactContext(cue)
	if block == "" {
		return
	}
	a.mu.Lock()
	if !strings.Contains(a.memoryText, block) {
		a.memoryText = contextualClip(a.memoryText+block, memoryBlockRunes)
		a.landVolatileLocked()
	}
	a.mu.Unlock()
}
