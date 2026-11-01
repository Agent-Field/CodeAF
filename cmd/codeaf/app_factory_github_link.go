package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
)

const (
	// githubLinkAskWait bounds one ask of GitHub behind the reads.
	githubLinkAskWait = 20 * time.Second
	// githubLinkFresh is how long an answer stands before reads ask again; a
	// failed ask waits twice as long as the one before, up to githubLinkSlowest.
	githubLinkFresh   = time.Minute
	githubLinkSlowest = 10 * time.Minute
	// githubLinkFile keeps the person GitHub last named on the floor: a login
	// and how it was reached, never a token.
	githubLinkFile = "github-link.json"
)

// githubLink answers who the person is from what GitHub last said, and asks
// GitHub again behind the answer. THE FACTORY NEVER WAITS ON GITHUB TO OPEN: a
// hung gh or a dead network once left every read of the factory on this Mac
// waiting until the app called it "not answering". At most one ask is out at a
// time, bound to the host's context (which quitting cancels) and to
// [githubLinkAskWait]. When the floor has not heard who the person is, reads
// wait at most [factorygithub.FirstWait] from when the ask began, then answer
// without a person. A failed ask, or one that names nobody, keeps the last
// answer and backs off; one that names a person replaces it.
// The answer lives in memory, read from the floor once.
type githubLink struct {
	ctx   context.Context
	path  string
	spawn func(string, func()) bool // runs an ask in the background; false when it may not
	ask   func(context.Context) (factory.GitHubLink, error)

	mu          sync.Mutex
	link        factory.GitHubLink // the last answer; zero for none
	out         chan struct{}      // the ask that is out, or nil
	asked, next time.Time          // when it began; when the next may begin
	pause       time.Duration
}

// rememberGitHub hangs a githubLink over seam's GitHub door, and forgets the
// answer whenever the person connects GitHub again, so a new account is asked
// for at once.
func rememberGitHub(ctx context.Context, root string, spawn func(string, func()) bool, seam factory.Seam) factory.Seam {
	if seam.GitHub == nil {
		return seam
	}
	l := &githubLink{ctx: ctx, path: filepath.Join(root, githubLinkFile), spawn: spawn, ask: seam.GitHub, pause: githubLinkFresh}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, &l.link)
	}
	seam.GitHub = l.read
	if connect := seam.ConnectGitHub; connect != nil {
		seam.ConnectGitHub = func(ctx context.Context, token string) error {
			err := connect(ctx, token)
			if err == nil {
				l.forget()
			}
			return err
		}
	}
	return seam
}

func (l *githubLink) read(ctx context.Context) (factory.GitHubLink, error) {
	if link := l.known(); link.Login != "" {
		l.refresh()
		return link, nil
	}
	if done, began := l.refresh(); done != nil {
		select {
		case <-done:
		case <-time.After(time.Until(began.Add(factorygithub.FirstWait))):
		case <-ctx.Done():
		}
	}
	return l.known(), nil
}

func (l *githubLink) known() factory.GitHubLink {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.link
}

// forget drops the last answer, and the ask that is out with it, and lets the
// next read ask at once.
func (l *githubLink) forget() {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = os.Remove(l.path)
	l.link, l.out, l.next, l.pause = factory.GitHubLink{}, nil, time.Time{}, githubLinkFresh
}

// refresh starts an ask unless one is out or the last one says to wait, and
// answers the ask that is out (nil for none) and when it began.
func (l *githubLink) refresh() (<-chan struct{}, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.out != nil {
		return l.out, l.asked
	}
	if time.Now().Before(l.next) {
		return nil, time.Time{}
	}
	done := make(chan struct{})
	if !l.spawn("app/factory github link", func() { link, ok := l.once(); l.settle(done, link, ok) }) {
		return nil, time.Time{}
	}
	l.out, l.asked = done, time.Now()
	return done, l.asked
}

// once asks GitHub one time, and answers what it said and whether it did.
func (l *githubLink) once() (factory.GitHubLink, bool) {
	ctx, cancel := context.WithTimeout(l.ctx, githubLinkAskWait)
	defer cancel()
	link, err := l.ask(ctx)
	return link, err == nil && l.ctx.Err() == nil
}

// settle keeps what the ask behind done said, unless it was forgotten while
// it was out (the person connected GitHub again): then it keeps nothing.
func (l *githubLink) settle(done chan struct{}, link factory.GitHubLink, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	defer close(done)
	if l.out != done {
		return
	}
	// AN ANSWER OF NOBODY KEEPS THE LAST PERSON: it is what a gh that hangs
	// answers too, so it cannot tell a sign-out from a slow keychain. Only
	// connecting GitHub again ([githubLink.forget]) lets the person go; an
	// action that needs a sign-in says so where it is pressed.
	if ok && link.Login != "" {
		l.link, l.pause = link, githubLinkFresh
		if data, err := json.Marshal(link); err == nil {
			_ = writeFactoryPrivate(l.path, append(data, '\n'))
		}
	} else {
		l.pause = min(2*l.pause, githubLinkSlowest)
	}
	l.next, l.out = time.Now().Add(l.pause), nil
}
