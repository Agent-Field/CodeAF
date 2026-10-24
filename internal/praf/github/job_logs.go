package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// CheckRunLog reads the Actions job named by a check's canonical GitHub page.
// It follows GitHub's signed log redirect through the ordinary REST client.
func (c *client) CheckRunLog(ctx context.Context, owner, repo string, run CheckRun) (string, error) {
	u, err := url.Parse(run.URL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" {
		return "", fmt.Errorf("check has no GitHub Actions job log")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 7 || !strings.EqualFold(parts[0], owner) || !strings.EqualFold(parts[1], repo) || parts[2] != "actions" || parts[3] != "runs" || parts[5] != "job" {
		return "", fmt.Errorf("check has no GitHub Actions job log")
	}
	id, err := strconv.ParseInt(parts[6], 10, 64)
	if err != nil || id <= 0 {
		return "", fmt.Errorf("check has an invalid job id")
	}
	body, _, err := c.read(ctx, c.repoURL(owner, repo, "/actions/jobs/"+strconv.FormatInt(id, 10)+"/logs"), "")
	if err != nil {
		return "", err
	}
	return string(body), nil
}
