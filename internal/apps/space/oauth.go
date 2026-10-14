package space

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/oauth2"
)

var (
	googleEndpoint = oauth2.Endpoint{
		AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL: "https://oauth2.googleapis.com/token",
	}
	githubEndpoint = oauth2.Endpoint{
		AuthURL:  "https://github.com/login/oauth/authorize",
		TokenURL: "https://github.com/login/oauth/access_token",
	}
)

// Provider is one sign-in service. Google and GitHub are the two that ship.
type Provider interface {
	Name() string
	AuthCodeURL(state string) string
	Email(ctx context.Context, code string) (string, error)
}

type oauthProvider struct {
	name   string
	config *oauth2.Config
	email  func(ctx context.Context, client *http.Client) (string, error)
}

func (p oauthProvider) Name() string { return p.name }

func (p oauthProvider) AuthCodeURL(state string) string {
	return p.config.AuthCodeURL(state)
}

func (p oauthProvider) Email(ctx context.Context, code string) (string, error) {
	tok, err := p.config.Exchange(ctx, code)
	if err != nil {
		return "", err
	}
	return p.email(ctx, p.config.Client(ctx, tok))
}

// ProvidersFromEnv reads CODEAF_SPACE_GOOGLE_* and CODEAF_SPACE_GITHUB_*.
// A provider with no client id is left out, so the sign-in page cannot offer it.
func ProvidersFromEnv(redirectBase string) map[string]Provider {
	out := map[string]Provider{}
	if p := googleProvider(os.Getenv("CODEAF_SPACE_GOOGLE_CLIENT_ID"), os.Getenv("CODEAF_SPACE_GOOGLE_CLIENT_SECRET"), redirectBase); p != nil {
		out["google"] = p
	}
	if p := githubProvider(os.Getenv("CODEAF_SPACE_GITHUB_CLIENT_ID"), os.Getenv("CODEAF_SPACE_GITHUB_CLIENT_SECRET"), redirectBase); p != nil {
		out["github"] = p
	}
	return out
}

func googleProvider(id, secret, redirectBase string) Provider {
	if id == "" || secret == "" {
		return nil
	}
	return oauthProvider{
		name: "google",
		config: &oauth2.Config{
			ClientID: id, ClientSecret: secret,
			RedirectURL: strings.TrimRight(redirectBase, "/") + "/app/v1/space/signin/oauth/google/callback",
			Scopes:      []string{"openid", "email", "profile"},
			Endpoint:    googleEndpoint,
		},
		email: googleEmail,
	}
}

func githubProvider(id, secret, redirectBase string) Provider {
	if id == "" || secret == "" {
		return nil
	}
	return oauthProvider{
		name: "github",
		config: &oauth2.Config{
			ClientID: id, ClientSecret: secret,
			RedirectURL: strings.TrimRight(redirectBase, "/") + "/app/v1/space/signin/oauth/github/callback",
			Scopes:      []string{"user:email"},
			Endpoint:    githubEndpoint,
		},
		email: githubEmail,
	}
}

func googleEmail(ctx context.Context, client *http.Client) (string, error) {
	body, err := getJSON(ctx, client, "https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return "", err
	}
	var doc struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(doc.Email)), nil
}

func githubEmail(ctx context.Context, client *http.Client) (string, error) {
	body, err := getJSON(ctx, client, "https://api.github.com/user/emails")
	if err != nil {
		return "", err
	}
	var rows []struct {
		Email   string `json:"email"`
		Primary bool   `json:"primary"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return "", err
	}
	var fallback string
	for _, row := range rows {
		email := strings.ToLower(strings.TrimSpace(row.Email))
		if row.Primary && email != "" {
			return email, nil
		}
		if fallback == "" {
			fallback = email
		}
	}
	return fallback, nil
}

func getJSON(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, invalid("the sign-in service did not return an email")
	}
	return body, nil
}
