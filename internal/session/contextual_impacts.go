package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	written := 0
	owners := map[string]string{}
	for _, r := range source.Receipts {
		if r.Path != "" {
			if _, known := owners[r.Path]; !known {
				owners[r.Path] = contextualPathOwner(r.Path)
			}
		}
	}
	for _, consumer := range source.Receipts {
		if consumer.Path == "" {
			continue
		}
		for _, producer := range source.Receipts {
			if producer.Path == "" || producer.Path == consumer.Path {
				continue
			}
			relative, err := filepath.Rel(filepath.Dir(consumer.Path), producer.Path)
			if err != nil {
				continue
			}
			// Full resolved paths are identity evidence; shared names are never enough.
			if !contextualResolvedReference(consumer.Path, consumer.Body, producer.Path, relative) {
				continue
			}
			producerOwner, consumerOwner := owners[producer.Path], owners[consumer.Path]
			if producerOwner == "" || consumerOwner == "" || producerOwner == consumerOwner {
				continue
			}
			// Work can change the consumer after its first read. Recheck its exact
			// reference after the turn, so an unrelated formatting edit does not
			// leave the link attached to an obsolete whole-file snapshot.
			consumerHash, consumerReceipt := consumer.Hash, consumer.ID
			current, readErr := contextualReadFile(consumer.Path)
			if readErr != nil || !contextualResolvedReference(consumer.Path, current, producer.Path, relative) {
				continue
			}
			if hash := contextualHash(current); hash != consumer.Hash {
				consumerHash = hash
				consumerReceipt = "contextual-recheck:" + hash
			}
			d := store.ContextualDependencyObservation{ID: "", ProducerOwner: producerOwner, ConsumerOwner: consumerOwner, EntityID: producer.Path, ProducerPath: producer.Path, ConsumerPath: consumer.Path, ProducerHash: producer.Hash, ConsumerHash: consumerHash, ReceiptIDs: []string{producer.ID, consumerReceipt}, Assumption: "Consumer source references the exact producer path " + relative}
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
