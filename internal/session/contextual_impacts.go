package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

const contextualFileBytes = 64 << 10

// contextualReadReceipt accepts only a successful full read the tool actually
// made. Partial output, refused reads and arbitrary shell programs prove no link.
func (a *Agent) contextualReadReceipt(call ai.ToolCall, result toolResult, r memoryToolReceipt) memoryToolReceipt {
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	root, err := exec.CommandContext(ctx, "git", "-C", directory, "rev-parse", "--show-toplevel").Output()
	if err == nil {
		directory = strings.TrimSpace(string(root))
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
			d := store.ContextualDependencyObservation{ID: store.NewMemoryID(), ProducerOwner: producerOwner, ConsumerOwner: consumerOwner, EntityID: producer.Path, ProducerPath: producer.Path, ConsumerPath: consumer.Path, ProducerHash: producer.Hash, ConsumerHash: consumer.Hash, ReceiptIDs: []string{producer.ID, consumer.ID}, Assumption: "Consumer source references the exact producer path " + relative}
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
	lower := strings.ToLower(cue)
	relevant := false
	for _, word := range []string{"contract", "format", "schema", "export", "return", "decimal", "change", "release", "api"} {
		if strings.Contains(lower, word) {
			relevant = true
			break
		}
	}
	if !relevant || a.config.MemoryProjectKey == "" {
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
			if !said && len(a.memory.impactNotices) >= contextualContextLimit {
				said = true
			}
			if !said {
				a.memory.impactNotices[notice.EvidenceHash] = notice
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
	return "\n<contextual_impacts>\nMention only a useful supported consequence for the current work; batch related consequences. File change alone does not prove breakage.\n" + strings.Join(lines, "\n") + "\n</contextual_impacts>\n"
}

func (a *Agent) dismissContextualNotices(user string) {
	lower := strings.ToLower(user)
	if !strings.Contains(lower, "dismiss") && !strings.Contains(lower, "don't bring that up") && !strings.Contains(lower, "do not bring that up") {
		return
	}
	a.memory.mu.Lock()
	notices := make([]store.ContextualImpactNotice, 0, len(a.memory.impactNotices))
	for _, n := range a.memory.impactNotices {
		notices = append(notices, n)
	}
	a.memory.mu.Unlock()
	for _, n := range notices {
		if err := a.memory.store.DismissContextualImpact([]string{n.Dependency.ProducerOwner, n.Dependency.ConsumerOwner}, n); err != nil {
			a.journalMemoryFailure("impact-dismissal", err)
		}
	}
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
