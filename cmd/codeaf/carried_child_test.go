package main

// The program this package's tests carry, and the door a shell run's REAL
// child comes in by: carried_host_test.go starts this very test binary as the
// program's process, exactly as a shell run starts codeaf's own executable —
// the program's line after it, the model API's address and token in its
// environment and no key — marked by [carriedChildEnv], and TestMain then runs
// the whole dispatch (`execute`) with the fake program on the build's list.

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
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

// carriedChildEnv marks a process started as a shell run's child.
const carriedChildEnv = "CODEAF_TEST_CARRIED_CHILD"

// fakeCarried is the fake program's name. It is a word no verb of codeaf's
// own is spelled with, which carried_test.go's collision law holds it to.
const fakeCarried = "fake-carried"

// fakeCarriedProgram is a program with a command flag of its own: `--calls`
// questions to the model API, a step for each answer, and a passing ending —
// or, with `--wait`, it waits to be stopped and says it was.
func fakeCarriedProgram() delegate.Delegate {
	return delegate.Delegate{
		Name: fakeCarried, Summary: "a program the tests carry, which asks its model a question or two", Default: "run", Page: "delegates",
		Commands: []delegate.Command{{
			Name: "run", Usage: "[flags] -- <brief>", Summary: "does the whole task",
			Bind: func(fs *flag.FlagSet) delegate.Body {
				calls := fs.Int("calls", 1, "how many questions to ask the model")
				wait := fs.Bool("wait", false, "wait to be stopped after the questions")
				return func(ctx context.Context, host delegate.Host, args []string) error {
					host.Hello([]string{"implement", "verify"})
					host.Stage("implement", "running")
					for call := 1; call <= *calls && ctx.Err() == nil; call++ {
						reply, err := askCarried(ctx, host.Models(), fmt.Sprintf("question %d: %s", call, strings.Join(args, " ")))
						if err != nil {
							host.Step("model: ask", "refused: "+err.Error())
							continue
						}
						host.Step("model: ask", reply)
					}
					if *wait || ctx.Err() != nil {
						<-ctx.Done()
						host.Terminal(delegate.Ending{Status: delegate.StatusFail, Message: "stopped before it finished"})
						return nil
					}
					host.Stage("verify", "pass")
					host.Terminal(delegate.Ending{Status: delegate.StatusPass, Message: "submitted and verified", Claim: "the test is fixed", Observed: "pass"})
					return nil
				}
			},
		}, {
			Name: "check", Usage: "", Summary: "says whether it could run",
			Bind: func(*flag.FlagSet) delegate.Body {
				return func(ctx context.Context, host delegate.Host, args []string) error {
					host.Terminal(delegate.Ending{Status: delegate.StatusPass, Message: "it could run"})
					return nil
				}
			},
		}},
	}
}

// askCarried is one question through the model API, as any OpenAI client asks
// one.
func askCarried(ctx context.Context, api delegate.ModelAPI, question string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":    "openrouter/deepseek/deepseek-v4-flash-0731",
		"messages": []map[string]string{{"role": "user", "content": question}},
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
	payload, _ := io.ReadAll(response.Body)
	var answer struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(payload), &answer); err != nil {
		return "", fmt.Errorf("%d: %s", response.StatusCode, payload)
	}
	if answer.Error != nil {
		return "", errors.New(answer.Error.Message)
	}
	if len(answer.Choices) == 0 {
		return "", errors.New("no answer")
	}
	return answer.Choices[0].Message.Content, nil
}

// runAsCarriedChild runs the dispatch when this binary was started as a shell
// run's child, and says whether it was: with the fake program carried when
// the mark is "1", and with the build's own list — senior-dev itself — when it
// is "real".
func runAsCarriedChild() (int, bool) {
	switch os.Getenv(carriedChildEnv) {
	case "1":
		restore := builtin.Override([]delegate.Delegate{fakeCarriedProgram()})
		defer restore()
		return execute(), true
	case "real":
		return execute(), true
	}
	return 0, false
}
