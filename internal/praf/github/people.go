package github

import (
	"context"
	"encoding/json"
	"net/url"
)

// UserProfile is public GitHub identity, including outside issue authors.
type UserProfile struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

// User conditionally reads a public profile by handle, never /user membership.
func (c *client) User(ctx context.Context, handle, etag string) (UserProfile, string, error) {
	body, tag, err := c.read(ctx, c.baseURL+"/users/"+url.PathEscape(handle), etag)
	var profile UserProfile
	if err == nil {
		err = json.Unmarshal(body, &profile)
	}
	return profile, tag, err
}
