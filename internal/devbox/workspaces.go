package devbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const railwayGraphQLURL = "https://backboard.railway.com/graphql/v2"

type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var workspaceID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func resolveWorkspace(ctx context.Context, token, chosen string, lookup func(context.Context, string) ([]Workspace, error)) (string, error) {
	if workspaceID.MatchString(chosen) {
		return chosen, nil
	}
	workspaces, err := lookup(ctx, token)
	if err != nil {
		return "", err
	}
	if len(workspaces) == 0 {
		if chosen != "" {
			return "", errors.New("this token cannot look up workspace names; use --workspace with its id")
		}
		return "", nil
	}
	if chosen != "" {
		for _, w := range workspaces {
			if strings.EqualFold(w.Name, chosen) {
				return w.ID, nil
			}
		}
		return "", fmt.Errorf("no Railway workspace is called %q; use --workspace with a listed name or id", chosen)
	}
	if len(workspaces) == 1 {
		return workspaces[0].ID, nil
	}
	names := make([]string, len(workspaces))
	for i, w := range workspaces {
		names[i] = w.Name
	}
	return "", fmt.Errorf("choose a Railway workspace with --workspace <name-or-id>: %s", strings.Join(names, ", "))
}

func LookupWorkspaces(ctx context.Context, token string) ([]Workspace, error) {
	return lookupWorkspaces(ctx, &http.Client{Timeout: 20 * time.Second}, token)
}

// The client seam lets tests answer the public GraphQL request without a network.
func lookupWorkspaces(ctx context.Context, client *http.Client, token string) ([]Workspace, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, railwayGraphQLURL, bytes.NewBufferString(`{"query":"{ me { workspaces { id name } } }"}`))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("could not look up Railway workspaces; check the connection and token")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Railway workspace lookup failed (%d)", resp.StatusCode)
	}
	var body struct {
		Data struct {
			Me *struct {
				Workspaces []Workspace `json:"workspaces"`
			} `json:"me"`
		} `json:"data"`
		Errors []struct {
			Message string   `json:"message"`
			Path    []string `json:"path"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, errors.New("could not read Railway workspaces")
	}
	if len(body.Errors) > 0 {
		for _, e := range body.Errors {
			text := strings.ToLower(e.Message)
			if strings.Contains(text, "unauthor") || strings.Contains(text, "forbidden") || strings.Contains(text, "not authorized") || strings.Contains(text, "not authenticated") {
				return nil, nil
			}
		}
		return nil, errors.New("Railway could not list workspaces; use --workspace with its id")
	}
	if body.Data.Me == nil {
		return nil, nil
	}
	return body.Data.Me.Workspaces, nil
}
