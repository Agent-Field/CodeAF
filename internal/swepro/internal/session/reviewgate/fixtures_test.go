// This file replays golden outputs generated from
// src/session/review-gate.ts:56-1481,
// src/session/review-synthesizer.ts:1-161,
// src/session/retry-advisor.ts:1-112, and
// src/session/issue-advisor.ts:1-141.
package reviewgate

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixtureLine struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixtureLine {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	out := []fixtureLine{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		var line fixtureLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		out = append(out, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func fixtureArgs(t *testing.T, line fixtureLine) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(line.ArgsJSON), &args); err != nil {
		t.Fatalf("%s/%s args: %v", line.Fn, line.Name, err)
	}
	return args
}

func fixtureCall(t *testing.T, line fixtureLine) any {
	t.Helper()
	args := fixtureArgs(t, line)
	switch line.Fn {
	case "readContract":
		var task *struct {
			Description *string `json:"description"`
		}
		if err := json.Unmarshal(args[0], &task); err != nil {
			t.Fatal(err)
		}
		description := ""
		if task != nil && task.Description != nil {
			description = *task.Description
		}
		return ReadContract(description)

	case "stripPolicyLines":
		var input string
		mustUnmarshal(t, args[0], &input)
		return StripPolicyLines(input)

	case "reviewSchema":
		verdict, issues := ParseReviewVerdict(args[0])
		if len(issues) > 0 {
			return nil
		}
		return verdict

	case "synthesizeFailVerdict":
		var reason, evidence string
		mustUnmarshal(t, args[0], &reason)
		mustUnmarshal(t, args[1], &evidence)
		return SynthesizeFailVerdict(reason, evidence)

	case "extractVerdictFromProse":
		var transcript string
		mustUnmarshal(t, args[0], &transcript)
		return ExtractVerdictFromProse(transcript)

	case "buildReviewPrompt":
		var input ReviewPromptArgs
		mustUnmarshal(t, args[0], &input)
		input.Config = fixtureConfig()
		return BuildReviewPrompt(input)

	case "buildRepairDescription":
		var input RepairDescriptionArgs
		mustUnmarshal(t, args[0], &input)
		return BuildRepairDescription(input)

	case "buildRepairPrompt":
		var input struct {
			RepairTitle string `json:"repairTitle"`
			Description string `json:"description"`
		}
		mustUnmarshal(t, args[0], &input)
		return BuildRepairPrompt(input.RepairTitle, input.Description)

	case "buildReviewTaskDescription":
		var implTaskID, reviewerAgent string
		mustUnmarshal(t, args[0], &implTaskID)
		mustUnmarshal(t, args[1], &reviewerAgent)
		return BuildReviewTaskDescription(implTaskID, reviewerAgent)

	case "buildReviewReminder":
		var reviewTaskID, implTaskID, worktreePath, baseSHA string
		var attempt int
		mustUnmarshal(t, args[0], &reviewTaskID)
		mustUnmarshal(t, args[1], &implTaskID)
		mustUnmarshal(t, args[2], &attempt)
		mustUnmarshal(t, args[3], &worktreePath)
		mustUnmarshal(t, args[4], &baseSHA)
		return BuildReviewReminder(reviewTaskID, implTaskID, attempt, worktreePath, baseSHA)

	case "buildRepairReminder":
		var repairTaskID, implTaskID, previousBranch, worktreePath string
		var attempt, repairCap int
		mustUnmarshal(t, args[0], &repairTaskID)
		mustUnmarshal(t, args[1], &implTaskID)
		mustUnmarshal(t, args[2], &attempt)
		mustUnmarshal(t, args[3], &repairCap)
		mustUnmarshal(t, args[4], &previousBranch)
		mustUnmarshal(t, args[5], &worktreePath)
		return BuildRepairReminder(
			repairTaskID, implTaskID, attempt, repairCap, previousBranch, worktreePath,
		)

	case "buildVerdictNote":
		var verdict ReviewVerdict
		var attempt int
		mustUnmarshal(t, args[0], &verdict)
		mustUnmarshal(t, args[1], &attempt)
		return BuildVerdictNote(verdict, attempt)

	case "buildAuditorPrompt":
		var values [6]string
		for index := range values {
			mustUnmarshal(t, args[index], &values[index])
		}
		return BuildAuditorPrompt(
			values[0], values[1], values[2], values[3], values[4], values[5],
		)

	case "buildSynthesizerPrompt":
		var input SynthesizerPromptInput
		var outputPath string
		mustUnmarshal(t, args[0], &input)
		mustUnmarshal(t, args[1], &outputPath)
		return BuildSynthesizerPrompt(input, outputPath)

	case "SYNTH_FALLBACK":
		return SynthFallback

	case "synthesizedReviewVerdict":
		var decision SynthesizerDecision
		mustUnmarshal(t, args[0], &decision)
		return SynthesizedReviewVerdict(decision)

	case "synthesizerSchema":
		decision, issues := ParseSynthesizerDecision(args[0])
		if len(issues) > 0 {
			return nil
		}
		return decision

	case "buildRetryAdvisorPrompt":
		var input RetryAdvisorInput
		var outputPath string
		mustUnmarshal(t, args[0], &input)
		mustUnmarshal(t, args[1], &outputPath)
		return BuildRetryAdvisorPrompt(input, outputPath)

	case "buildIssueAdvisorPrompt":
		var input IssueAdvisorInput
		var outputPath string
		mustUnmarshal(t, args[0], &input)
		mustUnmarshal(t, args[1], &outputPath)
		return BuildIssueAdvisorPrompt(input, outputPath)

	case "isHighRisk":
		var task any
		mustUnmarshal(t, args[0], &task)
		return IsHighRisk(task)

	case "matchesAnyGlob":
		var file string
		var globs []string
		mustUnmarshal(t, args[0], &file)
		mustUnmarshal(t, args[1], &globs)
		return MatchesAnyGlob(file, globs)
	}
	t.Fatalf("unknown fixture function %q", line.Fn)
	return nil
}

func fixtureConfig() Config {
	return Config{
		Enabled: true, RepairCap: 3, TimeoutMS: 1_800_000,
		MaxToolCalls: 50, SkipGlobs: append([]string(nil), defaultSkipGlobs...),
		RepairTier: "high", ReviewerAgent: "superpowers-code-reviewer",
		RepairAgent: "fixer",
	}
}

func mustUnmarshal(t *testing.T, raw json.RawMessage, value any) {
	t.Helper()
	if err := json.Unmarshal(raw, value); err != nil {
		t.Fatal(err)
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) != 71 {
		t.Fatalf("fixture count = %d, want 71", len(fixtures))
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Fn+"/"+fixture.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(fixtureCall(t, fixture))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != fixture.OutJSON {
				t.Fatalf("output mismatch\nwant: %s\n got: %s", fixture.OutJSON, got)
			}
		})
	}
}
