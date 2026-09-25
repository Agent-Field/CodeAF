package run_test

// The program a delegated run's REAL child runs: this test binary, started by
// the worker exactly as it starts codeaf's own executable — the program's line
// after it, the model API's address and token in its environment and no key —
// and marked by [delegateChildEnv] so its TestMain runs the program instead of
// the suite. It is how the worker is tested against a process that is really
// another process, speaking the records on a real pipe and calling the real
// model API over a real socket, rather than against a script that only
// pretends to.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

// delegateChildEnv marks a process started as a delegate's child.
const delegateChildEnv = "RUN_TEST_DELEGATE_CHILD"

// childProgram is the fake program: it says hello, asks the model API
// FAKE_CALLS questions — a step for each answer — and ends passing, or, with
// FAKE_ENDING=wait, waits to be told to stop and says it stopped. Its terminal
// claims a cost of its own that no bank may believe.
func childProgram() delegate.Delegate {
	name := os.Getenv("FAKE_PROGRAM_NAME")
	if name == "" {
		name = "fake"
	}
	return delegate.Delegate{
		Name: name, Summary: "a fake program", Default: "run", Page: name,
		Commands: []delegate.Command{{
			Name: "run", Usage: "[flags] -- <brief>", Summary: "does the whole task",
			Bind: func(*flag.FlagSet) delegate.Body { return childBody },
		}},
	}
}

func childBody(ctx context.Context, host delegate.Host, args []string) error {
	if path := os.Getenv("FAKE_API_FILE"); path != "" {
		api := host.Models()
		_ = os.WriteFile(path, []byte(api.BaseURL+"\n"+api.Token+"\n"), 0o600)
	}
	if path := os.Getenv("FAKE_ENV"); path != "" {
		_ = os.WriteFile(path, []byte(strings.Join(os.Environ(), "\n")), 0o600)
	}
	host.Hello([]string{"implement", "verify"})
	host.Stage(delegate.StageRecord{Stage: "implement", Status: "running"})
	calls, _ := strconv.Atoi(os.Getenv("FAKE_CALLS"))
	for call := 1; call <= calls; call++ {
		if ctx.Err() != nil {
			break
		}
		reply, err := askModel(ctx, host.Models(), fmt.Sprintf("call %d: %s", call, strings.Join(args, " ")))
		if err != nil {
			host.Step(delegate.StepRecord{Command: "model: ask", Observation: "refused: " + err.Error()})
			if os.Getenv("FAKE_ENDING") == "crash" {
				// senior-dev's own ending after a refusal: its sum of its
				// answers' costs never reached its ceiling, so it cannot tell a
				// ceiling from a broken road and says it crashed.
				host.Terminal(delegate.Ending{Status: delegate.StatusCrashed, Message: "the model road refused a call"})
				return nil
			}
			continue
		}
		host.Step(delegate.StepRecord{Command: "model: ask", Observation: reply})
	}
	if os.Getenv("FAKE_ENDING") == "wait" || ctx.Err() != nil {
		<-ctx.Done()
		host.Terminal(delegate.Ending{Status: delegate.StatusBudget, Message: "told to stop", CostUSD: 99})
		return nil
	}
	host.Stage(delegate.StageRecord{Stage: "verify", Status: "pass"})
	host.Terminal(delegate.Ending{Status: delegate.StatusPass, Message: "submitted and verified", Claim: "all green", Observed: "pass", CostUSD: 99})
	return nil
}

// askModel is one call through the model API, the way any OpenAI client
// makes one: the route joined to the base, the bearer token, one question,
// the answer's words back — or the API's own refusal as the error.
func askModel(ctx context.Context, api delegate.ModelAPI, question string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":    "deepseek/deepseek-v4-flash-0731",
		"messages": []map[string]string{{"role": "system", "content": "be brief"}, {"role": "user", "content": question}},
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, modelapi.ChatURL(api.BaseURL), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	api.Authorize(request)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	var answer struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(payload), &answer); err != nil {
		return "", fmt.Errorf("%d: %s", response.StatusCode, payload)
	}
	if answer.Error != nil {
		return "", fmt.Errorf("%d: %s", answer.Error.Code, answer.Error.Message)
	}
	if len(answer.Choices) == 0 {
		return "", errors.New("no choices")
	}
	return answer.Choices[0].Message.Content, nil
}

// runAsDelegateChild runs the fake program when this binary was started as a
// delegate's child, and says whether it was.
func runAsDelegateChild() (int, bool) {
	if os.Getenv(delegateChildEnv) != "1" {
		return 0, false
	}
	program := childProgram()
	if len(os.Args) < 2 || os.Args[1] != program.Name {
		fmt.Fprintf(os.Stderr, "started as a delegate's child with %q\n", os.Args)
		return 3, true
	}
	inv, err := delegate.Parse(program, os.Args[2:], os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1, true
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if delegate.RunChild(ctx, inv, os.Stdout) == delegate.StatusPass {
		return 0, true
	}
	return 2, true
}
