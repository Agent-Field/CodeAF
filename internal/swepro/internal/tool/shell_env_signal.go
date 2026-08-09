package tool

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/session/loopguard"
)

const (
	shellStallWindow = 30 * time.Second
	shellHeavyWindow = 30 * time.Second
)

type shellDeathInput struct {
	ExitCode         *int
	Expired          bool
	Aborted          bool
	OOMDelta         *int64
	MemoryLimitBytes *int64
	SinceLastOutput  *time.Duration
	Timeout          time.Duration
	CommandDuration  time.Duration
}

func readCgroupInt(path, key string) *int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := strings.TrimSpace(string(data))
	if key == "" {
		if text == "" || text == "max" {
			return nil
		}
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil || value <= 0 {
			return nil
		}
		return &value
	}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != key {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err == nil {
			return &value
		}
	}
	return nil
}

func readShellOOMCount() *int64 {
	return readCgroupInt("/sys/fs/cgroup/memory.events", "oom_kill")
}

func readShellMemoryLimit() *int64 {
	return readCgroupInt("/sys/fs/cgroup/memory.max", "")
}

func shellGB(bytes *int64) string {
	if bytes == nil || *bytes <= 0 {
		return "unknown"
	}
	return strconv.FormatFloat(float64(*bytes)/(1024*1024*1024), 'f', 1, 64) + "GB"
}

func shellSeconds(duration time.Duration) string {
	return strconv.FormatInt(int64(duration.Round(time.Second)/time.Second), 10) + "s"
}

func classifyShellDeath(input shellDeathInput) string {
	if input.Aborted {
		return ""
	}
	if input.OOMDelta != nil && *input.OOMDelta > 0 {
		return "[environment-signal] " + strconv.FormatInt(*input.OOMDelta, 10) +
			" process(es) were OOM-killed by the kernel during this command (container memory limit ≈ " +
			shellGB(input.MemoryLimitBytes) + "). This is an environment resource limit, not a code bug. " +
			"Do not rerun the same command unchanged — reduce its memory footprint (fewer parallel workers, " +
			"narrower scope) or verify with a cheaper command (e.g. a targeted test instead of a full build)."
	}
	if input.ExitCode != nil && *input.ExitCode == 137 && (input.OOMDelta == nil || *input.OOMDelta == 0) {
		return "[environment-signal] this command was killed by the system (SIGKILL / exit 137), likely memory pressure " +
			"or an external kill rather than a code bug. Do not blindly retry the same command — reduce its memory " +
			"footprint (fewer parallel workers, narrower scope) or verify with a cheaper command."
	}
	if input.Expired && input.SinceLastOutput != nil && *input.SinceLastOutput > shellStallWindow {
		return "[environment-signal] this command timed out after " + shellSeconds(input.Timeout) +
			" AND produced no output for the final " + shellSeconds(*input.SinceLastOutput) +
			" — it was stalled (hung, waiting on I/O, or resource-starved), so a larger timeout alone is unlikely " +
			"to help. Diagnose the hang or run a cheaper check instead of rerunning."
	}
	if input.Expired {
		return "[environment-signal] this command timed out while still producing output — it may simply need more time; " +
			"raise timeout only if this command is genuinely required, otherwise prefer a cheaper verification."
	}
	if input.ExitCode != nil && *input.ExitCode == 143 {
		return "[environment-signal] this command was terminated by SIGTERM (exit 143) — an external stop signal rather " +
			"than a normal exit. Check whether an orchestrator or timeout ended it before assuming a code failure."
	}
	return ""
}

var shellRepeatGuards = struct {
	sync.Mutex
	bySession map[string]loopguard.LoopGuard
}{bySession: map[string]loopguard.LoopGuard{}}

var shellCommandPlumbing = []*regexp.Regexp{
	regexp.MustCompile(`\s*2>&1\s*$`),
	regexp.MustCompile(`\s*\|\s*tail\b[^|]*$`),
	regexp.MustCompile(`\s*\|\s*head\b[^|]*$`),
}
var shellCommandWhitespace = regexp.MustCompile(`\s+`)

func normalizeShellCommand(command string) string {
	out := strings.TrimSpace(command)
	for {
		before := out
		for _, pattern := range shellCommandPlumbing {
			out = pattern.ReplaceAllString(out, "")
		}
		out = strings.TrimRight(out, " \t\r\n")
		if out == before {
			break
		}
	}
	return strings.TrimSpace(shellCommandWhitespace.ReplaceAllString(out, " "))
}

func registerShellOutcome(sessionID, command string, duration time.Duration, failed bool) string {
	if duration <= shellHeavyWindow && !failed {
		return ""
	}
	shellRepeatGuards.Lock()
	guard := shellRepeatGuards.bySession[sessionID]
	if guard == nil {
		cap := float64(3)
		guard = loopguard.CreateLoopGuard(loopguard.LoopGuardOptions{RepeatCap: &cap})
		shellRepeatGuards.bySession[sessionID] = guard
	}
	verdict := guard.Observe(loopguard.LoopAction{Tool: "shell", ArgsKey: normalizeShellCommand(command)})
	shellRepeatGuards.Unlock()
	if verdict.Status == loopguard.LoopStatusWarn || verdict.Status == loopguard.LoopStatusStop {
		return "[environment-signal] this is a repeated attempt of a failing/expensive command — repeating it unchanged " +
			"is unlikely to succeed; change the approach (narrower test, different diagnosis) instead."
	}
	return ""
}
