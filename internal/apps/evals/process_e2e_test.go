//go:build e2e

package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

type appProcess struct {
	binary  string
	home    string
	env     []string
	cmd     *exec.Cmd
	in      io.WriteCloser
	out     <-chan wireReply
	id      int
	onOffer bool
}

type wireReply struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *appwire.Error  `json:"error"`
	Event  string          `json:"event"`
}

func launch(ctx context.Context, binary, scratch, key, model string) (*appProcess, error) {
	// The detached chat clock can outlive this disposable home. App runs use
	// the engine's own dispatcher, which still starts and joins normally.
	env := []string{"HOME=" + scratch, "CODEAF_HOME=" + scratch, "PATH=" + os.Getenv("PATH"), "CODEAF_NO_AUTOMATIONS=1"}
	if key != "" {
		env = append(env, "OPENROUTER_API_KEY="+key, "CODEAF_APPS_BUILDER_MODEL="+model)
	}
	return launchWithEnv(ctx, binary, scratch, env)
}

func launchWithEnv(ctx context.Context, binary, scratch string, env []string) (*appProcess, error) {
	cmd := exec.CommandContext(ctx, binary, "app")
	cmd.Dir, cmd.Env = scratch, env
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	replies := make(chan wireReply, 16)
	go func() {
		defer close(replies)
		dec := json.NewDecoder(stdout)
		for {
			var reply wireReply
			if dec.Decode(&reply) != nil {
				return
			}
			if reply.Event != "" && reply.Event != "hello" {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case replies <- reply:
			}
		}
	}()
	select {
	case reply := <-replies:
		if reply.Event == "hello" {
			return &appProcess{binary: binary, home: scratch, env: env, cmd: cmd, in: in, out: replies}, nil
		}
	case <-ctx.Done():
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return nil, errors.New("app engine did not send hello")
}

func (p *appProcess) Close() error {
	if p.cmd == nil {
		return nil
	}
	_ = p.in.Close()
	err := p.cmd.Wait()
	p.cmd = nil
	return err
}

func (p *appProcess) wire(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if p.cmd == nil {
		return nil, errors.New("app engine is closed")
	}
	p.id++
	request := map[string]any{"id": p.id, "method": method, "params": params}
	if err := json.NewEncoder(p.in).Encode(request); err != nil {
		return nil, fmt.Errorf("%s: write to app engine: %w", method, err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case reply, ok := <-p.out:
			if !ok {
				return nil, errors.New("app engine closed its reply pipe")
			}
			if reply.ID != p.id {
				continue
			}
			if reply.Error != nil {
				return nil, CodeError(reply.Error.Code)
			}
			return reply.Result, nil
		}
	}
}

func (p *appProcess) cli(ctx context.Context, verb string, params any) (json.RawMessage, error) {
	args, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, p.binary, "app", verb, "-")
	cmd.Env, cmd.Dir, cmd.Stdin = p.env, p.home, bytes.NewReader(args)
	out, err := cmd.Output()
	if err != nil {
		// Engine logs may include provider details. Report only the exit status.
		return nil, fmt.Errorf("codeaf app %s: %w", verb, err)
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("codeaf app %s returned no JSON", verb)
	}
	return out, nil
}

func (p *appProcess) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if verb, ok := strings.CutPrefix(method, "app."); ok {
		return p.cli(ctx, verb, params)
	}
	if method == "build-app" {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		var args struct {
			Prompt string
			Reuse  []string
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		state, err := p.build(ctx, args.Prompt, args.Reuse, func(BuildState) error { return nil })
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"app": state.App})
	}
	if method == "wait-and-apply" {
		_, err := p.waitCheck(ctx, params)
		if err != nil {
			return nil, err
		}
		return p.wire(ctx, "apps.apply", params)
	}
	return p.wire(ctx, method, params)
}

type draftReply struct {
	Draft  string `json:"draft"`
	Offers []struct {
		Type  string `json:"type"`
		Count int    `json:"count"`
		App   string `json:"app"`
	} `json:"offers"`
}

func readReply[T any](body json.RawMessage, err error) (T, error) {
	var v T
	if err == nil {
		err = json.Unmarshal(body, &v)
	}
	return v, err
}

func (p *appProcess) Build(ctx context.Context, prompt string, _ bool, onPlan func(BuildState) error) (BuildState, error) {
	var reuse []string
	if p.onOffer {
		reuse = []string{"contact"}
	}
	return p.build(ctx, prompt, reuse, onPlan)
}

func (p *appProcess) build(ctx context.Context, prompt string, reuse []string, onPlan func(BuildState) error) (BuildState, error) {
	body, err := p.cli(ctx, "plan", map[string]string{"ask": prompt})
	plan, err := readReply[draftReply](body, err)
	if err != nil {
		return BuildState{}, err
	}
	if plan.Draft == "" {
		return BuildState{}, errors.New("app plan did not return a draft")
	}
	if err := onPlan(BuildState{Plan: body}); err != nil {
		return BuildState{}, err
	}
	// Check the proposal without sending a separate reuse acceptance.
	for _, typ := range reuse {
		found := false
		for _, offer := range plan.Offers {
			if offer.Type == typ && offer.App != "" {
				found = true
				break
			}
		}
		if !found {
			return BuildState{}, fmt.Errorf("plan did not offer existing %s records", typ)
		}
	}
	params := map[string]string{"draft": plan.Draft}
	if _, err := p.cli(ctx, "build", params); err != nil {
		return BuildState{}, err
	}
	state, err := p.waitCheck(ctx, params)
	if err != nil {
		return state, err
	}
	applied, err := readReply[struct {
		Meta struct {
			ID string `json:"id"`
		} `json:"meta"`
	}](p.wire(ctx, "apps.apply", params))
	state.App = applied.Meta.ID
	return state, err
}

var recipeGap = regexp.MustCompile(`^recipe_(unavailable|unsupported|gap|not_found)$`)

func (p *appProcess) waitCheck(ctx context.Context, params any) (BuildState, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var result BuildState
	for {
		state, err := readReply[struct {
			State    string `json:"state"`
			Problem  string `json:"problem"`
			Progress []struct {
				Round  int   `json:"round"`
				OK     *bool `json:"ok"`
				Report struct {
					Problems []struct {
						Code string `json:"code"`
						Path string `json:"path"`
					} `json:"problems"`
				} `json:"report"`
			} `json:"progress"`
		}](p.cli(ctx, "build_state", params))
		if err != nil {
			return result, err
		}
		for _, event := range state.Progress {
			result.Rounds = max(result.Rounds, event.Round)
			if event.OK != nil {
				result.Checked = *event.OK
			}
			for _, problem := range event.Report.Problems {
				if recipeGap.MatchString(problem.Code) {
					gap := strings.TrimSpace(problem.Code + " " + problem.Path)
					if !slices.Contains(result.RecipeGaps, gap) {
						result.RecipeGaps = append(result.RecipeGaps, gap)
					}
				}
			}
		}
		switch state.State {
		case "failed", "needs_you":
			return result, fmt.Errorf("%s: %s", state.State, state.Problem)
		case "ready":
			if result.Checked {
				return result, nil
			}
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *appProcess) Usage(ctx context.Context) (Usage, error) {
	result, err := readReply[struct{ Totals Usage }](p.wire(ctx, "spend.calls.export", map[string]any{
		"from":        time.Now().AddDate(0, 0, -1).Format(time.DateOnly),
		"to":          time.Now().AddDate(0, 0, 1).Format(time.DateOnly),
		"destination": filepath.Join(p.home, "eval-usage.csv"),
	}))
	return result.Totals, err
}
