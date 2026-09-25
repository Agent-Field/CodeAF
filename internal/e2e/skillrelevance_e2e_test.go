//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// ── does a skill get picked up when the request does not use its words ─────
//
// TestSkillRelevanceEval asks a real model a set of requests through the real
// binary's headless door, against a home holding ten skills written the way
// people write them, and records which requests reached the skill they are
// about. Twelve requests PARAPHRASE a skill's purpose, and four are about
// nothing any skill covers. Every skill's body holds a code word that exists
// nowhere else and tells the model to open its answer with it, so a reply
// that carries the word is a reply that read the skill — and a reply that
// carries the wrong one, or one where no skill applies, is a false pick.
//
// A PICK IS ALSO A SKILL THE RUN OPENED. The model does not always obey the
// code-word rule after reading a skill — it may follow the procedure and drop
// the ceremony — so a run whose printed steps read one skill's SKILL.md
// counts as picking it too. The table says which way each pick was seen.
//
// IT EXISTS BECAUSE THE FIRST CHOICE WAS LITERAL. The skills a message carries
// are picked by the words it shares with a description, and "sketch the deck
// for the board" shares none with "PowerPoint presentations: slides". The
// table this prints is the before and after of giving the model the whole
// catalog to choose from.
//
// Two knobs, both for measuring rather than for passing:
//
//	SKILL_EVAL_BINARY=<path>   run another build (the "before" arm) instead of bin/codeaf
//	SKILL_EVAL_REPORT_ONLY=1   print the table and assert nothing
//	SKILL_EVAL_MEMORY=off      run with memory.enabled off
func TestSkillRelevanceEval(t *testing.T) {
	key := liveKey(t)
	bin := strings.TrimSpace(os.Getenv("SKILL_EVAL_BINARY"))
	if bin == "" {
		bin = binary(t)
	}
	memory := config.MemoryOn
	if strings.TrimSpace(os.Getenv("SKILL_EVAL_MEMORY")) == config.MemoryOff {
		memory = config.MemoryOff
	}
	home, err := os.MkdirTemp("", "afev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	writeSkillJSON(t, filepath.Join(home, "config.json"), map[string]any{
		"model.talk":            "deepseek/deepseek-v4-flash",
		"tools.approvalMode":    "allow",
		config.KeyMemoryEnabled: memory,
	})
	for _, skill := range evalSkills {
		writeSkill(t, filepath.Join(home, ".claude", "skills", skill.name), skill.name, skill.description,
			"This procedure has one rule that proves it was followed: begin your reply with the line "+skill.code+
				", then answer. Keep the answer under five lines and do not create or edit any files.")
	}

	type row struct {
		prompt, want, got, seen string
		shared                  int
		hit                     bool
	}
	rows := make([]row, 0, len(evalPrompts))
	for index, prompt := range evalPrompts {
		// EACH REQUEST IN A FOLDER OF ITS OWN. The headless door resumes the
		// last conversation in a folder, so one shared folder would hand every
		// request the skills the ones before it read, and the rows would stop
		// being independent measurements.
		ws := newWorkspace(t, fmt.Sprintf("evalspace%02d", index+1), false)
		out := evalOnce(t, bin, home, ws, key, prompt.text)
		var got, seen []string
		for _, skill := range evalSkills {
			said := strings.Contains(out, skill.code)
			opened := strings.Contains(out, filepath.Join(".claude", "skills", skill.name, "SKILL.md"))
			switch {
			case said && opened:
				got, seen = append(got, skill.name), append(seen, "code+read")
			case said:
				got, seen = append(got, skill.name), append(seen, "code")
			case opened:
				got, seen = append(got, skill.name), append(seen, "read")
			}
		}
		rows = append(rows, row{
			prompt: prompt.text, want: prompt.skill,
			got: strings.Join(got, " "), seen: strings.Join(seen, " "),
			shared: sharedWords(prompt.text, prompt.skill),
			hit:    strings.Join(got, " ") == prompt.skill,
		})
	}

	var table strings.Builder
	paraphraseHits, paraphrases, quietRight, quiet := 0, 0, 0, 0
	fmt.Fprintf(&table, "\n| # | want | got | seen as | shared words | result | request |\n|---|---|---|---|---|---|---|\n")
	for index, r := range rows {
		want := r.want
		if want == "" {
			want = "(none)"
			quiet++
			if r.hit {
				quietRight++
			}
		} else {
			paraphrases++
			if r.hit {
				paraphraseHits++
			}
		}
		result := "miss"
		if r.hit {
			result = "hit"
		}
		got := r.got
		if got == "" {
			got = "(none)"
		}
		seen := r.seen
		if seen == "" {
			seen = "-"
		}
		fmt.Fprintf(&table, "| %d | %s | %s | %s | %d | %s | %s |\n", index+1, want, got, seen, r.shared, result, r.prompt)
	}
	fmt.Fprintf(&table, "\nparaphrases reaching their skill: %d/%d; requests with no skill left alone: %d/%d (memory %s)\n",
		paraphraseHits, paraphrases, quietRight, quiet, memory)
	t.Log(table.String())

	if os.Getenv("SKILL_EVAL_REPORT_ONLY") == "1" {
		return
	}
	// THE FLOOR IS THE GOAL STATED AS A NUMBER: two paraphrases in three find
	// their skill, and at most one request that needs none is handed one. One
	// run is one sample of a model that does not answer the same way twice —
	// on 2026-09-23 two runs of the same catalog scored 8 and 11 of 12 — so
	// the floor sits below the spread rather than at its top.
	if paraphraseHits < paraphrases*2/3 {
		t.Errorf("only %d of %d paraphrased requests reached their skill", paraphraseHits, paraphrases)
	}
	if quiet-quietRight > 1 {
		t.Errorf("%d of %d requests that need no skill were handed one", quiet-quietRight, quiet)
	}
}

// evalOnce runs one request through the headless door and answers what it
// printed. A run that fails is recorded as an empty answer — a miss — rather
// than stopping the table, because the table is the result.
func evalOnce(t *testing.T, bin, home, ws, key, text string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, bin, "chat", "--one-model", "--once", text)
	command.Dir = ws
	command.Env = append(os.Environ(),
		"CODEAF_HOME="+home, "HOME="+home, "CODEAF_TELEMETRY=off",
		config.APIKeyEnv+"="+key)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Logf("the run for %q ended with %v", text, err)
	}
	t.Logf("── %s ──\n%s", text, out)
	return string(out)
}

// sharedWords counts the words of three letters or more a request shares with
// the description of the skill it is about — the whole signal the literal
// first pass has. Zero is a true paraphrase.
func sharedWords(text, skill string) int {
	description := ""
	for _, candidate := range evalSkills {
		if candidate.name == skill {
			description = candidate.description
		}
	}
	words := func(s string) map[string]bool {
		set := map[string]bool{}
		for _, field := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
		}) {
			if len(field) >= 3 {
				set[field] = true
			}
		}
		return set
	}
	have := words(description)
	count := 0
	for word := range words(text) {
		if have[word] {
			count++
		}
	}
	return count
}

// evalSkill is one skill in the evaluation's home: synthetic stand-ins with
// the shape and the vocabulary of the skills people actually install.
type evalSkill struct{ name, description, code string }

var evalSkills = []evalSkill{
	{"slide-deck", "Create, edit and read PowerPoint .pptx presentations: slides, layouts, speaker notes and templates.", "SKILLCODE-DECK-4471"},
	{"spreadsheet-kit", "Create and edit Excel .xlsx files: formulas, cell formatting, charts and pivot tables.", "SKILLCODE-SHEET-8820"},
	{"pdf-tools", "Extract text and tables from PDF files, fill PDF forms, merge and split PDF documents.", "SKILLCODE-PDF-3190"},
	{"word-docs", "Create and edit .docx documents with tracked changes, comments and formatting.", "SKILLCODE-DOCX-5562"},
	{"release-notes", "Write release notes and changelog entries from merged pull requests and commits.", "SKILLCODE-NOTES-2047"},
	{"docker-deploy", "Build container images and write Dockerfiles and compose files for deployment.", "SKILLCODE-CONTAINER-6603"},
	{"sql-migrations", "Write and review database schema migrations for PostgreSQL, with rollback steps.", "SKILLCODE-MIGRATE-7715"},
	{"brand-voice", "Apply the company's brand colours, typography and tone of voice to written and visual material.", "SKILLCODE-BRAND-9938"},
	{"flaky-tests", "Diagnose intermittently failing tests: reproduce, isolate timing and ordering causes, stabilise.", "SKILLCODE-FLAKY-1284"},
	{"api-docs", "Generate OpenAPI reference documentation for HTTP endpoints.", "SKILLCODE-OPENAPI-3356"},
}

// evalPrompts are the requests: twelve that paraphrase one skill's purpose
// and four that no skill covers (skill "").
var evalPrompts = []struct{ text, skill string }{
	{"I have to walk the board through our quarterly numbers on Thursday. Sketch the deck I should put together.", "slide-deck"},
	{"Turn these three points into something I can put up on screen for investors: growth, margins, hiring.", "slide-deck"},
	{"Help me set up a household budget tracker workbook where the monthly totals add themselves up.", "spreadsheet-kit"},
	{"A vendor emailed me a scanned invoice as an attachment. How do I pull the line items out into something I can edit?", "pdf-tools"},
	{"My lawyer wants redlines on the contract draft she sent me as a Microsoft Word file. How should I mark up my edits?", "word-docs"},
	{"We ship version 2.3 tomorrow. Summarise what changed since 2.2 in a way our customers will understand.", "release-notes"},
	{"How do I package this little Flask service so it runs the same on the staging server as on my laptop?", "docker-deploy"},
	{"I need to add a non-null region column to the orders table in Postgres without taking the site down. How?", "sql-migrations"},
	{"Draft a short post announcing our new office that sounds like us: warm, plain, a bit playful.", "brand-voice"},
	{"One of our CI checks passes on my machine but fails about a third of the time on the build server. Where do I start?", "flaky-tests"},
	{"Our partners keep asking what our REST routes accept and return. How should we publish a reference for them?", "api-docs"},
	{"Combine these two scanned contracts into a single file and pull page seven out on its own.", "pdf-tools"},
	{"What is the capital of Australia?", ""},
	{"Explain the difference between a mutex and a semaphore in two sentences.", ""},
	{"Suggest a name for a golden retriever puppy.", ""},
	{"Convert 72 degrees Fahrenheit to Celsius.", ""},
}
