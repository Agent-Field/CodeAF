package tui3

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── A RUN NEVER STARTS WITHOUT A CHECKOUT ───────────────────────────────────
//
// `r` on an item whose repository this machine has no checkout of used to
// start anyway: plan and write were skipped and the run stopped at test with
// `codeaf does not know where … is checked out` (owner's screenshot,
// 2026-10-08 17:02). Now every launch the floor asks for (`r`, `L` with or
// without marks) reads [factory.Snapshot.Checkouts] first, and an item whose
// repository is not in it is not launched:
//
//   - WITH A CLONE DOOR the question stands where `U`'s does, `<repo> is not
//     checked out on this machine · clone it into ~/.codeaf/v3/factory/repos?`;
//     `y` clones off the loop with the note line spinning `cloning
//     owner/name…`, then asks the same gate again and launches; `n` and `esc`
//     launch nothing and say [factoryCloneNoWords].
//   - WITHOUT ONE nothing is asked: the note line says [factoryNoCloneWords].
//
// `L` OVER MARKS ON SEVERAL REPOSITORIES ASKS ONCE PER MISSING REPOSITORY, IN
// TURN: the gate stops at the first one, a `y` clones it and runs the gate
// again over the same items, which stops at the next one or launches them all.
// It is the same gate each time, so there is no second road to keep in step.
//
// A SEAM THAT DOES NOT SAY WHERE ANYTHING IS CHECKED OUT (Checkouts nil: the
// fixture, a far host) is not gated here; the launch door is its own floor.

// factoryCloneWait bounds a clone: a large repository over a slow line takes
// minutes, and the door's own error says what went wrong when it ends early.
const factoryCloneWait = 15 * time.Minute

// factoryCloneFolderWords is the folder a clone lands in, as the question
// says it: the factory's own folder's `repos` (cmd/codeaf's
// factoryCloneFolder), under the codeaf home.
const factoryCloneFolderWords = "~/.codeaf/v3/factory/repos"

// factoryRunKind is which launch asked the gate, for the note it leaves.
type factoryRunKind int

const (
	factoryRunItem   factoryRunKind = iota // `r`: the note names where the run stops
	factoryRunOne                          // `L` with nothing marked
	factoryRunMarked                       // `L`'s marks, after its own question
)

// factoryCloneAsk is the clone question while it stands: the repository as
// the floor shows it, the name the door is asked with, and the launch it
// stands in front of.
type factoryCloneAsk struct {
	repo string
	full string
	ids  []int
	kind factoryRunKind
}

// factoryNoCheckout is the first of ids whose repository has no checkout on
// this machine, and false when every one has (or the seam does not say).
func (a *app) factoryNoCheckout(ids []int) (factory.Item, bool) {
	have := a.fp.snap.Checkouts
	if have == nil {
		return factory.Item{}, false
	}
	for _, id := range ids {
		it, ok := a.factoryItemByID(id)
		if !ok || strings.TrimSpace(it.Repo) == "" {
			continue
		}
		if strings.TrimSpace(have[it.Repo]) == "" {
			return it, true
		}
	}
	return factory.Item{}, false
}

// factoryRepoFull is the name the Clone door is asked with: `owner/name` when
// the item knows its owner, else the name the floor shows, which the door
// finds among the watched repositories.
func factoryRepoFull(it factory.Item) string {
	repo := strings.TrimSpace(it.Repo)
	if owner := strings.TrimSpace(it.Product); owner != "" && !strings.Contains(repo, "/") {
		return owner + "/" + repo
	}
	return repo
}

// factoryCloneQuestion is the question's words, before its keys.
func factoryCloneQuestion(repo string) string {
	return factoryRepoShort(repo) + " is not checked out on this machine" + rowSep + "clone it into " + factoryCloneFolderWords
}

// factoryCloneNoWords is what `n` says: nothing was started, and the two ways on.
func factoryCloneNoWords(repo string) string {
	repo = factoryRepoShort(repo)
	return "not run" + rowSep + "clone " + repo + " first, or tell codeaf where it is in the repos list"
}

// factoryNoCloneWords is what a launch says on a seam with no Clone door.
func factoryNoCloneWords(repo string) string {
	return "not run" + rowSep + factoryNoCheckoutWords(factoryRepoShort(repo)) + rowSep + "open it from that folder once"
}

// factoryRunIDs is THE GATE every launch the floor asks goes through: it
// launches ids when each has a checkout, and otherwise asks to clone the
// first that has none, or says why it cannot.
func (a *app) factoryRunIDs(ids []int, kind factoryRunKind) tea.Cmd {
	if it, missing := a.factoryNoCheckout(ids); missing {
		if !a.factory.Has("clone") {
			a.factorySay(factoryNoCloneWords(it.Repo))
			return nil
		}
		a.pageMsg = ""
		a.fp.act.clone = &factoryCloneAsk{repo: it.Repo, full: factoryRepoFull(it), ids: ids, kind: kind}
		a.touch()
		return nil
	}
	return a.factoryLaunchNow(ids, kind)
}

// factoryLaunchNow launches ids through the seam's Launch door and says what
// became of them.
func (a *app) factoryLaunchNow(ids []int, kind factoryRunKind) tea.Cmd {
	if kind != factoryRunMarked && len(ids) == 1 {
		id := ids[0]
		return a.factoryVerb(id, func(s factory.Seam) error { return s.Launch(id) }, func(it factory.Item) string {
			tail := ""
			if kind == factoryRunItem && it.Gate == factory.GatePlan {
				tail = rowSep + factoryAskAtWords(it.Gate)
			}
			return a.factoryLaunchNote(it, tail)
		})
	}
	// THE MARKS ARE SPENT BY THE LAUNCH: an item that is now a stream has
	// nothing left for a mark to mean. A launch the gate held back (no
	// checkout, and `n` to the clone) keeps them.
	for _, m := range ids {
		delete(a.fp.marked, m)
	}
	return a.factoryDo(func(s factory.Seam) error {
		for _, m := range ids {
			if err := s.Launch(m); err != nil {
				return err
			}
		}
		return nil
	}, func(err error) {
		if err == nil {
			a.factorySay("launched " + itoa(len(ids)))
		}
	})
}

// factoryCloneKey is a key while the clone question stands: `y` clones and
// then runs the gate again, `n` and `esc` launch nothing and say so, and
// every other key is the question's and does nothing.
func (a *app) factoryCloneKey(k string) tea.Cmd {
	q := a.fp.act.clone
	switch k {
	case "y":
		a.fp.act.clone = nil
		a.pageMsg = ""
		a.fp.act.doing = "cloning " + q.full + "…"
		a.touch()
		return a.factoryDoThen(func(s factory.Seam) error {
			ctx, cancel := context.WithTimeout(context.Background(), factoryCloneWait)
			defer cancel()
			_, err := s.Clone(ctx, q.full)
			return err
		}, func(err error) tea.Cmd {
			if err != nil {
				return nil
			}
			// THE SAME GATE AGAIN, over the floor the clone's own read
			// folded: the next missing repository asks in turn, and a clone
			// the floor still does not see is refused rather than asked
			// about forever.
			if it, missing := a.factoryNoCheckout(q.ids); missing && it.Repo == q.repo {
				a.factorySay(factoryNoCloneWords(it.Repo))
				return nil
			}
			return a.factoryRunIDs(q.ids, q.kind)
		})
	case "n", "esc":
		a.fp.act.clone = nil
		a.factorySay(factoryCloneNoWords(q.repo))
	}
	return nil
}

// factoryCloneRows is the clone question as drawn: one row where it fits,
// and broken after the repository where it does not, the folder and the keys
// on the second row, so the folder is never cut off.
func (a *app) factoryCloneRows(measure int) []string {
	q := a.fp.act.clone
	if q == nil || measure <= 0 {
		return nil
	}
	pal := a.pal
	keys := pal.accent("[y] clone") + pal.dim(" · [n] not now")
	one := pal.ink(factoryCloneQuestion(q.repo)+"? ") + keys
	if ansi.StringWidth(one) <= measure {
		return []string{fit(one, measure)}
	}
	head := factoryRepoShort(q.repo) + " is not checked out on this machine"
	return []string{
		fit(pal.ink(head), measure),
		fit(pal.ink("clone it into "+factoryCloneFolderWords+"? ")+keys, measure),
	}
}
