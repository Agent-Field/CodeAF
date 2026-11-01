package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const (
	// githubLinkFirstWait is how long reads wait for GitHub when this floor
	// has not heard who the person is, counted from when the ask began. Past it
	// a read answers without a person, and the answer lands for the next read.
	githubLinkFirstWait = 300 * time.Millisecond
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
// [githubLinkAskWait]. A failed ask keeps the last answer and backs off; a
// successful one replaces it, and an answer of nobody (signed out) clears it.
type githubLink struct {
	ctx   context.Context
	path  string
	spawn func(string, func()) bool // runs an ask in the background; false when it may not
	ask   func(context.Context) (factory.GitHubLink, error)

	mu          sync.Mutex
	out         chan struct{} // the ask that is out, or nil
	asked, next time.Time     // when it began; when the next may begin
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
	if link, ok := l.known(); ok {
		l.refresh()
		return link, nil
	}
	if done, began := l.refresh(); done != nil {
		select {
		case <-done:
		case <-time.After(time.Until(began.Add(githubLinkFirstWait))):
		case <-ctx.Done():
		}
	}
	link, _ := l.known()
	return link, nil
}

func (l *githubLink) known() (factory.GitHubLink, bool) {
	var link factory.GitHubLink
	data, err := os.ReadFile(l.path)
	return link, err == nil && json.Unmarshal(data, &link) == nil && link.Login != ""
}

// forget drops the last answer and lets the next read ask at once.
func (l *githubLink) forget() {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = os.Remove(l.path)
	l.next, l.pause = time.Time{}, githubLinkFresh
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
	if !l.spawn("app/factory github link", func() { l.settle(done, l.once()) }) {
		return nil, time.Time{}
	}
	l.out, l.asked = done, time.Now()
	return done, l.asked
}

// once asks GitHub one time and keeps what it said, and answers whether it did.
func (l *githubLink) once() bool {
	ctx, cancel := context.WithTimeout(l.ctx, githubLinkAskWait)
	defer cancel()
	link, err := l.ask(ctx)
	if err != nil || l.ctx.Err() != nil {
		return false
	}
	if link.Login == "" {
		_ = os.Remove(l.path)
	} else if data, err := json.Marshal(link); err == nil {
		_ = writeFactoryPrivate(l.path, append(data, '\n'))
	}
	return true
}

func (l *githubLink) settle(done chan struct{}, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ok {
		l.pause = githubLinkFresh
	} else {
		l.pause = min(2*l.pause, githubLinkSlowest)
	}
	l.next, l.out = time.Now().Add(l.pause), nil
	close(done)
}
