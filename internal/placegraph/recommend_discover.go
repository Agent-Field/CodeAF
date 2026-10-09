package placegraph

// Finding a group no rule can see, and how many questions one organizing pass
// may ask.
//
// WHY A MODEL IS SHOWN WHAT THE RULES LEFT OVER. Rules group chats by a shared
// folder and by a shared topic word. Real chats about one subject often share
// neither: five chats about a home network were titled "5 GHz Wi-Fi drops every
// evening", "Fixed vs auto router channel", "Mesh vs extender for upstairs dead
// zone", "Laptop prefers 2.4 GHz over 5 GHz" and "Detecting neighbour Wi-Fi
// interference" (deepseek/deepseek-v4.1-flash, 2026-10-09) — the mesh chat never
// says Wi-Fi, and no word reaches five of them. No word rule finds that group
// without also finding groups that are not there, so none was written.
//
// THE QUESTION IS BOUNDED THE SAME WAY EVERY OTHER ONE IS. It is asked at most
// once per organizing pass, after the rule groups, only when at least
// MinClusterChats chats are left that no rule grouped and no offer already
// covers; it shows at most maxDiscoveryShown of them, newest first; it is the
// same Place suggestions role, from the same daily budget, through the same
// engine door; and its answer is held to the same contract — labels it was
// shown, at least MinClusterChats of them, at least MinConfidence sure, a name
// a person approves. A set it was shown and found nothing in is remembered,
// and is not shown again until more than half of what is left is new, or the
// snooze has passed.
//
// ONE PASS, ONE INTERVAL. The organizing interval (OrganizeEveryMinutes) says
// how long a pass that calls a model waits after the last one. It used to be
// enforced per CALL, so a pass that found three groups asked about the first
// and left the other two for an hour each. A pass now spends the interval
// once and may then ask about each group it found, up to maxPassCalls, still
// within ClusterCallsPerDay.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/roles"
)

// BasisDiscover marks a group the model found among chats no rule grouped.
const BasisDiscover = "discover"

// maxPassCalls is the most model questions one organizing pass may ask: one
// per rule group it considers, and one to find a group among the rest.
const maxPassCalls = maxClustersPass + 1

// Discovery sees a broader bounded sample than an already identified cluster.
// Showing only 24 newest chats hides older projects in a mixed saved library.
const maxDiscoveryShown = 64

// organizePass is what one Organize call has spent. A pass whose ask failed
// asks nothing more: the engine that could not answer one question will not
// answer the next one a moment later, and each try may cost.
type organizePass struct {
	calls  int
	failed bool
}

// reservePassCall spends one Place suggestions call for this pass. The pass's
// first call waits out the organizing interval; the rest of the same pass do
// not, and every call still counts against the daily budget.
func (r *Recommender) reservePassCall(pass *organizePass, role string, pol RecommendPolicy) (bool, error) {
	if pass == nil {
		pass = &organizePass{}
	}
	if pass.calls >= maxPassCalls || pass.failed {
		return false, nil
	}
	every := time.Duration(pol.OrganizeEveryMinutes) * time.Minute
	if pass.calls > 0 {
		every = 0
	}
	ok, err := r.reserveCall(role, pol.ClusterCallsPerDay, every, nil)
	if ok {
		pass.calls++
	}
	return ok, err
}

// inAnyOffer reports whether a chat is already the subject of an offer that
// stands: one made in this pass, one open or accepted, or one declined or
// found wanting within the snooze. Such a chat is not shown again.
func inAnyOffer(st *ledgerState, fresh []Proposal, chatID string, now time.Time, pol RecommendPolicy) bool {
	for _, p := range fresh {
		if p.Status == StatusPending && contains(p.ChatIDs, chatID) {
			return true
		}
	}
	snooze := time.Duration(pol.DeclineSnoozeDays) * 24 * time.Hour
	for _, p := range st.Proposals {
		if p.Basis == BasisDiscover && p.Status == StatusDropped {
			continue // what was shown, not what was offered
		}
		if !contains(p.ChatIDs, chatID) {
			continue
		}
		switch p.Status {
		case StatusPending, StatusAccepted:
			return true
		case StatusDeclined, StatusDropped:
			if now.Sub(p.DecidedAt) < snooze {
				return true
			}
		}
	}
	return false
}

// discoverOffer shows the chats no rule grouped to the model once, and offers
// the one group it finds, or records that it found none.
func (r *Recommender) discoverOffer(ctx context.Context, snap *Snapshot, st *ledgerState, pol RecommendPolicy, rest []ChatEvidence, lib map[string]ChatEvidence, pass *organizePass) (*Proposal, error) {
	if len(rest) < pol.MinClusterChats {
		return nil, nil
	}
	shown := append([]ChatEvidence(nil), rest...)
	// Older unseen chats must eventually get a turn in a large library.
	// Sets already examined without an offer are lower priority during the
	// snooze; the same whole set still costs no further question.
	now := r.now()
	snooze := time.Duration(pol.DeclineSnoozeDays) * 24 * time.Hour
	seen := map[string]bool{}
	for _, p := range st.Proposals {
		if p.Basis == BasisDiscover && p.Status == StatusDropped && now.Sub(p.DecidedAt) < snooze {
			for _, id := range p.ChatIDs {
				seen[id] = true
			}
		}
	}
	sort.SliceStable(shown, func(i, j int) bool {
		if seen[shown[i].ChatID] != seen[shown[j].ChatID] {
			return !seen[shown[i].ChatID]
		}
		if !shown[i].UpdatedAt.Equal(shown[j].UpdatedAt) {
			return shown[i].UpdatedAt.After(shown[j].UpdatedAt)
		}
		return shown[i].ChatID < shown[j].ChatID
	})
	if len(shown) > maxDiscoveryShown {
		shown = shown[:maxDiscoveryShown]
	}
	ids := make([]string, len(shown))
	for i, c := range shown {
		ids[i] = c.ChatID
	}
	sort.Strings(ids)
	for _, p := range st.Proposals {
		if p.Basis == BasisDiscover && p.Status == StatusDropped && now.Sub(p.DecidedAt) < snooze && overlapsHalf(p.ChatIDs, ids) {
			return nil, nil // shown this set, or most of it, and found nothing
		}
	}
	// The places worth naming are each shown chat's own best match by rules,
	// most often matched first: a place two chats point at is likelier to be
	// where a group of them belongs than one a single chat brushes against.
	hits := map[string]int{}
	byID := map[string]candidate{}
	for _, c := range shown {
		if cs := rankCandidates(snap, cleanFolder(c.Workspace), chatWords(c), nil, lib); len(cs) > 0 {
			hits[cs[0].place.ID]++
			byID[cs[0].place.ID] = cs[0]
		}
	}
	var cands []candidate
	for id := range hits {
		cands = append(cands, byID[id])
	}
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if hits[a.place.ID] != hits[b.place.ID] {
			return hits[a.place.ID] > hits[b.place.ID]
		}
		if a.place.Name != b.place.Name {
			return a.place.Name < b.place.Name
		}
		return a.place.ID < b.place.ID
	})
	if len(cands) > pol.MaxCandidates {
		cands = cands[:pol.MaxCandidates]
	}
	counts := countAI(snap, st.AIPlaces)
	rootOK, _ := canCreateUnder(snap, pol, counts, "")
	var parents []Place
	for _, c := range cands {
		if ok, _ := canCreateUnder(snap, pol, counts, c.place.ID); ok {
			parents = append(parents, c.place)
		}
	}
	if len(cands) == 0 && len(parents) == 0 && !rootOK {
		return nil, nil
	}
	q := discoverQuestion(snap, shown, cands, parents, pol.MinClusterChats)
	reserved, err := r.reservePassCall(pass, string(q.Role), pol)
	if err != nil || !reserved {
		return nil, err
	}
	found := &Proposal{ID: proposalID("discover|" + strings.Join(ids, "|")), Kind: ProposalCreate, Status: StatusDropped, ChatIDs: ids,
		Basis: BasisDiscover, Revision: snap.Revision, CreatedAt: now, DecidedAt: now}
	answer, err := r.Ask(ctx, q)
	if err != nil {
		pass.failed = true
		return nil, err // the engine could not answer; the set may be shown again
	}
	parentNames := make([]string, len(parents))
	for i, p := range parents {
		parentNames[i] = p.Name
	}
	sg, err := readSuggestAnswer(answer, len(shown), candidateNames(cands), parentNames)
	if err != nil {
		return found, err
	}
	if sg == nil || len(sg.chats) < pol.MinClusterChats || sg.confidence < pol.MinConfidence {
		return found, nil
	}
	var chats []string
	for _, i := range sg.chats {
		chats = append(chats, shown[i].ChatID)
	}
	sort.Strings(chats)
	if sg.use >= 0 {
		return r.moveOffer(snap, cands[sg.use].place, sg.confidence, BasisModel, chats), nil
	}
	parent := ""
	if sg.under >= 0 {
		parent = parents[sg.under].ID
	} else if !rootOK {
		return found, nil
	}
	return r.createOffer(snap, sg.name, parent, sg.confidence, BasisModel, chats), nil
}

// discoverQuestion asks for the one largest group among chats that are mostly
// unrelated. Its answer has the suggest question's shape, so it is read by the
// same contract.
func discoverQuestion(s *Snapshot, chats []ChatEvidence, existing []candidate, parents []Place, least int) ModelRequest {
	q := suggestQuestion(s, chats, existing, parents)
	head := q.User[:strings.LastIndex(q.User, "\n\nDo most of these chats")]
	q.User = head + fmt.Sprintf("\n\nMost of these chats are probably unrelated. Find the largest set of at least %d that are clearly about the same subject or project, and list only those. ", least) +
		groupingGuard +
		fmt.Sprintf("If no %d of them share a subject, answer belong false. ", least) +
		"Prefer an existing place when one fits. Otherwise name a new place in one to four plain words. " +
		"Use labels (c1, p1, u1), not names, wherever a label is asked for. " + suggestShape
	q.Role = roles.RolePlaceSuggest
	return q
}

// unlessNamedTwice keeps two open offers from proposing places of the same
// name under the same parent: a person who accepted both would be refused the
// second for a taken name, and two groups the model names alike are, by its
// own account, one subject. The later offer is recorded as found wanting
// (so its chats are not asked about again within the snooze) instead.
func (r *Recommender) unlessNamedTwice(st *ledgerState, fresh []Proposal, p *Proposal) *Proposal {
	if p == nil || p.Kind != ProposalCreate || p.Status != StatusPending {
		return p
	}
	key := p.ParentID + "\x01" + sameNameKey(p.Name)
	clash := func(o Proposal) bool {
		return o.Kind == ProposalCreate && o.Status == StatusPending && o.ID != p.ID && o.ParentID+"\x01"+sameNameKey(o.Name) == key
	}
	for _, o := range fresh {
		if clash(o) {
			p.Status, p.DecidedAt = StatusDropped, r.now()
			return p
		}
	}
	for _, o := range st.Proposals {
		if clash(o) {
			p.Status, p.DecidedAt = StatusDropped, r.now()
			return p
		}
	}
	return p
}
