package desktopbridge

// Tab-group offers over the history library.
//
// The desktop sends the canonical ids of the loose chats it has open. This
// file loads those chats from their own meta.json — title, recap line, and
// the recorded workspace — and asks placegraph for a repo offer or, when the
// set is not one repository, one topic question on the organizing role.
// A title or a path in the request is not a field the body has, so it cannot
// be the ground truth. Chats the library does not hold are left out.
//
// NOTHING HERE WRITES A PLACE. The offer is a list of chat ids and a name.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

const tabGroupIDBytes = 16
const tabGroupIDLimit = 40

// TabGroups is the offer door. Policy is the Places organizing policy, read
// on each question for that role's daily budget and cadence. Home is the
// directory that is never treated as a repository. Ask, when set, replaces
// the open conversation's Places.Ask; the bridge leaves it nil.
type TabGroups struct {
	Policy func() placegraph.RecommendPolicy
	Home   string
	Ask    placegraph.Asker
	gate   placegraph.TopicGate
}

// UseTabGroups attaches the offer door. Nil clears it; repo offers then have
// no home directory to exclude and no model is asked.
func (b *Bridge) UseTabGroups(g *TabGroups) {
	if g != nil {
		g.gate.Policy = g.Policy
	}
	b.mu.Lock()
	b.groups = g
	b.mu.Unlock()
}

type tabGroupOfferItem struct {
	Basis string   `json:"basis"`
	Key   string   `json:"key"`
	IDs   []string `json:"ids"`
	Title string   `json:"title,omitempty"`
}

type tabGroupOffers struct {
	Offers []tabGroupOfferItem `json:"offers"`
}

func (b *Bridge) groupOffers(w http.ResponseWriter, r *http.Request, history *History) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.IDs) > tabGroupIDLimit {
		fail(w, 400, "too many tabs")
		return
	}
	ids := canonicalChatIDs(body.IDs)
	if len(ids) < placegraph.TabGroupMinimum {
		write(w, tabGroupOffers{Offers: []tabGroupOfferItem{}})
		return
	}
	b.mu.Lock()
	groups := b.groups
	b.mu.Unlock()
	var (
		home string
		ask  placegraph.Asker
		gate *placegraph.TopicGate
	)
	if groups != nil {
		home = groups.Home
		gate = &groups.gate
		ask = groups.Ask
		if ask == nil && b.placesAskReady() {
			ask = b.tabGroupAsk
		}
	}
	if ask == nil {
		gate = nil
	}
	offers := placegraph.GroupOffers(r.Context(), home, history.tabChats(ids), ask, gate)
	bodyOut := tabGroupOffers{Offers: make([]tabGroupOfferItem, 0, len(offers))}
	for _, offer := range offers {
		if (offer.Basis != "repo" && offer.Basis != "topic") || len(offer.IDs) < placegraph.TabGroupMinimum {
			continue
		}
		bodyOut.Offers = append(bodyOut.Offers, tabGroupOfferItem{Basis: offer.Basis, Key: offer.Key, IDs: offer.IDs, Title: offer.Title})
	}
	write(w, bodyOut)
}

func canonicalChatIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] || !canonicalChatID(id) {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func canonicalChatID(id string) bool {
	if len(id) != tabGroupIDBytes {
		return false
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

func (h *History) tabChats(ids []string) []placegraph.TabChat {
	if h == nil || strings.TrimSpace(h.Root) == "" {
		return nil
	}
	byID := map[string]historyRow{}
	for _, row := range h.rows(nil) {
		if row.meta.ID != "" {
			byID[row.meta.ID] = row
		}
	}
	out := make([]placegraph.TabChat, 0, len(ids))
	for _, id := range ids {
		row, ok := byID[id]
		if !ok {
			continue
		}
		chat := placegraph.TabChat{ID: row.meta.ID, Title: strings.TrimSpace(row.meta.Title), Workspace: strings.TrimSpace(row.meta.Workspace)}
		if row.meta.Recap != nil {
			chat.Recap = strings.TrimSpace(row.meta.Recap.Line)
		}
		out = append(out, chat)
	}
	return out
}

func (b *Bridge) placesAskReady() bool {
	_, ok := conversationAsks(b.askingConversation())
	return ok
}

// tabGroupAsk is Places.Ask on an open conversation. The deadline is the
// request's, or 45 seconds, so a slow answer ends and the offer is simply absent.
func (b *Bridge) tabGroupAsk(ctx context.Context, req placegraph.ModelRequest) (string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
	}
	door, ok := conversationAsks(b.askingConversation())
	if !ok {
		return "", errNoTabGroupAsk
	}
	answer, err := door.AskPlaces(ctx, req)
	if err != nil {
		return "", err
	}
	return answer.Text, nil
}

var errNoTabGroupAsk = errors.New("no open conversation can answer a tab group question")
