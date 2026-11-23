package agentfield

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/redact"
)

// ── ONE INSTANCE PER FACTORY HOME, ACROSS EVERY RESTART ─────────────────────
//
// The control plane reads a registration whose instance_id differs from the
// one it holds as "the previous process is gone": after its drain window it
// fails every execution still open under the old instance with
// "agent_restart_orphaned", because an SDK agent's in-memory work died with
// that process. A factory's work does not: the job is on the floor, the
// promise to report on it is in the side table (executions.go), and both
// outlive the engine. So the engine going down and coming back — a crash, a
// sleep, the app quitting and relaunching it, a late key restarting the node
// — MUST NOT look like a new process to AgentField. A caller's execution
// stays open and later gets the job's real result.
//
// THE INSTANCE IS THEREFORE THE FLOOR'S, NOT THE PROCESS'S: made once, kept
// beside the floor in agentfield-instance.json, and registered by every start
// over that floor. A different floor (another machine, another home) is a
// different instance, which is still exactly what the reaper is for.

const instanceFile = "agentfield-instance.json"

type instanceDoc struct {
	InstanceID string    `json:"instance_id"`
	Since      time.Time `json:"since"`
}

// instanceFor is the instance this floor registers as. A kept one is
// answered as it is. With none kept, the first start makes one — EXCEPT that
// a floor that already has open executions was running under an instance it
// did not keep (a build from before this file), and the control plane holds
// those executions under it: that instance is adopted from the control
// plane's node record, so the first start after the upgrade does not orphan
// them either. A control plane that cannot say leaves a fresh one.
//
// An instance that cannot be kept is still registered: this start behaves as
// every start once did, and the next start tries again.
func (n *Node) instanceFor(ctx context.Context, c *Client, node string) (string, error) {
	path := filepath.Join(n.Store.Root(), instanceFile)
	var d instanceDoc
	if readJSON(path, &d) == nil && strings.TrimSpace(d.InstanceID) != "" {
		return d.InstanceID, nil
	}
	id := ""
	if len(n.table().open()) > 0 {
		ask, cancel := context.WithTimeout(ctx, 5*time.Second)
		id = c.NodeInstance(ask, node)
		cancel()
	}
	if id == "" {
		var err error
		if id, err = newInstance(); err != nil {
			return "", err
		}
	}
	if err := writePrivate(path, instanceDoc{InstanceID: id, Since: n.now().UTC()}); err != nil {
		log.Printf("agentfield: the instance %s could not be kept beside the floor: %s", id, redact.Secrets(err.Error()))
	}
	return id, nil
}

// NodeInstance is the instance_id the control plane holds for node, or empty
// when it holds none or cannot say.
func (c *Client) NodeInstance(ctx context.Context, node string) string {
	var out struct {
		InstanceID string `json:"instance_id"`
	}
	if strings.TrimSpace(node) == "" || c.do(ctx, "read the node", http.MethodGet, "/api/v1/nodes/"+url.PathEscape(node), nil, nil, &out) != nil {
		return ""
	}
	return strings.TrimSpace(out.InstanceID)
}
