package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// Knowledge is the same project/profile vault the conversation's tools read.
// Capture is opt-in and only persisted, human-facing conversations write it.
func (a *Agent) Knowledge() *knowledge.Vault {
	a.knowledgeMu.RLock()
	defer a.knowledgeMu.RUnlock()
	return a.knowledgeVault
}

// KnowledgeCaptureError reports a failed journal-to-vault copy. The durable
// journal remains the recovery source; a failed copy never advances its cursor.
func (a *Agent) KnowledgeCaptureError() string {
	a.knowledgeMu.RLock()
	j := a.knowledgeJournal
	a.knowledgeMu.RUnlock()
	if j == nil {
		return ""
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastError
}

type knowledgeJournal struct {
	vault     *knowledge.Vault
	project   string
	scope     string
	mu        sync.Mutex
	offset    int64
	skip      int
	lastError string
	cancel    context.CancelFunc
	done      chan struct{}
}

func (a *Agent) initKnowledge() {
	if a.config.knowledgeReadVault != nil {
		a.knowledgeVault = a.config.knowledgeReadVault
		return
	}
	if strings.TrimSpace(a.config.transcriptPath()) == "" || a.config.InTask || a.config.taskID != 0 {
		return
	}
	a.knowledgeVault = knowledge.Open(config.ProfilePath(a.config.ProfileDir, ""), a.config.Workspace)
}
func (a *Agent) startKnowledge() {
	vault := a.Knowledge()
	if vault == nil || a.config.InTask || a.config.taskID != 0 || a.config.knowledgeReadVault != nil || a.file == nil {
		return
	}
	path := a.file.journalName()
	if path == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &knowledgeJournal{cancel: cancel, done: make(chan struct{}), vault: vault, project: a.config.Workspace}
	a.knowledgeMu.Lock()
	a.knowledgeJournal = j
	a.knowledgeMu.Unlock()
	sessionID := a.id
	go func() {
		defer close(j.done)
		lastCompile := time.Now()
		retryDelay := 5 * time.Second
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				a.scanKnowledge(j, path, sessionID)
				return
			case <-ticker.C:
				if err := a.scanKnowledge(j, path, sessionID); err != nil {
					continue
				}
				// Runtime batches across conversations using its durable queue and lock.
				// Credential resolution alone takes a.mu; compilation never does.
				if vault.Enabled() && time.Since(lastCompile) >= retryDelay {
					lastCompile = time.Now()
					snapshot, err := vault.Status()
					if err != nil || (snapshot.Pending == 0 && !snapshot.NeedsUpdate) {
						continue
					}
					if err := vault.Flush(ctx, a.knowledgeKey(), false); err != nil && ctx.Err() == nil {
						log.Printf("knowledge: compilation pending: %v", err)
						retryDelay *= 2
						if retryDelay > 120*time.Second {
							retryDelay = 120 * time.Second
						}
					} else {
						retryDelay = 5 * time.Second
					}
				}
			}
		}
	}()
}

// rebindKnowledgeLocked follows the persisted journal scope marker. Previous
// workers retain their old vault; their final scan stops matching at the marker.
// Caller holds a.mu. Cancellation never joins a network request under that lock.
func (a *Agent) rebindKnowledgeLocked(project string) {
	a.knowledgeMu.Lock()
	old := a.knowledgeJournal
	if old != nil {
		old.cancel()
		a.knowledgeRetired = append(a.knowledgeRetired, old)
	}
	a.knowledgeVault = knowledge.Open(config.ProfilePath(a.config.ProfileDir, ""), project)
	a.knowledgeJournal = nil
	a.knowledgeMu.Unlock()
	a.startKnowledge()
}
func (a *Agent) stopKnowledge() {
	a.knowledgeMu.RLock()
	workers := append([]*knowledgeJournal(nil), a.knowledgeRetired...)
	if a.knowledgeJournal != nil {
		workers = append(workers, a.knowledgeJournal)
	}
	a.knowledgeMu.RUnlock()
	for _, j := range workers {
		j.cancel()
	}
	timer := time.NewTimer(closeGrace)
	defer timer.Stop()
	for _, j := range workers {
		select {
		case <-j.done:
		case <-timer.C:
			log.Printf("knowledge: capture shutdown deadline; journal recovery will retry")
			return
		}
	}
}

func (a *Agent) knowledgeKey() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	service, ok := a.config.Sources.OrDefault(a.config.APIKey, a.config.BaseURL).ByID(modelsource.DefaultID)
	if !ok {
		return ""
	}
	address := service.Address
	if address == "" {
		address = service.Source.Address
	}
	if address != "" {
		parsed, err := url.Parse(address)
		if err != nil || !strings.EqualFold(parsed.Hostname(), "openrouter.ai") {
			return ""
		}
	}
	return service.Key
}

// scanKnowledge reads complete journal lines by byte offset. Re-journaled
// compaction windows are skipped, so stubs and summaries never become new
// claims. IDs survive reopen/retry and do not conflate equal human messages.
func (a *Agent) scanKnowledge(j *knowledgeJournal, path, sessionID string) (err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	defer func() {
		if err != nil {
			j.lastError = err.Error()
			log.Printf("knowledge: capture pending: %v", err)
		} else {
			j.lastError = ""
		}
	}()
	if !j.vault.Enabled() {
		return nil
	}
	since := j.vault.EnabledSince()
	if since.IsZero() {
		return fmt.Errorf("knowledge: missing opt-in timestamp")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	// Sealed cell headers omit cwd. With no anchor, this session's workspace
	// scopes its original messages. After anchoring, the first marker records
	// the previous scope, so a resume never attributes scratch text to a project.
	if j.offset == 0 {
		j.scope = j.project
		probe := bufio.NewReader(file)
		for {
			line, readErr := probe.ReadBytes('\n')
			if readErr != nil {
				break
			}
			var entry sessionEntry
			if json.Unmarshal(line, &entry) != nil {
				continue
			}
			if entry.Type == "knowledge_scope" {
				if entry.KnowledgePreviousProject != "" {
					j.scope = entry.KnowledgePreviousProject
				} else if entry.KnowledgeProject != "" {
					j.scope = entry.KnowledgeProject
				}
				break
			}
		}
	}
	if _, err = file.Seek(j.offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr == io.EOF {
			return nil
		} // partial last line is retried next time
		if readErr != nil {
			return readErr
		}
		next := j.offset + int64(len(line))
		var entry sessionEntry
		if err = json.Unmarshal(line, &entry); err != nil {
			return fmt.Errorf("journal offset %d: %w", j.offset, err)
		}
		if entry.Type == "session" {
			var header sessionHeader
			if err = json.Unmarshal(line, &header); err != nil {
				return err
			}
			if header.Cwd != "" {
				j.scope = header.Cwd
			}
			j.offset = next
			continue
		}
		if entry.Type == "knowledge_scope" {
			j.scope = entry.KnowledgeProject
			j.offset = next
			continue
		}
		if entry.Type == "compaction" {
			j.skip = entry.Window
			j.offset = next
			continue
		}
		if entry.Type != "message" {
			j.offset = next
			continue
		}
		if j.skip > 0 {
			j.skip--
			j.offset = next
			continue
		}
		ts, e := time.Parse(time.RFC3339Nano, entry.Timestamp)
		eligible := j.scope == j.project && e == nil && !ts.Before(since) && !entry.Note && (entry.Role == "user" || entry.Role == "assistant") && strings.TrimSpace(entry.Content) != ""
		// Tool-bearing narration and operational harness prose are not articles.
		if entry.Role == "assistant" && (len(entry.ToolCalls) > 0 || (entry.Presentation != nil && entry.Presentation.Audience != "human")) {
			eligible = false
		}
		if eligible {
			source := knowledge.Source{ID: fmt.Sprintf("%s:journal:%d", sessionID, j.offset), Session: sessionID, Role: entry.Role, TS: entry.Timestamp, Text: entry.Content}
			if err = j.vault.Capture(source); err != nil {
				return err
			}
		}
		j.offset = next
	}
}
