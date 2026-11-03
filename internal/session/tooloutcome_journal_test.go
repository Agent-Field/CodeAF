package session

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestToolOutcomeJournalPreservesProducerResult(t *testing.T) {
	_, path, _, _, _ := rejectionTurn(t, []rejectionCall{
		{id: "success", tool: "bash", args: `{"command":"printf 'PASS\\nCommand aborted\\n'"}`},
		{id: "failure", tool: "bash", args: `{"command":"exit 3"}`},
	})
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"success": "ok", "failure": "failed"}
	for _, line := range strings.Split(string(body), "\n") {
		var row struct {
			Role         string `json:"role"`
			Call         string `json:"toolCallId"`
			Presentation struct {
				Outcome string `json:"outcome"`
			} `json:"presentation"`
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if expected, ok := want[row.Call]; ok && row.Role == "tool" {
			if row.Presentation.Outcome != expected {
				t.Errorf("%s: outcome=%q want %q", row.Call, row.Presentation.Outcome, expected)
			}
			delete(want, row.Call)
		}
	}
	if len(want) != 0 {
		t.Fatal("missing tool results", want)
	}
}
