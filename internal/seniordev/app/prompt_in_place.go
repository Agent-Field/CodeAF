//go:build !windows

package app

import (
	"fmt"
	"strings"
)

// The prompts the model reads name git in a few places, because under the
// default recorder git is how the promises are kept. Under --in-place they are
// not, and a prompt that says otherwise is a prompt that lies: the model would
// reach for `git diff` to review its own work and get an error back.
//
// The git-mode text is NOT edited. Every rewrite below is applied only on the
// in-place path, so a default run's prompt bytes — and therefore its prompt
// hash, its cache prefix and its comparability to earlier runs — are exactly
// what they were before this mode existed. That is the whole reason this is a
// substitution table rather than a reworded prompt.
//
// Each entry must fire. A rewrite that silently matches nothing would leave
// the model with git-shaped instructions it cannot follow, so applyInPlace
// returns an error naming the miss, and a test pins every entry against the
// real prompt text.
type promptRewrite struct {
	from string
	to   string
}

// coderPromptRewrites adapt the baked system prompt.
var coderPromptRewrites = []promptRewrite{
	{
		from: "from the starting commit, when `.senior-dev/checklist.md` does not exist, when",
		to:   "from the tree senior-dev recorded at the start, when `.senior-dev/checklist.md` does not exist, when",
	},
	{
		from: "`.senior-dev/` and git-ignored paths are excluded from the answer. Everything else",
		to:   "`.senior-dev/` and ignored paths are excluded from the answer. Everything else",
	},
}

// soloPromptRewrites adapt the run instruction, and add the one thing the
// model cannot infer: that git is not available to it here.
var soloPromptRewrites = []promptRewrite{
	{
		from: "The workspace is a git repository. Your tools are the ones declared with this\nturn: a shell, file reading, editing, search, web access, and submit.",
		to: "The workspace is a directory. It may or may not be a git repository, and " +
			"either way\nthis run does not use git: it makes no commits and creates no " +
			"branches, and\n`git diff` will not show you your work. What is on disk is " +
			"the record.\n\nYour tools are the ones declared with this turn: a shell, " +
			"file reading, editing,\nsearch, web access, and submit.",
	},
	{
		from: ".senior-dev/ and git-ignored paths are excluded from the answer. Everything else in\nthe working tree, committed or not, is part of what you submit.",
		to:   ".senior-dev/ and ignored paths are excluded from the answer. Everything else in\nthe working tree is part of what you submit.",
	},
	{
		from: "It refuses, naming the cause, when the tree is unchanged from the starting\ncommit, when .senior-dev/checklist.md does not exist, when reason or evidence is\nempty, or when this run already submitted. A refusal does not end the run.",
		to:   "It refuses, naming the cause, when the tree is unchanged from the one senior-dev\nrecorded at the start, when .senior-dev/checklist.md does not exist, when reason or\nevidence is empty, or when this run already submitted. A refusal does not end\nthe run.",
	},
}

// applyPromptRewrites returns text with every rewrite applied, or an error
// naming the first one that matched nothing.
func applyPromptRewrites(text string, rewrites []promptRewrite) (string, error) {
	for index, rewrite := range rewrites {
		if !strings.Contains(text, rewrite.from) {
			return "", fmt.Errorf(
				"in-place prompt rewrite %d no longer matches the prompt: %q",
				index, firstLine(rewrite.from),
			)
		}
		text = strings.Replace(text, rewrite.from, rewrite.to, 1)
	}
	return text, nil
}

func firstLine(value string) string {
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return value[:index]
	}
	return value
}

// rewritesGitText reports whether this recorder's prompts need adapting. Only
// the git recorder leaves them alone.
func rewritesGitText(recorder workspaceRecorder) bool {
	return recorder != nil && recorder.Kind() != "git"
}

// adaptCoderPrompt rewrites the baked system prompt. It runs on every turn,
// because the system prompt is rebuilt for each one.
func adaptCoderPrompt(recorder workspaceRecorder, coder string) (string, error) {
	if !rewritesGitText(recorder) {
		return coder, nil
	}
	return applyPromptRewrites(coder, coderPromptRewrites)
}

// adaptSoloPrompt rewrites the run instruction. It runs once, where that
// instruction is assembled -- later turns carry short continuations that never
// contained this text and must not be searched for it.
func adaptSoloPrompt(recorder workspaceRecorder, solo string) (string, error) {
	if !rewritesGitText(recorder) {
		return solo, nil
	}
	return applyPromptRewrites(solo, soloPromptRewrites)
}
