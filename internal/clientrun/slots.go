package clientrun

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"sort"
	"unicode/utf8"

	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"time"
)

// TruncatedEntryMarker prefixes the tail of an oversized single entry so the
// phone never sees an empty transcript when the newest streaming entry exceeds
// the 64 KiB slot limit (contract L5).
const TruncatedEntryMarker = "…"

const (
	// slotSizeMargin reserves thirty-two bytes of headroom for monotonic
	// timestamp digit growth when budgeting a sealed SlotPlain against
	// clientproto.MaxSlotBytes.
	slotSizeMargin = 32
	// phoneEntryLimit bounds each chat's recent transcript snapshot to forty
	// entries so multi-chat snapshots stay within the 64 KiB slot budget without
	// paging.
	phoneEntryLimit = 40
	// minMeasuredAtMillis pads pre-epoch test clock timestamps to thirteen
	// decimal digits when estimating sealed JSON size so production wall-clock
	// timestamps cannot overflow the measured budget.
	minMeasuredAtMillis int64 = 1_700_000_000_000
	// kibibyte formats clientproto.MaxSlotBytes into KiB in slot-limit errors.
	kibibyte = 1024
)

// Envelope is the transport-independent snapshot format inside a slot's
// Payload JSON array. Agent is always present for chat reads, including empty
// lists which clear prior snapshots.
type Envelope struct {
	Writer string    `json:"writer"`
	At     time.Time `json:"at"`
	Agent  string    `json:"agent,omitempty"`
	Crew   string    `json:"crew,omitempty"`
	Result any       `json:"result"`
}

// SlotName keeps each writer's slot separate so computers cannot overwrite one
// another's cards, snapshots, or outcomes.
func SlotName(method, writer string) string { return method + "." + writer }

func (r *Runner) publish(ctx context.Context, c Client, l directory.Listing) error {
	if len(c.BoxKey) != boxKeyBytes {
		return errors.New("client encryption key is malformed")
	}
	wasWatching := r.isWatching(c.ID)
	err := r.publishPass(ctx, c, l)
	// If a slot PUT reply carried Codeaf-Client-Watching: 1 and transitioned the
	// phone into watching during this round, publish any remaining changed slots
	// that were held back earlier in the same pass.
	if !wasWatching && r.isWatching(c.ID) {
		err = errors.Join(err, r.publishPass(ctx, c, l))
	}
	return err
}

func (r *Runner) publishPass(ctx context.Context, c Client, l directory.Listing) error {
	var errs []error
	if err := r.publishCard(ctx, c); err != nil {
		errs = append(errs, err)
	}
	reads := c.Reads
	if len(reads) > clientproto.MaxComputerReads {
		reads = reads[:clientproto.MaxComputerReads]
	}
	for _, method := range reads {
		if err := r.publishMethod(ctx, c, l, method); err != nil {
			errs = append(errs, err)
		}
	}
	if err := r.publishOutcomes(ctx, c); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (r *Runner) getHash(key string) (publication, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.hashes[key]
	return p, ok
}

func (r *Runner) setHash(key string, p publication) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.isTombstonedLocked(stringsCutPrefix(key)) {
		return
	}
	if cancel, ok := r.coalesce[key]; ok {
		delete(r.coalesce, key)
		cancel()
	}
	delete(r.slotBackoffs, key)
	r.hashes[key] = p
}

func (r *Runner) scheduleCoalesce(key string, wait time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lock == nil || r.isTombstonedLocked(stringsCutPrefix(key)) {
		return
	}
	if _, exists := r.coalesce[key]; exists {
		return
	}
	cancel := r.afterFunc()(wait, func() {
		r.mu.Lock()
		delete(r.coalesce, key)
		r.mu.Unlock()
		r.notifyChanged()
	})
	r.coalesce[key] = cancel
}

func (r *Runner) inSlotBackoff(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.slotBackoffs[key]
	if !ok {
		return false
	}
	return r.cfg.Now().Before(b.until)
}

func (r *Runner) recordSlotError(key string, err error) error {
	if err == nil {
		return nil
	}
	r.mu.Lock()
	b := r.slotBackoffs[key]
	if b.delay <= 0 {
		b.delay = initialBackoff
	} else {
		b.delay = min(b.delay*2, maxBackoff)
	}
	wait := b.delay
	b.until = r.cfg.Now().Add(wait)
	r.slotBackoffs[key] = b
	r.mu.Unlock()
	r.scheduleCoalesce(key, wait)
	return err
}

func stringsCutPrefix(key string) string {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i]
		}
	}
	return key
}

func (r *Runner) withinWatchingFloor(clientID, key string) bool {
	if r.inSlotBackoff(key) {
		return true
	}
	previous, hadPrev := r.getHash(key)
	if !hadPrev || r.isWatching(clientID) {
		return false
	}
	return r.cfg.Now().Sub(previous.at) < clientproto.WatchingFloor
}

func (r *Runner) shouldPublishSlot(clientID, key string, hash [32]byte) bool {
	now := r.cfg.Now()
	previous, hadPrev := r.getHash(key)
	// Unchanged payloads are not rewritten before half the relay's 24h expiry.
	if hadPrev && previous.hash == hash && now.Sub(previous.at) < clientproto.MaxAge/2 {
		return false
	}
	// While not watching, any slot is written at most once per WatchingFloor (30s).
	if hadPrev && !r.isWatching(clientID) && now.Sub(previous.at) < clientproto.WatchingFloor {
		return false
	}
	// While watching, any (phone, slot) is written at most once per StreamCoalesce
	// (250ms); the first write after a >= 250ms quiet gap goes at once (contract L1).
	if hadPrev && r.isWatching(clientID) {
		if wait := clientproto.StreamCoalesce - now.Sub(previous.at); wait > 0 {
			r.scheduleCoalesce(key, wait)
			return false
		}
	}
	return true
}

func (r *Runner) publishCard(ctx context.Context, c Client) error {
	if r.cfg.Signer == nil {
		return errors.New("runner has no device signer")
	}
	key := c.ID + "/card"
	if r.withinWatchingFloor(c.ID, key) {
		return nil
	}
	pub, err := r.executorPublicKey()
	if err != nil {
		return r.recordSlotError(key, err)
	}
	self := r.cfg.Signer.Cert().DeviceID()
	name := r.cardName()
	if !clientproto.ValidCardName(name) {
		return r.recordSlotError(key, errors.New("invalid executor card name"))
	}
	cardContent, err := json.Marshal(struct {
		Writer   string `json:"writer"`
		CertSig  string `json:"cert_sig"`
		BoxPub   []byte `json:"box_pub"`
		Name     string `json:"name"`
		PhonePub []byte `json:"phone_pub"`
	}{
		Writer:   self,
		CertSig:  r.cfg.Signer.Cert().Sig,
		BoxPub:   pub[:],
		Name:     name,
		PhonePub: c.BoxKey,
	})
	if err != nil {
		return r.recordSlotError(key, err)
	}
	hash := sha256.Sum256(cardContent)
	if !r.shouldPublishSlot(c.ID, key, hash) {
		return nil
	}
	// THE WRITE IS TIMED FROM WHEN IT WAS DECIDED, NOT FROM WHEN THE RELAY ANSWERED. The 30-second floor
	// and the 250 ms gap count from the moment this runner issued the write, so a slow reply cannot
	// stretch the interval and the interval does not depend on how long the network took.
	issued := r.cfg.Now()
	slotName := SlotName("card", self)
	atMs, err := r.nextAt(self, slotName)
	if err != nil {
		return r.recordSlotError(key, err)
	}
	card := clientproto.Card{
		V:      clientproto.SlotVersion,
		Writer: self,
		Cert:   r.cfg.Signer.Cert(),
		BoxPub: pub[:],
		Name:   name,
		At:     atMs,
	}
	if len(r.cfg.DeviceKey) == ed25519.PrivateKeySize {
		card = card.Sign(r.cfg.DeviceKey)
	} else {
		card.Sig = r.cfg.Signer.Sign(card.SignText())
	}
	sealed, err := clientproto.SealCard(card, (*[boxKeyBytes]byte)(c.BoxKey), rand.Reader)
	if err != nil {
		return r.recordSlotError(key, err)
	}
	if err := r.putSlot(ctx, c.ID, slotName, sealed); err != nil {
		return r.recordSlotError(key, err)
	}
	r.setHash(key, publication{hash: hash, at: issued})
	return nil
}

func (r *Runner) publishOutcomes(ctx context.Context, c Client) error {
	if r.cfg.Signer == nil {
		return errors.New("runner has no device signer")
	}
	key := c.ID + "/outcomes"
	if r.withinWatchingFloor(c.ID, key) {
		return nil
	}
	self := r.cfg.Signer.Cert().DeviceID()
	outcomes := r.Outcomes(c.ID)
	plain, err := json.Marshal(outcomes)
	if err != nil {
		return r.recordSlotError(key, err)
	}
	hash := sha256.Sum256(plain)
	if !r.shouldPublishSlot(c.ID, key, hash) {
		return nil
	}
	issued := r.cfg.Now()
	slotName := SlotName("outcomes", self)
	if err := r.sealAndPutBoxSlot(ctx, c, self, slotName, plain); err != nil {
		return r.recordSlotError(key, err)
	}
	r.setHash(key, publication{hash: hash, at: issued})
	return nil
}

func (r *Runner) publishMethod(ctx context.Context, c Client, l directory.Listing, method string) error {
	if r.cfg.Engine == nil {
		return nil
	}
	key := c.ID + "/" + method
	if r.withinWatchingFloor(c.ID, key) {
		return nil
	}
	held := r.held(l)
	results, err := r.snapshots(method, held)
	if err != nil {
		return r.recordSlotError(key, err)
	}
	now := r.cfg.Now()
	self := r.cfg.Signer.Cert().DeviceID()
	slotName := SlotName(method, self)
	if method == "agents.entries" {
		if err := trimEntries(results, self, now, slotName, c.ID, c.Ver); err != nil {
			return r.recordSlotError(key, err)
		}
	}
	content, err := json.Marshal(results)
	if err != nil {
		return r.recordSlotError(key, err)
	}
	hash := sha256.Sum256(content)
	if !r.shouldPublishSlot(c.ID, key, hash) {
		return nil
	}
	issued := r.cfg.Now()
	envelopes := make([]Envelope, 0, len(results))
	for _, result := range results {
		envelopes = append(envelopes, Envelope{
			Writer: self,
			At:     now,
			Agent:  result.Agent,
			Crew:   result.Crew,
			Result: result.Result,
		})
	}
	plain, err := json.Marshal(envelopes)
	if err != nil {
		return r.recordSlotError(key, err)
	}
	if sealedSlotSize(slotName, c.ID, self, now.UnixMilli(), c.Ver, plain) > clientproto.MaxSlotBytes {
		return r.recordSlotError(key, appwire.Errorf(appwire.CodeTooLarge, "%s snapshot exceeds the %d KiB slot limit", method, clientproto.MaxSlotBytes/kibibyte))
	}
	if err := r.sealAndPutBoxSlot(ctx, c, self, slotName, plain); err != nil {
		return r.recordSlotError(key, err)
	}
	r.setHash(key, publication{hash: hash, at: issued})
	return nil
}

func (r *Runner) sealAndPutBoxSlot(ctx context.Context, c Client, self, slotName string, payload []byte) error {
	atMs, err := r.nextAt(self, slotName)
	if err != nil {
		return err
	}
	nonce, err := clientproto.NewNonce()
	if err != nil {
		return err
	}
	secret := r.secretKey()
	p := clientproto.SlotPlain{
		V:        clientproto.SlotVersion,
		Slot:     slotName,
		Client:   c.ID,
		Executor: self,
		At:       atMs,
		Ver:      c.Ver,
		Payload:  payload,
	}
	sealed, err := clientproto.SealSlot(p, nonce, (*[boxKeyBytes]byte)(c.BoxKey), &secret)
	if err != nil {
		return err
	}
	if len(sealed) > clientproto.MaxSlotBytes {
		return appwire.Errorf(appwire.CodeTooLarge, "%s snapshot exceeds the %d KiB slot limit", slotName, clientproto.MaxSlotBytes/kibibyte)
	}
	return r.putSlot(ctx, c.ID, slotName, sealed)
}

func (r *Runner) putSlot(ctx context.Context, clientID, slotName string, sealed []byte) error {
	hdr, err := r.requestHeader(ctx, "PUT", clientPath(clientID)+"/slots/"+url.PathEscape(slotName), struct {
		Sealed []byte `json:"sealed"`
	}{sealed}, nil)
	if err != nil {
		return err
	}
	if hdr.Get(clientproto.WatchingHeader) == "1" {
		r.markWatchingFromHeader(clientID)
	}
	return nil
}

func (r *Runner) held(l directory.Listing) []string {
	var ids []string
	self := r.cfg.Signer.Cert().DeviceID()
	for id, c := range l.Cells {
		if !c.Archived && slices.Contains([]string{"chat", string(cell.HostBound), string(cell.Sandboxed), string(cell.FilesOnly)}, c.Class) && c.Lease.Device == self && c.Lease.Expires > l.Now {
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
		if page, ok := result.(*appwire.EntriesResult); ok {
			cloned := *page
			cloned.Entries = slices.Clone(page.Entries)
			if method == "agents.entries" && r.stream != nil {
				r.mergeStreamPartial(id, &cloned)
			}
			result = &cloned
		}
		out = append(out, Envelope{Agent: id, Result: result})
	}
	return out, nil
}

func (r *Runner) mergeStreamPartial(agent string, page *appwire.EntriesResult) {
	partial := r.stream.Entry(agent)
	if partial.Text == "" {
		return
	}
	for _, row := range page.Entries {
		if row.Turn == partial.Turn && row.Msg != nil && partial.Msg != nil && *row.Msg == *partial.Msg {
			return
		}
	}
	page.Entries = append(slices.Clone(page.Entries), partial)
	if len(page.Entries) > phoneEntryLimit {
		page.Entries = page.Entries[len(page.Entries)-phoneEntryLimit:]
		page.More = true
	}
}

// globalSnapshots partitions the desktop's global answers by lease, keeping
// their original result shape and discarding chats this computer does not own.
func (r *Runner) globalSnapshots(method string, held []string) ([]Envelope, error) {
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

func sealedSlotSize(slotName, clientID, writer string, atMs int64, ver uint64, payload []byte) int {
	p := clientproto.SlotPlain{
		V:        clientproto.SlotVersion,
		Slot:     slotName,
		Client:   clientID,
		Executor: writer,
		At:       max(atMs, minMeasuredAtMillis),
		Ver:      max(ver, 1),
		Payload:  payload,
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return clientproto.MaxSlotBytes + 1
	}
	return clientproto.NonceSize + box.Overhead + len(raw)
}

func measureEntriesSlot(results []Envelope, writer string, at time.Time, slotName, clientID string, ver uint64) (int, error) {
	envelopes := make([]Envelope, len(results))
	for i, row := range results {
		envelopes[i] = Envelope{Writer: writer, At: at, Agent: row.Agent, Result: row.Result}
	}
	plain, err := json.Marshal(envelopes)
	if err != nil {
		return 0, err
	}
	return sealedSlotSize(slotName, clientID, writer, at.UnixMilli(), ver, plain), nil
}

// trimEntries budgets the entire sealed authenticated-box slot. Oldest whole
// entries leave first; when a single newest entry still exceeds the slot limit,
// its tail is kept with TruncatedEntryMarker and More set so a long streaming
// answer never vanishes into an empty list (contract L5).
func trimEntries(results []Envelope, writer string, at time.Time, slotName, clientID string, ver uint64) error {
	for i, row := range results {
		if page, ok := row.Result.(*appwire.EntriesResult); ok {
			cloned := *page
			cloned.Entries = slices.Clone(page.Entries)
			results[i].Result = &cloned
		}
	}
	budget := clientproto.MaxSlotBytes - slotSizeMargin
	size, err := measureEntriesSlot(results, writer, at, slotName, clientID, ver)
	if err != nil {
		return err
	}
	for size > budget {
		oldest := -1
		for i, row := range results {
			page := row.Result.(*appwire.EntriesResult)
			if len(page.Entries) <= 1 {
				continue
			}
			if oldest < 0 || page.Entries[0].At.Before(results[oldest].Result.(*appwire.EntriesResult).Entries[0].At) {
				oldest = i
			}
		}
		if oldest < 0 {
			break
		}
		page := results[oldest].Result.(*appwire.EntriesResult)
		page.Entries = page.Entries[1:]
		page.More = true
		size, err = measureEntriesSlot(results, writer, at, slotName, clientID, ver)
		if err != nil {
			return err
		}
	}
	for size > budget {
		longest := -1
		for i, row := range results {
			page := row.Result.(*appwire.EntriesResult)
			if len(page.Entries) == 0 {
				continue
			}
			if longest < 0 || len(page.Entries[0].Text) > len(results[longest].Result.(*appwire.EntriesResult).Entries[0].Text) {
				longest = i
			}
		}
		if longest < 0 {
			return appwire.Errorf(appwire.CodeTooLarge, "chat envelopes exceed the slot limit")
		}
		page := results[longest].Result.(*appwire.EntriesResult)
		orig := page.Entries[0].Text
		if len(orig) <= len(TruncatedEntryMarker) {
			return appwire.Errorf(appwire.CodeTooLarge, "chat envelopes exceed the slot limit")
		}
		page.More = true
		low, high := 1, len(orig)
		best := ""
		for low <= high {
			mid := (low + high) / 2
			cut := mid
			for cut < len(orig) && !utf8.RuneStart(orig[cut]) {
				cut++
			}
			page.Entries[0].Text = TruncatedEntryMarker + orig[cut:]
			s, err := measureEntriesSlot(results, writer, at, slotName, clientID, ver)
			if err != nil {
				return err
			}
			if s <= budget {
				best = page.Entries[0].Text
				high = mid - 1
			} else {
				low = mid + 1
			}
		}
		if best == "" || best == TruncatedEntryMarker {
			return appwire.Errorf(appwire.CodeTooLarge, "chat envelopes exceed the slot limit")
		}
		page.Entries[0].Text = best
		size, err = measureEntriesSlot(results, writer, at, slotName, clientID, ver)
		if err != nil {
			return err
		}
	}
	return nil
}
