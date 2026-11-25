package serve

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const (
	gitPrefix     = "/v1/hosted/git/"
	gitRequestCap = 1 << 30

	gitSignIn           = "Sign in to use git here."
	gitNoSuchPlace      = "There is nothing at that address."
	gitHostDenied       = "This server does not reach that git host for chats."
	gitNoAccess         = "You have no GitHub access stored on this server yet. Add it, then try again."
	gitAccessUnreadable = "Your GitHub access could not be opened on this server."
	gitNotAnswering     = "That git host is not answering."
	gitAccessRejected   = "That git host did not accept your GitHub access."
	gitTooLarge         = "That request is too large for this server."
)

var gitName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func (h *Handler) gitPath(r *http.Request) bool {
	hc := h.config.Hosted
	return hc != nil && hc.state != nil && strings.HasPrefix(r.URL.Path, gitPrefix)
}

func (h *Handler) hostedGitRoutes() {
	h.read("/v1/hosted/audit", h.readHostedAudit)
}

type hostedGitRequest struct {
	host, repo, rest, service string
}

func parseHostedGitRequest(r *http.Request) (hostedGitRequest, bool) {
	rest := strings.TrimPrefix(r.URL.Path, gitPrefix)
	if strings.ContainsAny(rest, "\\\x00") || strings.Contains(rest, "..") ||
		strings.Contains(strings.ToLower(r.URL.EscapedPath()), "%2f") || path.Clean("/"+rest) != "/"+rest {
		return hostedGitRequest{}, false
	}
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) != 4 || parts[0] == "" {
		return hostedGitRequest{}, false
	}
	owner, repo := parts[1], strings.TrimSuffix(parts[2], ".git")
	if !gitName.MatchString(owner) || !gitName.MatchString(repo) {
		return hostedGitRequest{}, false
	}
	var service string
	switch parts[3] {
	case "info/refs":
		if r.Method != http.MethodGet {
			return hostedGitRequest{}, false
		}
		switch r.URL.RawQuery {
		case "service=git-upload-pack":
			service = "git-upload-pack"
		case "service=git-receive-pack":
			service = "git-receive-pack"
		default:
			return hostedGitRequest{}, false
		}
	case "git-upload-pack", "git-receive-pack":
		if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.URL.ForceQuery {
			return hostedGitRequest{}, false
		}
		service = parts[3]
	default:
		return hostedGitRequest{}, false
	}
	return hostedGitRequest{strings.ToLower(parts[0]), owner + "/" + repo, parts[3], service}, true
}

func (h *Handler) hostedGit(w http.ResponseWriter, r *http.Request) {
	hs := h.config.Hosted.state
	_, password, basic := r.BasicAuth()
	row, live := hs.bearers.lookup(password, h.clock())
	if !basic || !live {
		w.Header().Set("WWW-Authenticate", `Basic realm="codeaf"`)
		http.Error(w, gitSignIn, http.StatusUnauthorized)
		return
	}
	request, valid := parseHostedGitRequest(r)
	if !valid {
		http.Error(w, gitNoSuchPlace, http.StatusNotFound)
		return
	}
	var target *url.URL
	for _, origin := range hs.gitHosts {
		if u, err := url.Parse(origin); err == nil && u.Host == request.host {
			target = u
			break
		}
	}
	if target == nil {
		http.Error(w, gitHostDenied, http.StatusForbidden)
		return
	}
	key, _, err := h.hostedKeyLookup()(row.person, "github")
	if err != nil {
		http.Error(w, gitAccessUnreadable, http.StatusForbidden)
		return
	}
	if key == "" {
		http.Error(w, gitNoAccess, http.StatusForbidden)
		return
	}
	if r.ContentLength > gitRequestCap {
		http.Error(w, gitTooLarge, http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, gitRequestCap)
	target.Path = "/" + request.repo + ".git/" + request.rest
	target.RawPath = ""
	target.RawQuery = r.URL.RawQuery

	audit := sync.OnceFunc(func() {
		if request.rest == "info/refs" {
			h.hostedAudit(row.person, row.session, "git", request.host, request.repo, request.service)
		}
	})
	(&httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = target
			pr.Out.Host = ""
			// A short allow-list keeps organization credentials and addresses
			// out of requests to the git host.
			allowed := make(http.Header)
			for _, name := range []string{"Git-Protocol", "Content-Type", "Content-Encoding", "Accept", "Accept-Encoding", "User-Agent", "Content-Length"} {
				if values := pr.Out.Header.Values(name); len(values) > 0 {
					allowed[http.CanonicalHeaderKey(name)] = values
				}
			}
			pr.Out.Header = allowed
			pr.Out.Trailer = nil
			pr.Out.SetBasicAuth("x-access-token", key)
		},
		FlushInterval: -1,
		Transport:     hs.modelTransport,
		ErrorLog:      log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			audit()
			http.Error(w, gitNotAnswering, http.StatusBadGateway)
		},
		ModifyResponse: func(res *http.Response) error {
			if res.StatusCode >= 300 && res.StatusCode < 400 {
				return errors.New("upstream redirected git")
			}
			audit()
			res.Header.Del("Set-Cookie")
			if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
				res.Body.Close()
				body := gitAccessRejected + "\n"
				res.Body = io.NopCloser(strings.NewReader(body))
				res.ContentLength = int64(len(body))
				res.Header.Set("Content-Length", strconv.Itoa(len(body)))
				res.Header.Set("Content-Type", "text/plain; charset=utf-8")
				res.Header.Del("Content-Encoding")
				res.Trailer = nil
			}
			return nil
		},
	}).ServeHTTP(w, r)
}
