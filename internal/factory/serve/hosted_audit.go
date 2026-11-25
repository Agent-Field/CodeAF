package serve

import (
	"bufio"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const hostedAuditLimit = 200

type hostedAuditEntry struct {
	At      string `json:"at"`
	Person  string `json:"person"`
	Chat    string `json:"chat"`
	Kind    string `json:"kind"`
	Host    string `json:"host"`
	Repo    string `json:"repo"`
	Service string `json:"service"`
	Asker   string `json:"asker,omitempty"`
}

// One organization process owns the file, so the same lock gives readers
// complete lines and prevents concurrent writers interleaving appends.
var hostedAuditMu sync.Mutex

func (h *Handler) hostedAuditPath() string {
	if root := h.eventRoot(); root != "" {
		return filepath.Join(root, "hosted-audit.jsonl")
	}
	return ""
}

func (h *Handler) hostedAudit(person, chat, kind, host, repo, service string) {
	entry := hostedAuditEntry{Person: person, Chat: chat, Kind: kind, Host: host, Repo: repo, Service: service}
	if kind == "git" {
		if asker := h.config.Hosted.state.askerOf(chat); asker != person {
			entry.Asker = asker
		}
	}
	h.recordHostedAudit(entry)
}

func (h *Handler) recordHostedAudit(entry hostedAuditEntry) {
	entry.At = h.clock().UTC().Format(time.RFC3339Nano)
	if err := appendHostedAudit(h.hostedAuditPath(), entry); err != nil {
		log.Print("audit")
	}
}

func appendHostedAudit(path string, entry hostedAuditEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	hostedAuditMu.Lock()
	defer hostedAuditMu.Unlock()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return errors.Join(err, f.Sync(), f.Close())
}

func (h *Handler) readHostedAudit(w http.ResponseWriter, r *http.Request) {
	person, ok := h.hostedPerson(w, r)
	if !ok {
		return
	}
	entries, err := hostedAuditFor(h.hostedAuditPath(), person)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "The server could not read your git activity.")
		return
	}
	h.write(w, http.StatusOK, struct {
		Entries []hostedAuditEntry `json:"entries"`
	}{entries})
}

func hostedAuditFor(path, person string) ([]hostedAuditEntry, error) {
	hostedAuditMu.Lock()
	defer hostedAuditMu.Unlock()
	entries := make([]hostedAuditEntry, 0, hostedAuditLimit)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Reuse the oldest slot once the window is full to bound the memory
	// needed to select one person's latest entries from a shared file.
	next := 0
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		var entry hostedAuditEntry
		if json.Unmarshal(scan.Bytes(), &entry) != nil || entry.Person != person {
			continue
		}
		if len(entries) < hostedAuditLimit {
			entries = append(entries, entry)
			continue
		}
		entries[next] = entry
		next = (next + 1) % hostedAuditLimit
	}
	if next != 0 {
		entries = append(entries[next:], entries[:next]...)
	}
	return entries, scan.Err()
}
