package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// JobWrites contains the two GitHub writes that a job decision owns. Requests
// never follow redirects with a token, and GitHub response bodies stay private.
type JobWrites struct {
	Token func(context.Context) string
	HTTP  *http.Client
}

func jobRepository(it factory.Item) (string, string, error) {
	full := it.Repo
	if !strings.Contains(full, "/") && it.Product != "" {
		full = it.Product + "/" + full
	}
	owner, repo, ok := strings.Cut(full, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", errors.New("job repository must be owner/name")
	}
	return url.PathEscape(owner), url.PathEscape(repo), nil
}

// Assign mirrors the assignee to GitHub. An empty handle gives the issue back.
func (j JobWrites) Assign(ctx context.Context, it factory.Item, handle string) error {
	if it.Origin != factory.OriginForge || it.Num <= 0 {
		return nil
	}
	owner, repo, err := jobRepository(it)
	if err != nil {
		return err
	}
	people := []string{}
	if handle != "" {
		people = append(people, factory.NormalizeHandle(handle))
	}
	return j.write(ctx, http.MethodPatch, "/repos/"+owner+"/"+repo+"/issues/"+strconv.Itoa(it.Num), struct {
		Assignees []string `json:"assignees"`
	}{people}, false)
}

// DeleteDroppedBranch removes only the factory branch after its retention date.
func (j JobWrites) DeleteDroppedBranch(ctx context.Context, it factory.Item, branch string, now time.Time) error {
	if it.DoneHow != "dropped" || it.BranchDeleteAt.IsZero() || now.Before(it.BranchDeleteAt) || !strings.HasPrefix(branch, "factory/"+strconv.Itoa(jobNumber(it))+"-") {
		return errors.New("dropped branch retention has not ended")
	}
	owner, repo, err := jobRepository(it)
	if err != nil {
		return err
	}
	return j.write(ctx, http.MethodDelete, "/repos/"+owner+"/"+repo+"/git/refs/heads/"+url.PathEscape(branch), nil, true)
}

func (j JobWrites) write(ctx context.Context, method, path string, body any, absentOK bool) error {
	token := ""
	if j.Token != nil {
		token = j.Token(ctx)
	}
	if token == "" {
		return errors.New("Reconnect GitHub to change this job")
	}
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Accept", "application/vnd.github+json")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", "codeaf-factory")
	client := http.Client{Timeout: 30 * time.Second}
	if j.HTTP != nil {
		client = *j.HTTP
	}
	if client.Timeout == 0 {
		client.Timeout = 30 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(r)
	if err != nil {
		return errors.New("GitHub isn't answering; retry this job change")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if absentOK && response.StatusCode == 404 {
		// GitHub also hides inaccessible repositories behind 404. Confirm
		// repository access before claiming its branch is already gone.
		repoPath := strings.SplitN(path, "/git/refs/", 2)[0]
		return j.write(ctx, http.MethodGet, repoPath, nil, false)
	}
	return fmt.Errorf("GitHub refused this job change (HTTP %d)", response.StatusCode)
}

func jobNumber(it factory.Item) int {
	if it.Num > 0 {
		return it.Num
	}
	return it.ID
}
