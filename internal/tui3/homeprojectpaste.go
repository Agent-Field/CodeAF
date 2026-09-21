package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// homeProjectPaste is a one-use offer, separate from the draft's text. Once
// dismissed, the path remains message text until that draft is cleared.
type homeProjectPaste struct {
	path, text string
	literal    bool
}

func (h *homeView) pastedProject() string {
	if h.projectPaste.text != h.box.String() {
		return ""
	}
	return h.projectPaste.path
}

// offerPastedProject runs only for a paste into home's own box. The entire
// paste must resolve to one directory; prose and lists never choose a project.
func (a *app) offerPastedProject(text string, wasEmpty bool) {
	h := &a.home
	h.projectPaste.path = ""
	if word := strings.TrimSpace(text); strings.HasPrefix(word, "/") && knownCommand(strings.TrimPrefix(word, "/")) {
		return
	}
	if hits, _ := a.pasteResolve(text, false); len(hits) == 1 && hits[0].info.IsDir() {
		h.projectPaste.literal = true
		if wasEmpty && !a.hosted() {
			h.projectPaste.path = hits[0].path
			h.projectPaste.text = h.box.String()
			h.picked = false
		}
	}
}

// dismissProjectPaste leaves Enter as the offer's only accepting key. It is
// called before the key routers so motion, shortcuts and editing all dismiss.
func (a *app) dismissProjectPaste(msg tea.KeyPressMsg) {
	if !a.at(pageHome) || msg.String() == "enter" || a.home.projectPaste.path == "" {
		return
	}
	a.home.projectPaste.path = ""
	a.home.build()
	a.touch()
}
