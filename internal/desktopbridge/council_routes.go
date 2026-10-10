package desktopbridge

// Council routes: the list of discussions, steering one, and the rows History
// would otherwise skip.
//
// A council is an ordinary chat (internal/council). This file does not run
// the turns. It lists what the store already recorded, and a steer is the
// person's message handed to the runner the desktop attached. Home's live
// list reads the same store through Places.Discussions.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/council"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func init() {
	registerSeamRoute(http.MethodGet, "/councils", councilsList)
	registerSeamRoute(http.MethodPost, "/councils/{id}/steer", councilSteer)
}

// councilDoor is the store and the runner a bridge was given. The runner may
// be nil when the process can list discussions but must not take a message.
type councilDoor struct {
	store *council.Store
	run   *council.Runner
}

// UseCouncils attaches the discussions door. A nil store leaves the list
// empty. A nil runner refuses steer. Places and History, if already attached,
// start reading this store; attaching either later does the same.
func (b *Bridge) UseCouncils(store *council.Store, run *council.Runner) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if store == nil {
		b.councils = nil
	} else {
		b.councils = &councilDoor{store: store, run: run}
	}
	b.attachCouncilsLocked()
}

// attachCouncilsLocked points Places and History at the council store. The
// bridge lock is held. A missing store clears nothing already set: a later
// UsePlaces on a bridge with no councils simply finds none.
func (b *Bridge) attachCouncilsLocked() {
	if b.councils == nil || b.councils.store == nil {
		return
	}
	store := b.councils.store
	if b.places != nil {
		b.places.Discussions = store.List
		b.places.CouncilFile = func(chatID string) string {
			dir := store.SessionDir(chatID)
			if dir == "" {
				return ""
			}
			return session.Place{Dir: dir}.Transcript()
		}
	}
	if b.history != nil {
		b.history.Extra = func() []session.SessionRow { return councilSessionRows(store) }
	}
}

// OpenCouncils opens the councils file beside the place graph and a runner
// that can take a person's message. The runner does not call a model: the
// turn loop is not this door. Sessions are a folder next to the graph, not a
// bucket under the places root, so the terminal's home does not gain a
// project named after discussions.
func OpenCouncils(places *placegraph.Store, path, sessionsDir string) (*council.Store, *council.Runner, error) {
	if places == nil {
		return nil, nil, fmt.Errorf("%w: no place graph", council.ErrInvalid)
	}
	store, err := council.Open(council.Options{Path: path, SessionsDir: sessionsDir, Places: places})
	if err != nil {
		return nil, nil, err
	}
	run, err := council.NewRunner(store, places, refuseCouncilAsk, journalSink{store: store})
	if err != nil {
		return nil, nil, err
	}
	return store, run, nil
}

func refuseCouncilAsk(context.Context, council.Request) (council.Answer, error) {
	return council.Answer{}, errors.New("this door lists and steers a discussion; it does not run the turns")
}

// councilItem is one discussion on the wire. The fields are the store's.
type councilItem struct {
	ID          string    `json:"id"`
	Places      [2]string `json:"places"`
	Topic       string    `json:"topic"`
	ChatID      string    `json:"chatId"`
	SessionFile string    `json:"sessionFile,omitempty"`
	Label       string    `json:"label"`
	Turns       int       `json:"turns"`
	Cap         int       `json:"cap"`
	Spend       float64   `json:"spend"`
	CapUSD      float64   `json:"capUSD"`
	State       string    `json:"state"`
	Outcome     string    `json:"outcome,omitempty"`
	OpenedAt    time.Time `json:"openedAt"`
	ClosedAt    time.Time `json:"closedAt,omitzero"`
}

func councilToItem(c council.Council, file string) councilItem {
	return councilItem{
		ID: c.ID, Places: c.Places, Topic: c.Topic, ChatID: c.ChatID, SessionFile: file,
		Label: c.Label, Turns: c.Turns, Cap: c.Cap, Spend: c.Spend, CapUSD: c.CapUSD,
		State: string(c.State), Outcome: c.Outcome, OpenedAt: c.OpenedAt, ClosedAt: c.ClosedAt,
	}
}

func (b *Bridge) councilStore() *council.Store {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.councils == nil {
		return nil
	}
	return b.councils.store
}

func (b *Bridge) councilRunner() *council.Runner {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.councils == nil {
		return nil
	}
	return b.councils.run
}

func councilsList(b *Bridge, w http.ResponseWriter, r *http.Request, _ map[string]string) {
	store := b.councilStore()
	if store == nil {
		write(w, map[string]any{"councils": []councilItem{}})
		return
	}
	var (
		all []council.Council
		err error
	)
	if place := strings.TrimSpace(r.URL.Query().Get("place")); place != "" {
		all, err = store.InPlace(place)
	} else {
		all, err = store.List()
	}
	if err != nil {
		fail(w, 500, "Discussions couldn't be read.")
		return
	}
	items := make([]councilItem, 0, len(all))
	for _, c := range all {
		items = append(items, councilToItem(c, councilFile(store, c.ChatID)))
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].OpenedAt.Equal(items[j].OpenedAt) {
			return items[i].OpenedAt.After(items[j].OpenedAt)
		}
		return items[i].ID < items[j].ID
	})
	write(w, map[string]any{"councils": items})
}

func councilFile(store *council.Store, chatID string) string {
	dir := store.SessionDir(chatID)
	if dir == "" {
		return ""
	}
	return session.Place{Dir: dir}.Transcript()
}

func councilSteer(b *Bridge, w http.ResponseWriter, r *http.Request, ids map[string]string) {
	run := b.councilRunner()
	if run == nil {
		fail(w, 409, "This discussion can't be steered from here.")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		fail(w, 400, "A message is needed.")
		return
	}
	c, err := run.Steer(ids["id"], body.Text)
	if err != nil {
		councilFail(w, err)
		return
	}
	file := ""
	if store := b.councilStore(); store != nil {
		file = councilFile(store, c.ChatID)
	}
	write(w, councilToItem(c, file))
}

func councilFail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, council.ErrNotFound):
		fail(w, 404, "That discussion doesn't exist.")
	case errors.Is(err, council.ErrEnded):
		fail(w, 409, "That discussion has ended.")
	case errors.Is(err, council.ErrInvalid):
		fail(w, 400, "That message can't be sent.")
	default:
		fail(w, 500, "That discussion couldn't be changed.")
	}
}

// journalSink writes a spoken line into the council's ordinary transcript.
// History reads that file. A place's own turns are not run from this door,
// so Escalate refuses and leaves the discussion running for a later runner.
type journalSink struct {
	store *council.Store
	now   func() time.Time
}

func (j journalSink) clock() time.Time {
	if j.now != nil {
		return j.now().UTC()
	}
	return time.Now().UTC()
}

func (j journalSink) Say(chatID, speaker, text string) error {
	if j.store == nil {
		return errors.New("no discussions")
	}
	dir := j.store.SessionDir(chatID)
	if dir == "" {
		return fmt.Errorf("%w: bad chat", council.ErrInvalid)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("%w: empty message", council.ErrInvalid)
	}
	role := "assistant"
	if speaker == council.SpeakerPerson {
		role = "user"
	}
	now := j.clock()
	line, err := json.Marshal(struct {
		Type      string `json:"type"`
		Role      string `json:"role"`
		Content   string `json:"content"`
		Timestamp string `json:"timestamp"`
	}{Type: "message", Role: role, Content: text, Timestamp: now.Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	path := session.Place{Dir: dir}.Transcript()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	if cerr != nil {
		return cerr
	}
	if role != "user" {
		return nil
	}
	meta, err := session.LoadMeta(dir)
	if err != nil {
		return err
	}
	meta.LastUserAt = now
	return session.SaveMeta(dir, meta)
}

func (j journalSink) Escalate(context.Context, council.Escalation) error {
	return errors.New("this door does not take a discussion further")
}

// councilSessionRows is what History adds for council chats the world walk
// does not return. A deleted chat stays deleted: the marker the delete door
// writes is honoured here too.
func councilSessionRows(store *council.Store) []session.SessionRow {
	if store == nil {
		return nil
	}
	all, err := store.List()
	if err != nil {
		return nil
	}
	out := make([]session.SessionRow, 0, len(all))
	for _, c := range all {
		dir := store.SessionDir(c.ChatID)
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, teams.ConversationDeletedFile)); err == nil {
			continue
		}
		meta, err := session.LoadMeta(dir)
		if err != nil || meta.ID != c.ChatID {
			continue
		}
		at := meta.LastUserAt
		if at.IsZero() {
			at = c.OpenedAt
		}
		if at.IsZero() {
			at = meta.Created
		}
		title := strings.TrimSpace(meta.Title)
		if title == "" {
			title = c.Label
		}
		transcript := session.Place{Dir: dir}.Transcript()
		if strings.TrimSpace(transcript) == "" {
			continue
		}
		out = append(out, session.SessionRow{
			ID: c.ChatID, Dir: dir, Transcript: transcript, Title: title,
			Workspace: strings.TrimSpace(meta.Workspace), Owned: meta.Owned,
			At: at, Created: meta.Created, Archived: meta.Archived,
		})
	}
	return out
}
