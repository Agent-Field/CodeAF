package praf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// failureRecord is evidence of a run that produced no assembled review.
// It deliberately has no Saved.Program or review payload, so post rejects it.
type failureRecord struct {
	Kind          string  `json:"kind"`
	PullRequest   string  `json:"pull_request_url"`
	Stage         string  `json:"stage"`
	Reason        string  `json:"reason"`
	Failure       string  `json:"failure"`
	Elapsed       float64 `json:"elapsed_seconds"`
	CostUSD       float64 `json:"cost_usd"`
	Sessions      int     `json:"agent_sessions"`
	Calls         int     `json:"single_calls"`
	FailedSession int     `json:"failed_agent_sessions"`
	FailedCall    int     `json:"failed_single_calls"`
}

func writeFailureRecord(folder string, record failureRecord) ([]string, error) {
	if folder == "" {
		return nil, nil
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	var written []string
	for _, file := range []struct {
		name string
		body []byte
	}{
		{"review-failure.json", append(data, '\n')},
		{"review-failure.md", []byte(fmt.Sprintf("# Review did not finish\n\nPull request: %s\n\nStage: %s\n\n%s\n\nFailure: %s\n\nElapsed: %s; recorded spend: $%.4f.\n\nRecorded agent sessions: %d (%d failed); single calls: %d (%d failed). These counts and spend were recorded when the pipeline ended.\n\nNo assembled review was returned. This record cannot be posted as a review. Nothing was posted to GitHub.\n",
			record.PullRequest, record.Stage, record.Reason, record.Failure, (time.Duration(record.Elapsed * float64(time.Second))).Round(time.Millisecond),
			record.CostUSD, record.Sessions, record.FailedSession, record.Calls, record.FailedCall))},
	} {
		path := filepath.Join(folder, file.name)
		if err := os.WriteFile(path, file.body, 0o600); err != nil {
			return written, err
		}
		written = append(written, path)
	}
	return written, nil
}
