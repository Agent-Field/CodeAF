package desktopbridge

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// errOrganizeRunning tells FileChat not to mark the chat weighed: a run is
// holding the tree, and the next settle can file it.
var errOrganizeRunning = errors.New("places are being organized")

func postGuess(p *Places, w http.ResponseWriter, r *http.Request, _ string) {
	if !needPost(w, r) {
		return
	}
	org := p.organizer()
	if org == nil || org.stream == nil {
		fail(w, 404, "unknown engine action")
		return
	}
	var ask struct {
		Draft     string `json:"draft"`
		Workspace string `json:"workspace"`
		ChatID    string `json:"chatId"`
	}
	if !readBody(w, r, &ask) {
		return
	}
	// An empty draft and a switched-off guess both draw nothing. The pill
	// hides when the body has no place.
	if !p.advice.policy().LiveGuess || (strings.TrimSpace(ask.Draft) == "" && strings.TrimSpace(ask.Workspace) == "") {
		write(w, struct{}{})
		return
	}
	_ = ask.ChatID
	snap, err := p.advice.snapshot()
	if err != nil {
		failPlaces(w, 500, "store", "Places could not be read.")
		return
	}
	tree, err := p.advice.existingTree()
	if err != nil {
		failPlaces(w, 500, "store", "Places could not be read.")
		return
	}
	org.prepareStream(snap)
	if id := folderPlace(snap, ask.Workspace); id != "" {
		if org.stream.Folders == nil {
			org.stream.Folders = map[string]string{}
		}
		org.stream.Folders[ask.Workspace] = id
	}
	guess, err := org.stream.Guess(r.Context(), tree, ask.Draft, ask.Workspace, recentPlace(snap, p.advice.now()))
	if err != nil || guess.Place == "" || strings.TrimSpace(guess.Name) == "" {
		write(w, struct{}{})
		return
	}
	if len(guess.Others) > 2 {
		guess.Others = guess.Others[:2]
	}
	write(w, guess)
}

// organizerFiling is FileChat's Decide when the organiser is wired. The
// folder rule still lives in FileChat for a bridge that has no runner. A
// local vector is never used here: a filing has to live in the tree's space.
func (a *PlaceAdvice) organizerFiling(ctx context.Context, chat placegraph.ChatEvidence, snap *placegraph.Snapshot) (placegraph.FilingChoice, error) {
	o := a.Organizer
	if o == nil || o.stream == nil {
		return placegraph.FilingChoice{}, nil
	}
	if running, _ := o.runningNow(); running {
		return placegraph.FilingChoice{}, errOrganizeRunning
	}
	tree, err := a.existingTree()
	if err != nil {
		return placegraph.FilingChoice{}, err
	}
	o.prepareStream(snap)
	c := placeorg.FromEvidence(chat, "", chat.UpdatedAt)
	c.Text = organizerText(chat)
	var vec []float32
	if o.Primary != nil && c.Text != "" && (o.State == nil || o.State.Space == "" || o.State.Space == o.Primary.Space()) {
		vecs, embedErr := o.Primary.Embed(ctx, []string{c.Text})
		if embedErr == nil && len(vecs) > 0 {
			vec = vecs[0]
			o.mem.put(chat.ChatID, vec)
		}
	}
	filing, err := o.stream.File(ctx, tree, placeorg.Arrival{
		Chat: c, Vector: vec, FolderPlace: folderPlace(snap, chat.Workspace),
	}, a.judge())
	if err != nil {
		return placegraph.FilingChoice{}, err
	}
	if filing.Op.Kind == placeorg.OpInbox || filing.Op.Place == "" {
		return placegraph.FilingChoice{}, nil
	}
	basis := placegraph.BasisFolder
	if filing.Judged {
		basis = placegraph.BasisOrganizer
	}
	reason := filing.Op.Why
	if reason == "" {
		reason = "Looks like the work in this place"
	}
	return placegraph.FilingChoice{PlaceID: filing.Op.Place, Confidence: 95, Reason: reason, Basis: basis}, nil
}

func (o *Organizer) prepareStream(snap *placegraph.Snapshot) {
	if o == nil || o.stream == nil {
		return
	}
	o.syncStream()
	o.stream.Folders = folderIndex(snap)
}

func folderIndex(snap *placegraph.Snapshot) map[string]string {
	if snap == nil {
		return nil
	}
	out := map[string]string{}
	best := map[string]int{}
	for _, p := range snap.Places {
		if p.Archived {
			continue
		}
		for _, src := range p.Context.Sources {
			if src.Kind != placegraph.SourceFolder && src.Kind != placegraph.SourceRepo {
				continue
			}
			ref := placegraph.CleanFolder(src.Ref)
			if ref == "" {
				continue
			}
			if prev, ok := best[ref]; ok && prev >= len(ref) && out[ref] != "" {
				continue
			}
			best[ref] = len(ref)
			out[ref] = p.ID
		}
	}
	return out
}

// folderPlace is the deepest place whose folder source covers workspace.
func folderPlace(snap *placegraph.Snapshot, workspace string) string {
	workspace = placegraph.CleanFolder(workspace)
	if snap == nil || workspace == "" {
		return ""
	}
	sep := string(filepath.Separator)
	best, bestLen := "", -1
	for _, p := range snap.Places {
		if p.Archived {
			continue
		}
		for _, src := range p.Context.Sources {
			if src.Kind != placegraph.SourceFolder && src.Kind != placegraph.SourceRepo {
				continue
			}
			ref := placegraph.CleanFolder(src.Ref)
			if ref == "" {
				continue
			}
			if workspace != ref && !strings.HasPrefix(workspace, ref+sep) {
				continue
			}
			if len(ref) > bestLen {
				best, bestLen = p.ID, len(ref)
			}
		}
	}
	return best
}

// recentPlace is the place opened within the last hour, for the guess's
// small continuation prior. None means there is no prior.
func recentPlace(snap *placegraph.Snapshot, now time.Time) string {
	if snap == nil {
		return ""
	}
	var best string
	var at time.Time
	for _, p := range snap.Places {
		if p.Archived || p.LastOpenedAt.IsZero() || now.Sub(p.LastOpenedAt) > placeorg.ContinuationWindow {
			continue
		}
		if at.IsZero() || p.LastOpenedAt.After(at) {
			best, at = p.ID, p.LastOpenedAt
		}
	}
	return best
}
