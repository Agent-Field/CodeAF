package serve

import (
	"io"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// githubVisibilityStatus keeps the headers that distinguish a spent limit
// from a repository denial, without retaining GitHub's response body.
func (h *Handler) githubVisibilityStatus(r *http.Request, endpoint, token string) (int, http.Header, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codeaf-factory")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := h.githubClient()
	response, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, response.Header, nil
}

func githubLimitSpent(status int, header http.Header) bool {
	return status == http.StatusTooManyRequests || status == http.StatusForbidden &&
		(header.Get("X-RateLimit-Remaining") == "0" || header.Get("Retry-After") != "")
}

// githubVisibilityLimited is used only when the caller named the repository. Job
// lookups must keep the same answer for hidden and missing jobs.
func (h *Handler) githubVisibilityLimited(w http.ResponseWriter, r *http.Request) bool {
	state, ok := r.Context().Value(authErrorKey{}).(*authErrorState)
	if !ok || state.githubRetryAt.IsZero() {
		return false
	}
	h.write(w, http.StatusTooManyRequests, struct {
		Factory api.Factory `json:"factory"`
		Error   string      `json:"error"`
		api.RepoIssuesRetry
	}{h.factory, "GitHub's rate limit is spent; retry after the reset", api.RepoIssuesRetry{RetryAt: state.githubRetryAt}})
	return true
}
