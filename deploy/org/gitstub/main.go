// The smoke test uses this server to exercise git smart HTTP with fake access.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type gitCall struct {
	At           time.Time `json:"at"`
	Path         string    `json:"path"`
	Service      string    `json:"service"`
	User         string    `json:"user"`
	PasswordOK   bool      `json:"password_ok"`
	PasswordTail string    `json:"password_tail,omitempty"`
}

type gitStub struct {
	git, root, password string
	reposMu             sync.Mutex
	callsMu             sync.Mutex
	calls               []gitCall
}

var repoPath = regexp.MustCompile(`^/([A-Za-z0-9][A-Za-z0-9._-]{0,99})/([A-Za-z0-9][A-Za-z0-9._-]{0,99}\.git)/(info/refs|git-upload-pack|git-receive-pack)$`)

func newGitStub(git, root, password string) *gitStub {
	return &gitStub{git: git, root: root, password: password}
}

func main() {
	git, err := exec.LookPath("git")
	if err != nil {
		log.Fatal(err)
	}
	password := os.Getenv("GITSTUB_TOKEN")
	if password == "" {
		log.Fatal("GITSTUB_TOKEN is required.")
	}
	server := &http.Server{
		Addr:              ":8891",
		Handler:           newGitStub(git, "/repos", password),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

func (s *gitStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/_calls" {
		s.callsMu.Lock()
		snapshot := append([]gitCall{}, s.calls...)
		s.callsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot)
		return
	}

	parts := repoPath.FindStringSubmatch(r.URL.Path)
	service := ""
	if parts != nil {
		switch {
		case parts[3] == "info/refs" && r.Method == http.MethodGet:
			service = r.URL.Query().Get("service")
		case parts[3] != "info/refs" && r.Method == http.MethodPost:
			service = parts[3]
		}
	}
	if service != "git-upload-pack" && service != "git-receive-pack" {
		http.NotFound(w, r)
		return
	}

	user, password, basic := r.BasicAuth()
	ok := basic && s.password != "" && subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) == 1
	call := gitCall{At: time.Now().UTC(), Path: r.URL.Path, Service: service, User: user, PasswordOK: ok}
	// A short or unrecognized password would be exposed by storing its tail.
	if ok && len(password) > 4 {
		call.PasswordTail = password[len(password)-4:]
	}
	s.callsMu.Lock()
	s.calls = append(s.calls, call)
	s.callsMu.Unlock()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="gitstub"`)
		http.Error(w, "Sign in to use git here.", http.StatusUnauthorized)
		return
	}
	if err := s.ensureRepo(r.Context(), parts[1], parts[2]); err != nil {
		log.Printf("Could not initialize the git repository: %v", err)
		http.Error(w, "The git repository could not be created.", http.StatusInternalServerError)
		return
	}

	// The backend needs to know the caller was authenticated, so pass its
	// username without forwarding the authentication header to the process.
	r = r.Clone(r.Context())
	r.Header.Del("Authorization")
	(&cgi.Handler{
		Path: s.git,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + s.root, "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=" + user},
	}).ServeHTTP(w, r)
}

func (s *gitStub) ensureRepo(ctx context.Context, owner, name string) error {
	s.reposMu.Lock()
	defer s.reposMu.Unlock()
	dir := filepath.Join(s.root, owner)
	repo := filepath.Join(dir, name)
	if _, err := os.Stat(repo); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Publish the repo only after it can accept a push, so the next request
	// can retry cleanly if initialization was interrupted.
	tmp, err := os.MkdirTemp(dir, ".repo-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := exec.CommandContext(ctx, s.git, "init", "--quiet", "--bare", "--initial-branch=main", tmp).Run(); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(tmp, "config"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	_, err = f.WriteString("[http]\n\treceivepack = true\n")
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Rename(tmp, repo)
}
