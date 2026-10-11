package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Contents is a file's bytes, blob identity and conditional-read tag. An empty
// ref asks GitHub for its default branch, so a job cannot supply its own rules.
func (c *client) Contents(ctx context.Context, owner, repo, path, ref, etag string) (text, sha, tag string, err error) {
	endpoint := c.repoURL(owner, repo, "/contents/"+path)
	// An omitted ref is GitHub's current default branch, never the job branch.
	if ref != "" {
		endpoint += "?ref=" + url.QueryEscape(ref)
	}
	body, tag, err := c.read(ctx, endpoint, etag)
	if err != nil {
		return "", "", tag, err
	}
	var file struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		SHA      string `json:"sha"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		return "", "", tag, err
	}
	if file.Type != "file" || file.Encoding != "base64" || file.SHA == "" {
		return "", "", tag, fmt.Errorf("%s is not a readable text file", path)
	}
	data, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil {
		return "", "", tag, err
	}
	if strings.ContainsRune(string(data), 0) {
		return "", "", tag, fmt.Errorf("%s is not text", path)
	}
	return string(data), file.SHA, tag, nil
}

// IsTransportError distinguishes an offline forge from a file or HTTP refusal.
func IsTransportError(err error) bool {
	var transport *transportError
	return errors.As(err, &transport)
}
