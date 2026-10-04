//go:build !windows

package tool

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/executor"
)

type ripgrepResult struct {
	stdout []byte
	stderr []byte
	code   int
}

type ripgrepRunner interface {
	Run(ctx context.Context, cwd string, args []string) (ripgrepResult, error)
}

type execRipgrepRunner struct{}

func (execRipgrepRunner) Run(ctx context.Context, cwd string, args []string) (ripgrepResult, error) {
	res, err := executor.For(ctx).In(cwd).Exec(ctx, executor.ExecRequest{
		Argv: append([]string{"rg"}, args...), Env: withoutEnv(os.Environ(), "RIPGREP_CONFIG_PATH"),
		Group: executor.GroupInherit,
	}, nil)
	if ctx.Err() != nil {
		return ripgrepResult{}, ctx.Err()
	}
	if err != nil {
		return ripgrepResult{}, err
	}
	return ripgrepResult{stdout: res.Stdout, stderr: res.Stderr, code: res.Exit}, nil
}

func withoutEnv(environment []string, name string) []string {
	prefix := name + "="
	out := make([]string, 0, len(environment))
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func ripgrepError(result ripgrepResult) error {
	message := strings.TrimSpace(string(result.stderr))
	if message == "" {
		message = "ripgrep failed with code " + itoa(result.code)
	}
	return errors.New(message)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		digits[index] = '-'
	}
	return string(digits[index:])
}
