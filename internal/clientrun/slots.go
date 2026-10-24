package clientrun

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"net/url"
	"slices"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/directory"
	"golang.org/x/crypto/nacl/box"
)

// Envelope is the transport-independent snapshot format. Agent is always
// present for chat reads, including empty lists which clear prior snapshots.
type Envelope struct {
	Writer string    `json:"writer"`
	At     time.Time `json:"at"`
	Agent  string    `json:"agent,omitempty"`
	Result any       `json:"result"`
}

// SlotName keeps each writer's snapshot separate so boxes cannot overwrite
// one another's chats. Grants tell clients which writer names to read.
func SlotName(method, writer string) string { return method + "." + writer }

func (r *Runner) publish(ctx context.Context, c Client, l directory.Listing) error {
	held := r.held(l)
	for _, method := range c.Reads {
		results, err := r.snapshots(method, held)
		if err != nil {
			return err
		}
		content, err := json.Marshal(results)
		if err != nil {
			return err
		}
		key := c.ID + "/" + method
		hash := sha256.Sum256(content)
		previous := r.hashes[key]
		// Refresh before the relay's 24-hour expiry even when data is unchanged.
		if previous.hash == hash && r.cfg.Now().Sub(previous.at) < 12*time.Hour {
			continue
		}
		envelopes := make([]Envelope, 0, len(results))
		for _, result := range results {
			envelopes = append(envelopes, Envelope{r.cfg.Signer.Cert().DeviceID(), r.cfg.Now(), result.Agent, result.Result})
		}
		plain, err := json.Marshal(envelopes)
		if err != nil {
			return err
		}
		pub := (*[32]byte)(c.BoxKey)
		sealed, err := box.SealAnonymous(nil, plain, pub, rand.Reader)
		if err != nil {
			return err
		}
		if err := r.request(ctx, "PUT", clientPath(c.ID)+"/slots/"+url.PathEscape(SlotName(method, r.cfg.Signer.Cert().DeviceID())), struct {
			Sealed []byte `json:"sealed"`
		}{sealed}, nil); err != nil {
			return err
		}
		r.hashes[key] = publication{hash, r.cfg.Now()}
	}
	return nil
}

func (r *Runner) held(l directory.Listing) []string {
	var ids []string
	for id, c := range l.Cells {
		if !c.Archived && c.Class == "chat" && c.Lease.Device == r.cfg.Signer.Cert().DeviceID() && c.Lease.Expires > l.Now {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (r *Runner) snapshots(method string, held []string) ([]Envelope, error) {
	if method == "spend.today" {
		result, err := appwire.Call(r.cfg.Engine, method, nil)
		return []Envelope{{Result: result}}, err
	}
	if method == "agents.list" || method == "inbox.list" {
		return r.globalSnapshots(method, held)
	}
	out := make([]Envelope, 0, len(held))
	for _, id := range held {
		params, _ := json.Marshal(appwire.AgentParams{Agent: id})
		result, err := appwire.Call(r.cfg.Engine, method, params)
		if err != nil {
			return nil, err
		}
		out = append(out, Envelope{Agent: id, Result: result})
	}
	return out, nil
}

// globalSnapshots partitions the desktop's global answers by lease, keeping
// their original result shape and discarding chats this box does not own.
func (r *Runner) globalSnapshots(method string, held []string) ([]Envelope, error) {
	result, err := appwire.Call(r.cfg.Engine, method, nil)
	if err != nil {
		return nil, err
	}
	out := make([]Envelope, 0, len(held))
	for _, id := range held {
		switch rows := result.(type) {
		case *appwire.AgentsResult:
			agents := slices.DeleteFunc(slices.Clone(rows.Agents), func(a appwire.Agent) bool { return a.ID != id })
			out = append(out, Envelope{Agent: id, Result: appwire.AgentsResult{Agents: agents}})
		case *appwire.ItemsCountsResult:
			items := slices.DeleteFunc(slices.Clone(rows.Items), func(i appwire.InboxItem) bool { return i.Ask.Agent != id })
			counts := appwire.InboxCounts{Waiting: len(items)}
			out = append(out, Envelope{Agent: id, Result: appwire.ItemsCountsResult{Items: items, Counts: counts}})
		}
	}
	return out, nil
}
