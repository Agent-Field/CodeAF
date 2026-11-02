package devbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

const deploymentPollEvery = 3 * time.Second
const deploymentSettleWithin = 3 * time.Minute

func settleDeployments(ctx context.Context, endpoint, token, serviceID, environmentID string) error {
	if endpoint == "" {
		endpoint = railwayGraphQLURL
	}
	return pollDeployments(ctx, &http.Client{Timeout: 20 * time.Second}, endpoint, token, serviceID, environmentID, deploymentPollEvery, deploymentSettleWithin)
}

func pollDeployments(ctx context.Context, client *http.Client, endpoint, token, serviceID, environmentID string, every, within time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	body, _ := json.Marshal(struct {
		Query     string            `json:"query"`
		Variables map[string]string `json:"variables"`
	}{
		Query:     `query($s: String!, $e: String!) { deployments(first: 10, input: {serviceId: $s, environmentId: $e}) { edges { node { id status createdAt } } } }`,
		Variables: map[string]string{"s": serviceID, "e": environmentID},
	})
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return errors.New("Railway deployments did not settle within the wait")
		}
		settled, err := deploymentsSettled(ctx, client, endpoint, token, body)
		if err == nil && settled {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("Railway deployments did not settle within the wait")
		case <-ticker.C:
		}
	}
}

func deploymentsSettled(ctx context.Context, client *http.Client, endpoint, token string, body []byte) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return false, errors.New("could not check Railway deployments")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return false, errors.New("could not check Railway deployments")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, errors.New("could not check Railway deployments")
	}
	var response struct {
		Data struct {
			Deployments *struct {
				Edges []struct {
					Node struct {
						Status string `json:"status"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"deployments"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&response); err != nil || len(response.Errors) != 0 || response.Data.Deployments == nil {
		return false, errors.New("could not read Railway deployments")
	}
	for _, edge := range response.Data.Deployments.Edges {
		switch edge.Node.Status {
		case "WAITING", "QUEUED", "INITIALIZING", "BUILDING", "DEPLOYING", "REMOVING":
			return false, nil
		}
	}
	return true, nil
}
