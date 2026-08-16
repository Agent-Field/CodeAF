package projectors

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type fixtureEvent struct {
	Now  int64           `json:"now"`
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()
	out := []fixture{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var item fixture
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		out = append(out, item)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 38 {
		t.Fatalf("expected at least 38 fixtures, got %d", len(fixtures))
	}
	for _, item := range fixtures {
		t.Run(item.Fn+"/"+item.Name, func(t *testing.T) {
			got := callFixture(t, item)
			encoded, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != item.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", item.ArgsJSON, encoded, item.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]int{}
	for _, item := range loadFixtures(t) {
		seen[item.Fn]++
	}
	for _, name := range []string{"toPartialRow", "isBusyError", "busyRetryScenario", "projectSequence"} {
		if seen[name] == 0 {
			t.Errorf("no fixtures for %s", name)
		}
	}
}

func callFixture(t *testing.T, item fixture) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(item.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	switch item.Fn {
	case "toPartialRow":
		if len(args) != 1 {
			t.Fatalf("toPartialRow args = %d", len(args))
		}
		row, err := ToPartialRow(args[0])
		if err != nil {
			t.Fatal(err)
		}
		return row
	case "isBusyError":
		if len(args) < 1 || len(args) > 2 {
			t.Fatalf("isBusyError args = %d", len(args))
		}
		if string(args[0]) == "null" || (len(args[0]) > 0 && args[0][0] == '"') {
			return false
		}
		var input fixtureSQLiteError
		if err := json.Unmarshal(args[0], &input); err != nil {
			t.Fatal(err)
		}
		if len(args) == 2 {
			var depth int
			if err := json.Unmarshal(args[1], &depth); err != nil {
				t.Fatal(err)
			}
			return IsBusyError(&input, depth)
		}
		return IsBusyError(&input)
	case "busyRetryScenario":
		if len(args) != 1 {
			t.Fatalf("busyRetryScenario args = %d", len(args))
		}
		var input retryFixtureInput
		if err := json.Unmarshal(args[0], &input); err != nil {
			t.Fatal(err)
		}
		return runRetryFixture(input)
	case "projectSequence":
		if len(args) != 1 {
			t.Fatalf("projectSequence args = %d", len(args))
		}
		var input struct {
			Events []fixtureEvent `json:"events"`
		}
		if err := json.Unmarshal(args[0], &input); err != nil {
			t.Fatal(err)
		}
		return replayFixtureEvents(t, input.Events)
	default:
		t.Fatalf("unknown fixture function %q", item.Fn)
		return nil
	}
}

type fixtureSQLiteError struct {
	Name    string              `json:"code"`
	Errno   int                 `json:"errno"`
	Message string              `json:"message"`
	Cause   *fixtureSQLiteError `json:"cause"`
}

func (e *fixtureSQLiteError) Error() string    { return e.Message }
func (e *fixtureSQLiteError) Code() int        { return e.Errno }
func (e *fixtureSQLiteError) CodeName() string { return e.Name }
func (e *fixtureSQLiteError) Unwrap() error {
	if e.Cause == nil {
		return nil
	}
	return e.Cause
}

type retryFixtureInput struct {
	Failures    int                 `json:"failures"`
	Error       *fixtureSQLiteError `json:"error"`
	Result      any                 `json:"result"`
	MaxAttempts *int                `json:"maxAttempts"`
	BaseDelayMS *int                `json:"baseDelayMs"`
	MaxDelayMS  *int                `json:"maxDelayMs"`
	Random      *float64            `json:"random"`
	DBPath      string              `json:"dbPath"`
}

func runRetryFixture(input retryFixtureInput) any {
	calls := 0
	sleeps := []int{}
	var logs bytes.Buffer
	random := 0.5
	if input.Random != nil {
		random = *input.Random
	}
	value, retryErr := WithBusyRetry(func() (any, error) {
		calls++
		if calls <= input.Failures {
			return nil, input.Error
		}
		return input.Result, nil
	}, BusyRetryOptions{
		MaxAttempts: input.MaxAttempts,
		BaseDelayMS: input.BaseDelayMS,
		MaxDelayMS:  input.MaxDelayMS,
		DBPath:      input.DBPath,
		Random:      func() float64 { return random },
		Sleep: func(delay time.Duration) {
			sleeps = append(sleeps, int(delay/time.Millisecond))
		},
		Log: &logs,
	})
	logLines := []string{}
	if text := strings.TrimSuffix(logs.String(), "\n"); text != "" {
		logLines = strings.Split(text, "\n")
	}
	var errorMessage any
	var causeCode any
	if retryErr != nil {
		errorMessage = retryErr.Error()
		var named namedCodeError
		if errors.As(errors.Unwrap(retryErr), &named) {
			causeCode = named.CodeName()
		}
	}
	return struct {
		Value     any      `json:"value"`
		Calls     int      `json:"calls"`
		Sleeps    []int    `json:"sleeps"`
		Logs      []string `json:"logs"`
		Error     any      `json:"error"`
		CauseCode any      `json:"causeCode"`
	}{
		Value:     value,
		Calls:     calls,
		Sleeps:    sleeps,
		Logs:      logLines,
		Error:     errorMessage,
		CauseCode: causeCode,
	}
}

func replayFixtureEvents(t *testing.T, events []fixtureEvent) any {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, ":memory:", BusyRetryOptions{Log: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE project (id text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO project (id) VALUES ('p1'), ('p2')`); err != nil {
		t.Fatal(err)
	}

	now := int64(0)
	store := NewStore(db, StoreOptions{Now: func() int64 { return now }})
	var projectErr error
	for _, item := range events {
		now = item.Now
		projectErr = store.Apply(ctx, Event{ID: item.ID, Type: item.Type, Data: item.Data})
		if projectErr != nil {
			break
		}
	}
	state, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if projectErr != nil {
		return struct {
			Error string   `json:"error"`
			State Snapshot `json:"state"`
		}{Error: projectErr.Error(), State: state}
	}
	return state
}
