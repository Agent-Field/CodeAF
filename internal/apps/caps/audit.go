package caps

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type auditLog struct {
	mu   sync.Mutex
	root string
}

// append keeps credentials, record values and prompts out of the audit trail.
func (a *auditLog) append(scope svc.Scope, method string, err error) {
	line := struct {
		At       string `json:"at"`
		App      string `json:"app,omitempty"`
		Member   string `json:"member"`
		Method   string `json:"method"`
		Decision string `json:"decision"`
	}{time.Now().UTC().Format(time.RFC3339Nano), scope.App, scope.Caller.Member, method, string(errorCode(err))}
	data, _ := json.Marshal(line)
	if a.root == "" {
		slog.Info("apps call", "audit", string(data))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.write(append(data, '\n')); err != nil {
		slog.Error("apps audit append failed", "error", err, "audit", string(data))
	}
}

func (a *auditLog) write(data []byte) error {
	if err := os.MkdirAll(a.root, 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(a.root, "audit.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
