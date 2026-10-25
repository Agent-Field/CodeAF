package clientrun

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"golang.org/x/crypto/nacl/box"
)

// Envelope is the transport-independent snapshot format. Agent is always
// present for chat reads, including empty lists which clear prior snapshots.
type Envelope struct {
	Writer string    `json:"writer"`
	At     time.Time `json:"at"`
	Agent  string    `json:"agent,omitempty"`
	Crew   string    `json:"crew,omitempty"`
	Result any       `json:"result"`
}

// SlotName keeps each writer's snapshot separate so boxes cannot overwrite
// one another's chats. Grants tell clients which writer names to read.
func SlotName(method, writer string) string { return method + "." + writer }

func (r *Runner) publish(ctx context.Context, c Client, l directory.Listing) error {
	if len(c.BoxKey) != 32 {
		return errors.New("client encryption key is malformed")
	}
	var errs []error
	for _, method := range c.Reads {
		if err := r.publishMethod(ctx, c, l, method); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *Runner) publishMethod(ctx context.Context, c Client, l directory.Listing, method string) error {
	held := r.held(l)

	results, err := r.snapshots(method, held)
	if err != nil {
		return err
	}
	at := r.cfg.Now()
	if method == "agents.entries" {
		if err := trimEntries(results, r.cfg.Signer.Cert().DeviceID(), at); err != nil {
			return err
		}
	}
	content, err := json.Marshal(results)
	if err != nil {
		return err
	}
	current, err := r.cfg.Directory.List(ctx)
	if err != nil {
		return err
	}
	if !slices.Equal(held, r.held(current)) {
		return errors.New("chat leases changed during snapshot read")
	}
	key := c.ID + "/" + method
	hash := sha256.Sum256(content)
	previous := r.hashes[key]
	// Refresh before the relay's 24-hour expiry even when data is unchanged.
	if previous.hash == hash && r.cfg.Now().Sub(previous.at) < clientproto.MaxAge/2 {
		return nil
	}
	envelopes := make([]Envelope, 0, len(results))
	for _, result := range results {
		envelopes = append(envelopes, Envelope{Writer: r.cfg.Signer.Cert().DeviceID(), At: at, Agent: result.Agent, Crew: result.Crew, Result: result.Result})
	}
	plain, err := json.Marshal(envelopes)
	if err != nil {
		return err
	}
	pub := (*[32]byte)(c.BoxKey)
	if len(plain)+box.AnonymousOverhead > clientproto.MaxSlotBytes {
		return appwire.Errorf(appwire.CodeTooLarge, "%s snapshot exceeds the %d KiB slot limit", method, clientproto.MaxSlotBytes/1024)
	}
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
	return nil
}

func (r *Runner) held(l directory.Listing) []string {
	var ids []string
	for id, c := range l.Cells {
		if !c.Archived && slices.Contains([]string{"chat", string(cell.HostBound), string(cell.Sandboxed), string(cell.FilesOnly)}, c.Class) && c.Lease.Device == r.cfg.Signer.Cert().DeviceID() && c.Lease.Expires > l.Now {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (r *Runner) snapshots(method string, held []string) ([]Envelope, error) {
	if strings.HasPrefix(method, "crews.") {
		return r.crewSnapshots(method, held)
	}
	if method == "spend.today" {
		result, err := appwire.Call(r.cfg.Engine, method, nil)
		return []Envelope{{Result: result}}, err
	}
	if method == "agents.list" || method == "inbox.list" || method == "tasks.list" {
		return r.globalSnapshots(method, held)
	}
	out := make([]Envelope, 0, len(held))
	for _, id := range held {
		params, _ := json.Marshal(appwire.AgentParams{Agent: id})
		if method == "agents.entries" {
			params, _ = json.Marshal(appwire.EntriesParams{ID: id, Limit: phoneEntryLimit})
		}
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
	// TasksResult has per-agent rows, and tasks.list requires an agent.
	// Read each held chat through that existing interface, then partition just
	// like the global agent and inbox lists; never publish foreign rows.
	if method == "tasks.list" {
		out := make([]Envelope, 0, len(held))
		for _, id := range held {
			params, _ := json.Marshal(appwire.AgentParams{Agent: id})
			result, err := appwire.Call(r.cfg.Engine, method, params)
			if err != nil {
				return nil, err
			}
			tasks := result.(*appwire.TasksResult)
			rows := slices.DeleteFunc(slices.Clone(tasks.Tasks), func(t appwire.Task) bool { return t.Agent != id })
			out = append(out, Envelope{Agent: id, Result: appwire.TasksResult{Tasks: rows}})
		}
		return out, nil
	}
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

// phoneEntryLimit bounds the history a scoped phone receives per chat.
const phoneEntryLimit = 40

// trimEntries budgets the entire sealed slot, including envelope metadata.
// Oldest whole entries leave first; an oversized newest entry leaves an empty
// page with More set so the client never mistakes truncation for no history.
func trimEntries(results []Envelope, writer string, at time.Time) error {
	envelopes := make([]Envelope, len(results))
	for i, row := range results {
		envelopes[i] = Envelope{Writer: writer, At: at, Agent: row.Agent, Result: row.Result}
	}
	plain, err := json.Marshal(envelopes)
	if err != nil {
		return err
	}
	size := len(plain) + box.AnonymousOverhead
	for size > clientproto.MaxSlotBytes {
		oldest := -1
		for i, row := range results {
			page := row.Result.(*appwire.EntriesResult)
			if len(page.Entries) == 0 {
				continue
			}
			if oldest < 0 || page.Entries[0].At.Before(results[oldest].Result.(*appwire.EntriesResult).Entries[0].At) {
				oldest = i
			}
		}
		if oldest < 0 {
			return appwire.Errorf(appwire.CodeTooLarge, "chat envelopes exceed the slot limit")
		}
		page := results[oldest].Result.(*appwire.EntriesResult)
		entry, err := json.Marshal(page.Entries[0])
		if err != nil {
			return err
		}
		size -= len(entry)
		if len(page.Entries) > 1 {
			size--
		}
		page.Entries = page.Entries[1:]
		if !page.More {
			page.More = true
			size--
		}
	}
	return nil
}
