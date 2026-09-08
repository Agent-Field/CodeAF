package session

import (
	"strings"
	"testing"
)

// THE PARSER'S WHOLE JOB IS TO FAIL WELL. A caption is a line a person reads
// while they wait; the family in front of it is a nicety. So every shape the
// narrator can get wrong has to leave the sentence intact and the family empty,
// and the table below is that claim spelled out one malformation at a time.
func TestTheActionParserKeepsTheSentenceWhateverTheModelDid(t *testing.T) {
	for _, c := range []struct {
		name     string
		raw      string
		category ActionCategory
		sentence string
	}{
		{
			name:     "the format as asked for",
			raw:      "run | starting the local server",
			category: ActionRun,
			sentence: "starting the local server",
		},
		{
			name:     "no spaces around the bar",
			raw:      "search|listing open github issues",
			category: ActionSearch,
			sentence: "listing open github issues",
		},
		{
			name:     "a capitalised family",
			raw:      "Read | reading the caption renderer",
			category: ActionRead,
			sentence: "reading the caption renderer",
		},
		{
			name:     "decorated with markdown, which stripMarkup takes off first",
			raw:      "**edit** | rewriting the loader's prefix",
			category: ActionEdit,
			sentence: "rewriting the loader's prefix",
		},
		{
			// THE LEGACY SHAPE, and it is the one that matters most: every
			// caption written before this field existed, and every model that
			// ignores the new clause, lands here.
			name:     "no bar at all — the plain sentence a previous build wrote",
			raw:      "ranking bugs by end-result quality",
			category: "",
			sentence: "ranking bugs by end-result quality",
		},
		{
			name:     "a word that is not a family — the label goes, the sentence stays",
			raw:      "investigate | reading the caption renderer",
			category: "",
			sentence: "reading the caption renderer",
		},
		{
			// A PHRASE IS NOT A LABEL. Anything with a space in front of the bar
			// is prose that happens to contain one, and prose is kept whole.
			name:     "a sentence that contains a bar",
			raw:      "piping the log through grep | sort",
			category: "",
			sentence: "piping the log through grep | sort",
		},
		{
			name:     "a very long single word before a bar is prose, not a label",
			raw:      "reticulatingthesplines | doing something",
			category: "",
			sentence: "reticulatingthesplines | doing something",
		},
		{
			name:     "a family and nothing after it",
			raw:      "run |",
			category: "",
			sentence: "run |",
		},
		{
			name:     "empty",
			raw:      "",
			category: "",
			sentence: "",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			category, sentence := SplitActionLine(c.raw)
			if category != c.category {
				t.Errorf("family: got %q, want %q", category, c.category)
			}
			if sentence != c.sentence {
				t.Errorf("sentence: got %q, want %q", sentence, c.sentence)
			}
		})
	}
}

// A LINE THAT IS ONLY THE VOCABULARY IS AN ECHO OF THE INSTRUCTION, and
// [cleanCaption] already refuses those in every other shape. This is the shape
// the new clause introduces, so it is refused where it is introduced: the
// deterministic composite standing in the caption slot is a better line than the
// word "run".
func TestAFamilyWordAloneIsNotACaption(t *testing.T) {
	for _, raw := range []string{"run", "search", "  Work  ", "plan."} {
		category, sentence := SplitActionLine(raw)
		if category != "" {
			t.Errorf("%q: a bare word claimed the family %q", raw, category)
		}
		if _, echo := ParseActionCategory(cleanCaption(sentence)); !echo {
			t.Errorf("%q cleaned to %q, which the caller cannot recognise as an echo",
				raw, cleanCaption(sentence))
		}
	}
}

// EVERY TOOL ON THE BELT HAS A FAMILY OF ITS OWN. A hand that landed without one
// would draw the bucket's mark forever and nothing would say so — the gutter
// would be right about "a step is happening" and silent about everything else,
// which is exactly the drift a table like this rots into.
//
// It walks the belt rather than a list, so a tool added tomorrow is on this gate
// the day it lands.
func TestEveryToolOnTheBeltHasAnActionFamily(t *testing.T) {
	agent := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}}
	agent.tools = agent.belt()
	tools := agent.offeredTools()
	if len(tools) == 0 {
		t.Fatal("the belt is empty")
	}
	for _, tool := range tools {
		if ActionCategoryForTool(tool.Name) == ActionWork {
			t.Errorf("the %s tool has no action family — add it to ActionCategoryForTool", tool.Name)
		}
	}
}

// AND THE CONDITIONAL HANDS ARE ON THE GATE TOO. Most of this belt is absent
// from a plain Config — the media verbs, the connected services' families, the
// workspace's furrow verbs, the standing and memory pairs — so the walk above
// cannot see them, and each is a capability a person watches happen.
//
// They are named rather than built because several need a key, a daemon or a
// connected account to exist at all, and a gate that needed those would be a
// gate that skips.
func TestTheConditionalHandsHaveActionFamiliesToo(t *testing.T) {
	for _, name := range []string{
		"web_search", "web_fetch", "search_conversations",
		"remember", "forget", "recall",
		"stand", "watch", "fork", "divide_work", "propose_task", "tasks",
		"revise_assignment", "revise_design", "build_harness", "list_harnesses",
		"propose_subharness", "list_subharnesses",
		"generate_image", "generate_music", "generate_video", "speak",
		"view_image", "edit_video", "read_document",
		"services", "use_service",
		"gmail_search", "gmail_read", "gmail_send",
		"slack_search", "slack_read_thread", "slack_list_channels", "slack_send",
		"calendar_list", "calendar_create",
		"workspace", "workspace_snapshots", "workspace_restore",
		"workspace_fork", "workspace_merge",
		"settings", "change_setting", "load_capability", "manual", "jobs", "track", "commit",
	} {
		if ActionCategoryForTool(name) == ActionWork {
			t.Errorf("the %s tool has no action family — add it to ActionCategoryForTool", name)
		}
	}
}

// A NAME THIS BUILD HAS NEVER SEEN IS A STEP AND NOTHING MORE. A connected
// service arms tools at runtime whose names live on somebody else's server, so
// the bucket is the honest answer — and it must be an ANSWER, because a blank
// there would collapse the gutter and move every sentence beside it.
func TestAnUnknownToolIsTheBucketAndNeverBlank(t *testing.T) {
	for _, name := range []string{"", "  ", "notion_search_pages", "acme_frobnicate", "🙂"} {
		if got := ActionCategoryForTool(name); got != ActionWork {
			t.Errorf("%q: got %q, want %q", name, got, ActionWork)
		}
	}
}

// A BATCH IS NAMED BY WHAT IT OPENED WITH, and the bucket never wins while
// anything more specific is in the batch.
func TestABatchTakesItsFirstNamedFamily(t *testing.T) {
	for _, c := range []struct {
		name  string
		tools []string
		want  ActionCategory
	}{
		{"a reading step that went on to edit", []string{"read", "read", "edit"}, ActionRead},
		{"an editing step that read after itself", []string{"edit", "read"}, ActionEdit},
		{"an unknown service call ahead of a read", []string{"acme_thing", "read"}, ActionRead},
		{"nothing but unknowns", []string{"acme_thing", "other_thing"}, ActionWork},
		{"no calls at all", nil, ActionWork},
	} {
		if got := ActionCategoryForTools(c.tools); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// THE PROMPT NAMES EVERY FAMILY IT WILL ACCEPT. A family the model is never told
// about is a family it can only reach by accident, and a word in the prompt with
// no family behind it is an instruction to write something the parser refuses.
func TestThePromptAndTheParserShareOneVocabulary(t *testing.T) {
	for _, category := range ActionCategories() {
		if !strings.Contains(captionPrompt, string(category)) {
			t.Errorf("the caption prompt never mentions the %q family", category)
		}
		if got, ok := ParseActionCategory(string(category)); !ok || got != category {
			t.Errorf("the parser refuses its own family %q", category)
		}
	}
	if !strings.Contains(captionPrompt, "run | starting the local server") {
		t.Error("the caption prompt no longer shows the format it asks for")
	}
}
