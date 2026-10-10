package desktopbridge

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// Mutation is the receipt of one write. 2xx means the store committed: Revision
// is the graph's revision afterwards, Receipts are the commits this request made
// (none when nothing changed), and Undo lists the receipt ids to hand to
// POST /places/undo. Nothing here is a promise the store did not make.
type Mutation struct {
	Revision uint64 `json:"revision"`
	// Generation is Revision under the name the desktop client reads.
	Generation uint64               `json:"generation"`
	Rail       *RailView            `json:"rail,omitempty"`
	Receipts   []placegraph.Receipt `json:"receipts"`
	Noop       bool                 `json:"noop"`
	Place      *PlaceDetail         `json:"place,omitempty"`
	Undo       []string             `json:"undo"`
	// Result carries what a delete or a merge did (store DeleteResult / MergeResult).
	Result any `json:"result,omitempty"`
	// Memberships are the filings a members write created.
	Memberships []placegraph.Membership `json:"memberships,omitempty"`
}

// batch collects the receipts of one request's store calls.
type batch struct{ receipts []placegraph.Receipt }

func (b *batch) add(rc placegraph.Receipt) {
	if !rc.Noop() {
		b.receipts = append(b.receipts, rc)
	}
}

// finish writes the success answer, with the changed place when there is one.
func (p *Places) finish(w http.ResponseWriter, b *batch, placeID string, extra func(*Mutation)) {
	rev, err := p.Store.Revision()
	if err != nil {
		p.failStore(w, err, "", b.receipts)
		return
	}
	m := Mutation{Revision: rev, Generation: rev, Receipts: b.receipts, Noop: len(b.receipts) == 0, Undo: []string{}}
	if m.Receipts == nil {
		m.Receipts = []placegraph.Receipt{}
	}
	for _, rc := range b.receipts {
		m.Undo = append(m.Undo, rc.ID)
	}
	if placeID != "" {
		if x, ok := p.open(w, false); ok {
			if pl, found := x.snap.Place(placeID); found {
				d := x.detail(pl)
				m.Place = &d
			}
		} else {
			return
		}
	}
	if extra != nil {
		extra(&m)
	}
	write(w, m)
	if !m.Noop {
		p.publishPlaces(m.Memberships)
	}
}

// ---- places ----------------------------------------------------------------

type createAsk struct {
	IfRevision   *uint64         `json:"ifRevision"`
	Name         string          `json:"name"`
	Parent       string          `json:"parent"`
	Parents      []string        `json:"parents"`
	Tint         placegraph.Tint `json:"tint"`
	Instructions string          `json:"instructions"`
}

func (p *Places) create(w http.ResponseWriter, r *http.Request) {
	var ask createAsk
	if !readBody(w, r, &ask) {
		return
	}
	name := strings.TrimSpace(ask.Name)
	if name == "" {
		failPlaces(w, 400, "invalid", "A place needs a name.")
		return
	}
	if ask.Tint != "" && !ask.Tint.Valid() {
		failPlaces(w, 400, "invalid", "That isn't one of the six place colours.")
		return
	}
	parents := ask.Parents
	if ask.Parent != "" {
		parents = append([]string{ask.Parent}, parents...)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	pl, rc, err := p.Store.CreatePlace(placegraph.NewPlace{Name: name, Parents: parents, Tint: ask.Tint, Context: placegraph.Context{Instructions: ask.Instructions}})
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var b batch
	b.add(rc)
	p.finish(w, &b, pl.ID, nil)
}

type updateAsk struct {
	IfRevision   *uint64 `json:"ifRevision"`
	Name         *string `json:"name"`
	Tint         *string `json:"tint"`
	Instructions *string `json:"instructions"`
	Policy       *struct {
		Model       *string `json:"model"`
		Permissions *string `json:"permissions"`
	} `json:"policy"`
}

// update applies each named field as its own store call, in a fixed order, and
// reports every receipt. A failure part-way returns the error and the receipts
// already committed; nothing is rolled back behind the person's back.
func (p *Places) update(w http.ResponseWriter, r *http.Request, id string) {
	var ask updateAsk
	if !readBody(w, r, &ask) {
		return
	}
	if ask.Name == nil && ask.Tint == nil && ask.Instructions == nil && ask.Policy == nil {
		failPlaces(w, 400, "invalid", "There is nothing to change.")
		return
	}
	if ask.Tint != nil && *ask.Tint != "" && !placegraph.Tint(*ask.Tint).Valid() {
		failPlaces(w, 400, "invalid", "That isn't one of the six place colours.")
		return
	}
	if ask.Name != nil && strings.TrimSpace(*ask.Name) == "" {
		failPlaces(w, 400, "invalid", "A place needs a name.")
		return
	}
	if ask.Policy != nil && !p.checkPolicy(w, r.Context(), ask.Policy.Model, ask.Policy.Permissions) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	snap, err := p.Store.Snapshot()
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	pl, found := snap.Place(id)
	if !found {
		failPlaces(w, 404, "not_found", "That place doesn't exist any more.")
		return
	}
	var b batch
	step := func(rc placegraph.Receipt, err error) bool {
		if err != nil {
			p.failStore(w, err, "", b.receipts)
			return false
		}
		b.add(rc)
		return true
	}
	if ask.Name != nil && !step(p.Store.Rename(id, strings.TrimSpace(*ask.Name))) {
		return
	}
	if ask.Tint != nil && !step(p.Store.SetTint(id, placegraph.Tint(*ask.Tint))) {
		return
	}
	if ask.Instructions != nil {
		c := pl.Context
		c.Instructions = *ask.Instructions
		if !step(p.Store.SetContext(id, c)) {
			return
		}
	}
	if ask.Policy != nil {
		pol := pl.Policy
		if ask.Policy.Model != nil {
			pol.Model = *ask.Policy.Model
		}
		if ask.Policy.Permissions != nil {
			pol.Permissions = *ask.Policy.Permissions
		}
		if !step(p.Store.SetPolicy(id, pol)) {
			return
		}
	}
	p.finish(w, &b, id, nil)
}

type parentsAsk struct {
	IfRevision *uint64   `json:"ifRevision"`
	Add        string    `json:"add"`
	Remove     string    `json:"remove"`
	Set        *[]string `json:"set"`
}

func (p *Places) parents(w http.ResponseWriter, r *http.Request, id string) {
	var ask parentsAsk
	if !readBody(w, r, &ask) {
		return
	}
	n := 0
	for _, used := range []bool{ask.Add != "", ask.Remove != "", ask.Set != nil} {
		if used {
			n++
		}
	}
	if n != 1 {
		failPlaces(w, 400, "invalid", "Say one thing to do: add a parent, remove one, or set them all.")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	snap, err := p.Store.Snapshot()
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var rc placegraph.Receipt
	switch {
	case ask.Add != "":
		rc, err = p.Store.AddParent(id, ask.Add)
	case ask.Remove != "":
		rc, err = p.Store.RemoveParent(id, ask.Remove)
	default:
		rc, err = p.Store.Reparent(id, *ask.Set)
	}
	if err != nil {
		sentence := ""
		target := ask.Add
		if target == "" && ask.Set != nil && len(*ask.Set) > 0 {
			target = (*ask.Set)[0]
		}
		if target != "" {
			sentence = "That would put " + placeName(snap, id) + " inside " + placeName(snap, target) + ", which is already inside it."
			if id == target {
				sentence = "A place can't be inside itself."
			}
		}
		p.failStore(w, err, sentence, nil)
		return
	}
	var b batch
	b.add(rc)
	p.finish(w, &b, id, nil)
}

type plainAsk struct {
	IfRevision *uint64 `json:"ifRevision"`
	Into       string  `json:"into"`
	Index      *int    `json:"index"`
}

func (p *Places) archive(w http.ResponseWriter, r *http.Request, id string, archive bool) {
	var ask plainAsk
	if !readBody(w, r, &ask) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	var rc placegraph.Receipt
	var err error
	if archive {
		rc, err = p.Store.Archive(id)
	} else {
		rc, err = p.Store.Restore(id)
	}
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var b batch
	b.add(rc)
	p.finish(w, &b, id, nil)
}

func (p *Places) deletePlace(w http.ResponseWriter, r *http.Request, id string) {
	var ask plainAsk
	if !readBody(w, r, &ask) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	res, rc, err := p.Store.DeletePlace(id)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var b batch
	b.add(rc)
	p.finish(w, &b, "", func(m *Mutation) { m.Result = res })
}

func (p *Places) merge(w http.ResponseWriter, r *http.Request, id string) {
	var ask plainAsk
	if !readBody(w, r, &ask) {
		return
	}
	if ask.Into == "" {
		failPlaces(w, 400, "invalid", "Say which place to merge into.")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	res, rc, err := p.Store.MergePlaces(id, ask.Into)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var b batch
	b.add(rc)
	p.finish(w, &b, ask.Into, func(m *Mutation) { m.Result = res })
}

func (p *Places) pin(w http.ResponseWriter, r *http.Request, id string, pin bool) {
	var ask plainAsk
	if !readBody(w, r, &ask) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	var rc placegraph.Receipt
	var err error
	if pin {
		index := -1
		if ask.Index != nil {
			index = *ask.Index
		}
		rc, err = p.Store.Pin(id, index)
	} else {
		rc, err = p.Store.Unpin(id)
	}
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var b batch
	b.add(rc)
	p.finish(w, &b, id, nil)
}

// visit records that the person went to a place. It moves no revision and has no
// receipt: going somewhere is not a change anybody would want to undo.
func (p *Places) visit(w http.ResponseWriter, r *http.Request, id string) {
	var ask plainAsk
	if !readBody(w, r, &ask) {
		return
	}
	if err := p.Store.TouchOpened(id, p.now()); err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	// Going somewhere also puts it at the top of the rail's Open section.
	if err := p.Store.Visit(id); err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	write(w, map[string]bool{"ok": true})
}

type undoAsk struct {
	Receipts []string `json:"receipts"`
}

// undo takes receipts back newest first, so a request that lists the receipts
// of a multi-step write in the order they were made unwinds them correctly. It
// stops at the first the store refuses and says how many were undone.
func (p *Places) undo(w http.ResponseWriter, r *http.Request) {
	var ask undoAsk
	if !readBody(w, r, &ask) {
		return
	}
	if len(ask.Receipts) == 0 {
		failPlaces(w, 400, "invalid", "There is nothing to undo.")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	undone := 0
	var rev uint64
	for i := len(ask.Receipts) - 1; i >= 0; i-- {
		var err error
		if rev, err = p.Store.Undo(ask.Receipts[i]); err != nil {
			status, code, sentence := storeFailure(err, "")
			writeStatus(w, status, placesError{Error: sentence, Code: code, Undone: &undone})
			return
		}
		undone++
	}
	write(w, map[string]any{"revision": rev, "undone": undone})
}

// ---- memberships -----------------------------------------------------------

const membersMax = 200

type membersAsk struct {
	IfRevision *uint64            `json:"ifRevision"`
	Chats      []string           `json:"chats"`
	AddedBy    placegraph.AddedBy `json:"addedBy"`
	MoveFrom   string             `json:"moveFrom"`
}

// checkedChats validates a list of chat ids and refuses the whole request
// before anything is written when one of them names no real conversation.
func (p *Places) checkedChats(w http.ResponseWriter, chats []string) ([]string, bool) {
	if len(chats) == 0 {
		failPlaces(w, 400, "invalid", "Say which chats.")
		return nil, false
	}
	if len(chats) > membersMax {
		failPlaces(w, 413, "too_large", "That is too many chats at once.")
		return nil, false
	}
	x, ok := p.open(w, true)
	if !ok {
		return nil, false
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range chats {
		c = strings.TrimSpace(c)
		if c == "" {
			failPlaces(w, 400, "invalid", "A chat id is empty.")
			return nil, false
		}
		if !x.knowsChat(p, c) {
			failPlaces(w, 404, "unknown_chat", "That conversation isn't saved on this machine, so it can't be filed yet.")
			return nil, false
		}
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out, true
}

func (p *Places) addMembers(w http.ResponseWriter, r *http.Request, id string) {
	var ask membersAsk
	if !readBody(w, r, &ask) {
		return
	}
	by := ask.AddedBy
	if by == "" {
		by = placegraph.AddedByYou
	}
	if !by.Valid() {
		failPlaces(w, 400, "invalid", "A chat is filed by you or by the AI.")
		return
	}
	if id == placegraph.NowID && ask.MoveFrom == "" {
		failPlaces(w, 400, "invalid", "Now holds the chats that aren't in any place. To put a chat there, move it out of its place.")
		return
	}
	if id == placegraph.RootID {
		failPlaces(w, 400, "invalid", "Chats are filed in a place, not in All places.")
		return
	}
	chats, ok := p.checkedChats(w, ask.Chats)
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	var b batch
	var filed []placegraph.Membership
	for _, c := range chats {
		if ask.MoveFrom != "" {
			rc, err := p.Store.MoveChat(c, ask.MoveFrom, id, by)
			if err != nil {
				p.failStore(w, err, "", b.receipts)
				return
			}
			b.add(rc)
			continue
		}
		m, rc, err := p.Store.AddChat(c, id, by)
		if err != nil {
			p.failStore(w, err, "", b.receipts)
			return
		}
		b.add(rc)
		filed = append(filed, m)
	}
	place := id
	if id == placegraph.NowID {
		place = ""
	}
	p.finish(w, &b, place, func(m *Mutation) {
		if len(filed) > 0 {
			m.Memberships = filed
		}
	})
}

func (p *Places) removeMembers(w http.ResponseWriter, r *http.Request, id string) {
	var ask membersAsk
	if !readBody(w, r, &ask) {
		return
	}
	if len(ask.Chats) == 0 || len(ask.Chats) > membersMax {
		failPlaces(w, 400, "invalid", "Say which chats.")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	var b batch
	for _, c := range ask.Chats {
		rc, err := p.Store.RemoveChat(c, id)
		if err != nil {
			p.failStore(w, err, "", b.receipts)
			return
		}
		b.add(rc)
	}
	p.finish(w, &b, id, nil)
}
