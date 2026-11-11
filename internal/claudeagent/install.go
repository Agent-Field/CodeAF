package claudeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

// EnvBinary names the Claude Code program to start in place of the first
// `claude` on PATH, for a machine whose `claude` is a wrapper or a shell
// function that a child process cannot reach.
const EnvBinary = "CODEAF_CLAUDE_BIN"

// Binary finds the Claude Code program: [EnvBinary] when it is set, else the
// first `claude` on PATH, else the place Claude Code's own installer puts it.
// The refusal says how to make it findable, because a person who has Claude
// Code installed and reads "not found" needs to know where codeaf looked.
//
// IT IS LOOKED FOR ON THE MACHINE THE ENGINE RUNS ON. A desk on a laptop that
// reaches an engine on another computer over the relay sees that computer's
// Claude Code, signed in there, because that is where every run starts.
func Binary() (string, error) {
	if named := strings.TrimSpace(env.Get(EnvBinary)); named != "" {
		if _, err := os.Stat(named); err != nil {
			return "", fmt.Errorf("%s names %s, which is not there", EnvBinary, named)
		}
		return named, nil
	}
	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		// An app launched from the Dock or a service manager does not inherit
		// the login shell's PATH, so the installer's own place is looked at too.
		for _, candidate := range []string{filepath.Join(home, ".local", "bin", "claude"), filepath.Join(home, ".claude", "local", "claude")} {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("Claude Code is not installed here: install it, run `claude` once to sign in, or set %s to its path", EnvBinary)
}

// Status is what `claude auth status` says about the sign-in, in the fields a
// settings row shows. Nothing that identifies the person is kept.
type Status struct {
	Installed bool
	Binary    string
	LoggedIn  bool
	// Method is how it is signed in: "claude.ai" for a Claude plan, or an API
	// key method that is billed per call.
	Method string
	// Plan is the plan word Claude Code reports, such as "max" or "pro".
	Plan string
	// Version is the version Claude Code reports, such as "2.1.4".
	Version string
}

// OnPlan says the sign-in is a Claude plan, which is not billed per call.
func (s Status) OnPlan() bool { return s.LoggedIn && s.Method == "claude.ai" }

// ReadStatus asks the installed Claude Code whether it is signed in. A missing
// program is not an error: it answers Installed false.
func ReadStatus(ctx context.Context) Status {
	binary, err := Binary()
	if err != nil {
		return Status{}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "auth", "status").Output()
	status := Status{Installed: true, Binary: binary}
	if raw, err := exec.CommandContext(ctx, binary, "--version").Output(); err == nil {
		// "2.1.4 (Claude Code)": the first word is the version.
		if fields := strings.Fields(string(raw)); len(fields) > 0 {
			status.Version = fields[0]
		}
	}
	if err != nil && len(out) == 0 {
		return status
	}
	var said struct {
		LoggedIn         bool   `json:"loggedIn"`
		AuthMethod       string `json:"authMethod"`
		SubscriptionType string `json:"subscriptionType"`
	}
	if json.Unmarshal(out, &said) == nil {
		status.LoggedIn, status.Method, status.Plan = said.LoggedIn, said.AuthMethod, said.SubscriptionType
	}
	return status
}

// Ask is one answer from Claude with no tools: a title, a summary, a check.
type Ask struct {
	Model  string
	System string
	Prompt string
}

// Answer is what one [Ask] came back with.
type Answer struct {
	Text string
	// InputTokens counts every token read, cached or not.
	InputTokens  int
	OutputTokens int
	// ListPriceUSD is what Claude Code says the call would cost at API prices.
	ListPriceUSD float64
	// Billed says the figure is a charge: the sign-in is an API key.
	Billed bool
}

// emptyDir is where a one-shot call runs, so that no CLAUDE.md of any folder
// is read into a title or a summary.
func emptyDir() string {
	dir := filepath.Join(os.TempDir(), "codeaf-claude-ask")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

// Once asks Claude one question through the installed Claude Code, with no
// tools, none of the person's settings, hooks or MCP servers, and nothing kept
// on disk afterwards.
//
// THIS IS FOR THE SMALL JOBS AROUND A CONVERSATION, NEVER ITS TOOL LOOP. A
// call starts the program afresh (about three seconds before the first word),
// and Claude Code will not hand back a tool call for tools it does not hold, so
// work that needs tools seats Claude Code as an agent ([Start]) instead.
func Once(ctx context.Context, ask Ask) (Answer, error) {
	binary, err := Binary()
	if err != nil {
		return Answer{}, err
	}
	args := []string{
		"-p", ask.Prompt, "--output-format", "json",
		"--tools", "", "--setting-sources", "", "--strict-mcp-config",
		"--no-session-persistence", "--disable-slash-commands",
	}
	if ask.System != "" {
		args = append(args, "--system-prompt", ask.System)
	}
	if ask.Model != "" {
		args = append(args, "--model", ask.Model)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = emptyDir()
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_ENTRYPOINT="+Entrypoint)
	out, runErr := cmd.Output()
	var said struct {
		Result       string  `json:"result"`
		IsError      bool    `json:"is_error"`
		Subtype      string  `json:"subtype"`
		TotalCostUSD float64 `json:"total_cost_usd"`
		Usage        struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			CacheReadTokens     int `json:"cache_read_input_tokens"`
			CacheCreationTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &said); err != nil {
		if runErr != nil {
			var exit *exec.ExitError
			if errors.As(runErr, &exit) && len(exit.Stderr) > 0 {
				return Answer{}, fmt.Errorf("Claude Code: %s", firstLine(string(exit.Stderr)))
			}
			return Answer{}, fmt.Errorf("Claude Code: %w", runErr)
		}
		return Answer{}, fmt.Errorf("Claude Code answered something that is not its JSON: %w", err)
	}
	if said.IsError {
		return Answer{}, fmt.Errorf("Claude Code: %s", firstLine(first(said.Result, said.Subtype)))
	}
	return Answer{
		Text:         said.Result,
		InputTokens:  said.Usage.InputTokens + said.Usage.CacheReadTokens + said.Usage.CacheCreationTokens,
		OutputTokens: said.Usage.OutputTokens,
		ListPriceUSD: said.TotalCostUSD,
	}, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Family answers the Claude Code model word (`opus`, `sonnet`, `haiku`) for a
// model id from any service's spelling of it, or "" for a model that is not
// Claude.
func Family(model string) string {
	lower := strings.ToLower(model)
	if !strings.Contains(lower, "claude") && !strings.HasPrefix(lower, "anthropic/") {
		return ""
	}
	for _, family := range []string{"opus", "sonnet", "haiku"} {
		if strings.Contains(lower, family) {
			return family
		}
	}
	return ""
}
