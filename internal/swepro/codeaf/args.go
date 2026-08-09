// This file ports the CLI surface from swe-pro/src/cli/cmd/run.ts:151-365,
// resume.ts:345-429, and portfolio.ts:238-280.
package codeaf

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	defaultHighModels = "openrouter/deepseek/deepseek-v4-flash-0731,openrouter/deepseek/deepseek-v4-pro,openrouter/qwen/qwen3.6-plus,openrouter/moonshotai/kimi-k2.6,openrouter/z-ai/glm-5.1,openrouter/minimax/minimax-m2.7"
	defaultLowModels  = "openrouter/deepseek/deepseek-v4-flash-0731,openrouter/qwen/qwen3.6-flash,openrouter/qwen/qwen3-coder-30b-a3b-instruct"
)

type cliArgs struct {
	Command    string
	Message    string
	Directory  string
	High       string
	Low        string
	Frontier   string
	Variant    string
	Format     string
	EntryAgent string
	TUI        bool
	PRReady    bool
	Hard       bool
	Blast      bool
	Attempts   float64
	Reconcile  bool
	MaxCost    *float64
	MaxHours   *float64
	Help       bool
	Version    bool
}

func parseArgs(argv []string) (cliArgs, error) {
	out := cliArgs{
		Command: "run", High: defaultHighModels, Low: defaultLowModels,
		Variant: "high", Format: "json", EntryAgent: "root-orchestrator",
	}
	if len(argv) > 0 && !strings.HasPrefix(argv[0], "-") {
		switch argv[0] {
		case "run", "resume", "portfolio", "arch", "review", "help", "version":
			out.Command = argv[0]
			argv = argv[1:]
		}
	}
	if out.Command == "portfolio" {
		out.EntryAgent = ""
		out.Hard = true
		out.Attempts = 3
		out.Reconcile = true
	}
	message := []string{}
	for index := 0; index < len(argv); index++ {
		arg := argv[index]
		if arg == "--" {
			message = append(message, argv[index+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			message = append(message, arg)
			continue
		}
		name, inline, hasInline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		switch name {
		case "entryAgent":
			name = "entry-agent"
		case "prReady":
			name = "pr-ready"
		case "noReconcile":
			name = "no-reconcile"
		}
		value := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			if index+1 >= len(argv) {
				return "", fmt.Errorf("--%s requires a value", name)
			}
			index++
			return argv[index], nil
		}
		boolean := func(defaultValue bool) (bool, error) {
			if hasInline {
				return inline == "true", nil
			}
			if strings.HasPrefix(name, "no-") {
				return false, nil
			}
			if index+1 < len(argv) && (argv[index+1] == "true" || argv[index+1] == "false") {
				index++
				return argv[index] == "true", nil
			}
			return defaultValue, nil
		}
		if out.Command == "portfolio" && hasInline && strings.HasPrefix(name, "no-") {
			camel := strings.ReplaceAll(name, "-", " ")
			parts := strings.Fields(camel)
			camel = parts[0]
			for _, part := range parts[1:] {
				camel += strings.ToUpper(part[:1]) + part[1:]
			}
			return out, fmt.Errorf("Unknown arguments: %s, %s", name, camel)
		}
		switch name {
		case "h", "help":
			out.Help = true
		case "version":
			out.Version = true
		case "tui":
			if out.Command == "portfolio" {
				return out, errors.New("Unknown argument: tui")
			}
			out.TUI = true
		case "pr-ready":
			parsed, err := boolean(true)
			if err != nil {
				return out, err
			}
			out.PRReady = parsed
		case "no-pr-ready":
			parsed, err := boolean(false)
			if err != nil {
				return out, err
			}
			out.PRReady = parsed
		case "hard":
			parsed, err := boolean(true)
			if err != nil {
				return out, err
			}
			out.Hard = parsed
		case "no-hard":
			parsed, err := boolean(false)
			if err != nil {
				return out, err
			}
			out.Hard = parsed
		case "blast", "no-blast":
			if out.Command != "portfolio" {
				return out, fmt.Errorf("unknown option: %s", arg)
			}
			parsed, err := boolean(name == "blast")
			if err != nil {
				return out, err
			}
			out.Blast = parsed
		case "reconcile", "no-reconcile":
			if out.Command != "portfolio" {
				return out, fmt.Errorf("unknown option: %s", arg)
			}
			parsed, err := boolean(name == "reconcile")
			if err != nil {
				return out, err
			}
			out.Reconcile = parsed
		case "dir", "high", "low", "frontier", "variant", "format", "entry-agent":
			if out.Command == "portfolio" && name == "frontier" {
				return out, errors.New("Unknown argument: frontier")
			}
			raw, err := value()
			if err != nil {
				return out, err
			}
			switch name {
			case "dir":
				out.Directory = raw
			case "high":
				out.High = raw
			case "low":
				out.Low = raw
			case "frontier":
				out.Frontier = raw
			case "variant":
				out.Variant = raw
			case "format":
				out.Format = raw
			case "entry-agent":
				out.EntryAgent = raw
			}
		case "attempts":
			if out.Command != "portfolio" {
				return out, fmt.Errorf("unknown option: %s", arg)
			}
			if !hasInline && index+1 >= len(argv) {
				return out, errors.New("Not enough arguments following: attempts")
			}
			raw, err := value()
			if err != nil {
				return out, err
			}
			number := 0.0
			if raw != "" {
				var parseErr error
				number, parseErr = strconv.ParseFloat(raw, 64)
				if raw == "Infinity" {
					number = math.Inf(1)
				} else if raw == "-Infinity" {
					number = math.Inf(-1)
				} else if parseErr != nil {
					number = math.NaN()
				}
			}
			out.Attempts = number
		case "max-cost", "max-hours":
			if out.Command == "portfolio" {
				return out, fmt.Errorf("Unknown argument: %s", name)
			}
			raw, err := value()
			if err != nil {
				return out, err
			}
			number, err := strconv.ParseFloat(raw, 64)
			if err != nil || number < 0 {
				return out, fmt.Errorf("--%s must be a non-negative number", name)
			}
			if name == "max-cost" {
				out.MaxCost = &number
			} else {
				out.MaxHours = &number
			}
		default:
			if out.Command == "portfolio" {
				return out, fmt.Errorf("Unknown argument: %s", name)
			}
			return out, fmt.Errorf("unknown option: %s", arg)
		}
	}
	out.Message = strings.TrimSpace(strings.Join(message, " "))
	if out.Format != "default" && out.Format != "json" {
		if out.Command == "portfolio" {
			return out, fmt.Errorf(
				"Invalid values:\n  Argument: format, Given: %q, Choices: \"default\", \"json\"",
				out.Format,
			)
		}
		return out, errors.New("--format must be default or json")
	}
	return out, nil
}

func splitPool(raw string) []string {
	out := []string{}
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func usage() string {
	return `codeaf — autonomous coding pipeline

Usage:
  codeaf run [options] "<goal>"
  codeaf resume [options] ["<goal override>"]
  codeaf portfolio [options] "<goal>"

  run and resume require a reachable AgentField control plane; codeaf
  cannot be used standalone. The control plane is resolved from
  CODEAF_CP_URL, then AGENTFIELD_URL, then http://localhost:8080.

  codeaf serve      expose this binary as an AgentField node (reasoners
                    code_task / code_resume). Env: AGENTFIELD_URL (required),
                    AGENT_NODE_ID, AGENT_LISTEN_ADDR, AGENT_PUBLIC_URL,
                    AGENTFIELD_TOKEN

Options:
  --dir PATH          workspace (default: current directory)
  --high MODELS       comma-separated high-tier model pool
  --low MODELS        comma-separated low-tier model pool
  --frontier MODELS   optional frontier model pool
  --variant NAME      provider reasoning variant (default: high)
  --format MODE       default or json; both preserve NDJSON events
  --entry-agent NAME  entry agent (default: root-orchestrator)
	  --tui               unsupported in Go; exits non-zero (use headless NDJSON)
  --pr-ready          run PR-readiness after an auditor pass
  --hard              decompose medium roots and use stricter verification
  --max-cost USD      cumulative run cost ceiling
  --max-hours HOURS   cumulative wall-clock ceiling

Portfolio options:
  --blast             launch every attempt concurrently
  --attempts N        independent attempts (clamped to 1-8; default: 3)
  --reconcile         reconcile unique loser coverage (default: true)
  --no-reconcile      skip reconciliation
`
}
