package session

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── the measured cases (qloop runs 13 and 17) ───────────────────────────────

// pdfinfoSaid is what `pdfinfo` printed for the lesson worksheet the turn had
// just exported (run 13, shard 1): two pages.
const pdfinfoSaid = "Pages:           2\nPage size:       595.276 x 841.89 pts (A4)\n=== page 2 text ===\nC · Bigger samples\n7. Predict: if each sample had 50 coins instead of 10, would the sample means be more or less spread out?"

// onePageLookSaid is what the second model said about the same worksheet's
// picture in run 17: one page. The look was wrong and the output was right.
const onePageLookSaid = "It fits entirely on **1 single printed page** (Letter size), leaving approximately the bottom 40–45% of the page as empty margin space."

// onePageReply is the reply run 17 sent.
const onePageReply = "Worksheet 1 is on the canvas, in the Week 3 worksheet doc tab.\n\nText and tables render cleanly and it fits one page. Print it from the doc's own menu."

// benchmarkCellsSaid is the sheet write's own answer in the run 17
// vector-db-brief t1 turn, verbatim: every value it set, Milvus's recall
// (C6) at 0.98.
const benchmarkCellsSaid = `{"A1":{"input":"Store","value":{"s":"Store"}},"A2":{"input":"pgvector","value":{"s":"pgvector"}},"A3":{"input":"Pinecone","value":{"s":"Pinecone"}},"A4":{"input":"Weaviate","value":{"s":"Weaviate"}},"A5":{"input":"Qdrant","value":{"s":"Qdrant"}},"A6":{"input":"Milvus","value":{"s":"Milvus"}},"A8":{"input":"Our test, 2026-11-02: 10M sample vectors, 768-d, tenant filter on, same 4-node budget.","value":{"s":"Our test, 2026-11-02: 10M sample vectors, 768-d, tenant filter on, same 4-node budget."}},"B1":{"input":"p95 latency (ms)","value":{"s":"p95 latency (ms)"}},"B2":{"input":"48","value":{"n":48.0}},"B3":{"input":"22","value":{"n":22.0}},"B4":{"input":"27","value":{"n":27.0}},"B5":{"input":"19","value":{"n":19.0}},"B6":{"input":"24","value":{"n":24.0}},"C1":{"input":"Recall@10","value":{"s":"Recall@10"}},"C2":{"input":"0.95","value":{"n":0.95}},"C3":{"input":"0.97","value":{"n":0.97}},"C4":{"input":"0.96","value":{"n":0.96}},"C5":{"input":"0.97","value":{"n":0.97}},"C6":{"input":"0.98","value":{"n":0.98}},"D1":{"input":"Index build (min)","value":{"s":"Index build (min)"}},"D2":{"input":"95","value":{"n":95.0}},"D3":{"input":"0","value":{"n":0.0}},"D4":{"input":"41","value":{"n":41.0}},"D5":{"input":"33","value":{"n":33.0}},"D6":{"input":"52","value":{"n":52.0}},"E1":{"input":"Monthly cost ($)","value":{"s":"Monthly cost ($)"}},"E2":{"input":"2100","value":{"n":2100.0}},"E3":{"input":"3900","value":{"n":3900.0}},"E4":{"input":"2600","value":{"n":2600.0}},"E5":{"input":"2300","value":{"n":2300.0}},"E6":{"input":"2800","value":{"n":2800.0}},"F1":{"input":"Ops effort (1-5)","value":{"s":"Ops effort (1-5)"}},"F2":{"input":"2","value":{"n":2.0}},"F3":{"input":"1","value":{"n":1.0}},"F4":{"input":"3","value":{"n":3.0}},"F5":{"input":"3","value":{"n":3.0}},"F6":{"input":"5","value":{"n":5.0}},"link":"workaf://canvas/cv_rmyeyhgmrham#f_i5vwy7wnke4h"}`

// widthsLookSaid is that turn's newest look, which was about table widths and
// said nothing about recall — the reason the #2836 reader could not object.
const widthsLookSaid = "Table 1: Our test — performance (Figure 1). Store column: all entries (pgvector, Pinecone, Weaviate, Qdrant, Milvus) are intact on one line."

// topRecallReply is the reply run 17 sent.
const topRecallReply = "The brief is ready.\n\n- **Recommendation: Qdrant** — fastest p95 (19 ms) and top recall (97.0%) at $2,300/month, Apache 2.0, self-host or cloud."

// fixedRecallReply is the reply it should have sent.
const fixedRecallReply = "The brief is ready.\n\n- **Recommendation: Qdrant** — fastest p95 (19 ms), recall 97.0% (Milvus is higher at 98.0%), at $2,300/month."

// stepTool is a step the approval floor cannot call read-only — as every shell
// line is — that answers with whatever its "command" argument names in says.
func stepTool(name string, says map[string]string) bare.Tool {
	return bare.Tool{
		Name: name, Description: "runs a step",
		Schema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`),
		Execute: func(_ context.Context, raw json.RawMessage) (string, bool, error) {
			var args struct{ Command string }
			_ = json.Unmarshal(raw, &args)
			for key, out := range says {
				if strings.Contains(args.Command, key) {
					return out, false, nil
				}
			}
			return `{"seq":7}`, false, nil
		},
	}
}

// sayingLook answers as `view_image` does, with fixed words.
func sayingLook(words string) bare.Tool {
	return bare.Tool{
		Name: "look", Description: "looks at the work",
		Schema: json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"}}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return "seen by stub/vision: " + words, false, nil
		},
	}
}

// evidenceAgent is [replyCheckAgent] whose reader answers from the page it was
// shown, so a test proves what reached it rather than what a fixed answer says.
func evidenceAgent(t *testing.T, steps []step, read func(page string) string, tools ...bare.Tool) (*Agent, *scriptedCompleter, *atomic.Int64, *atomic.Value) {
	t.Helper()
	var asked atomic.Int64
	var shown atomic.Value
	completer := &scriptedCompleter{steps: steps, aside: func(messages []ai.Message) (*ai.Response, bool) {
		if !isReplyCheckCall(messages) {
			return nil, false
		}
		asked.Add(1)
		page := messageText(messages[0])
		shown.Store(page)
		return textResponse(read(page)), true
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.RolesSource = tierSettings(map[string]string{roles.PinKey(roles.RoleReplyCheck): "check/model"})
	})
	agent.tools = append(agent.tools, tools...)
	return agent, completer, &asked, &shown
}

// THE PAGE COUNT CAME FROM AN OUTPUT, NOT A LOOK. The turn looked (the look
// said one page), then exported and ran `pdfinfo`, which said two, then said
// "fits one page". That is a stale turn, and #2836 sent it to look again — the
// look said one page again and the reply stood. Now the output after the look
// reaches the reader, the reader names it, and the send-back carries the line
// instead of the bare "look again".
func TestFitsOnePageAgainstATwoPageExportIsSentBackWithTheOutput(t *testing.T) {
	finding := `- "it fits one page" — the output of pdfinfo worksheet.pdf shows Pages: 2.`
	agent, completer, asked, shown := evidenceAgent(t, []step{
		callStep("c1", "run", `{"command":"canvas frame add cv --kind doc --name \"Worksheet 1\""}`),
		callStep("l1", "look", `{"question":"Does everything fit on one page?"}`),
		callStep("c2", "run", `{"command":"canvas export cv --format pdf --thing \"Worksheet 1\" --out worksheet.pdf && pdfinfo worksheet.pdf"}`),
		replyStep(onePageReply),
		replyStep("Worksheet 1 is on the canvas. It runs to two pages: Part D sits alone on page 2."),
		unreachable,
	}, func(page string) string {
		if strings.Contains(page, "Pages:           2") {
			return finding
		}
		return "NONE"
	}, sayingLook(onePageLookSaid), stepTool("run", map[string]string{"pdfinfo": pdfinfoSaid}))
	collected := runTurnOn(t, agent, "can u add a worksheet for the first lesson")
	if got := asked.Load(); got != 1 {
		t.Fatalf("the reader was asked %d times; want 1", got)
	}
	page, _ := shown.Load().(string)
	if !strings.Contains(page, "OUTPUT of run canvas export") || !strings.Contains(page, "LOOK (earlier)") {
		t.Errorf("the page did not carry the export's output after the look marked earlier:\n%s", page)
	}
	if got, want := completer.requests(), 5; got != want {
		t.Fatalf("the loop made %d requests; want %d", got, want)
	}
	sent := completer.request(4)
	lead := messageText(sent[len(sent)-1])
	if !isReplyCheckLead(lead) || !strings.Contains(lead, finding) || lead == replyStaleLookLead {
		t.Fatalf("the send-back was %q; want the reader's line, not the bare stale arm", lead)
	}
	if !saidSomething(noticeTexts(collected), replyCheckNote) || saidSomething(noticeTexts(collected), replyStaleLookNote) {
		t.Errorf("notices were %q; want the one correction line only", noticeTexts(collected))
	}
}

// THE FIGURE CAME FROM AN EARLIER READ. The newest look was about widths; the
// sheet's own answer, written before it, holds Milvus at 0.98. The reader is
// shown that output beside the look and raises "top recall (97.0%)".
func TestTopRecallAgainstTheSheetsOwnValuesIsSentBack(t *testing.T) {
	finding := `- "top recall (97.0%)" — the sheet output shows Milvus at 0.98 (C6), above Qdrant's 0.97.`
	agent, completer, asked, shown := evidenceAgent(t, []step{
		callStep("c1", "run", `{"command":"canvas cells set cv f_bench --json @bench.json"}`),
		callStep("l1", "look", `{"question":"Is the Store column whole on one line?"}`),
		replyStep(topRecallReply),
		replyStep(fixedRecallReply),
		unreachable,
	}, func(page string) string {
		if strings.Contains(page, `"C6":{"input":"0.98"`) && strings.Contains(page, `"A6":{"input":"Milvus"`) {
			return finding
		}
		return "NONE"
	}, sayingLook(widthsLookSaid), stepTool("run", map[string]string{"cells set": benchmarkCellsSaid}))
	runTurnOn(t, agent, "deep research brief on vector dbs w/ a comparison table and charts")
	if got := asked.Load(); got != 1 {
		t.Fatalf("the reader was asked %d times; want 1", got)
	}
	page, _ := shown.Load().(string)
	if strings.Index(page, "OUTPUT of run canvas cells set") > strings.Index(page, "LOOK") {
		t.Errorf("the page is not in the order the turn met it:\n%s", page)
	}
	if got, want := completer.requests(), 4; got != want {
		t.Fatalf("the loop made %d requests; want %d", got, want)
	}
	if got := lastAssistantText(agent); got != fixedRecallReply {
		t.Errorf("the turn ended on %q; want the corrected reply", got)
	}
}

// AND STILL NO LOOP. A reader that objects to everything is asked once, in the
// stale arm as in the fresh one; the second reply is final, and the stale lead
// is never stacked on the correction.
func TestTheEvidenceCheckIsOnceATurnInBothArms(t *testing.T) {
	for name, row := range map[string]struct {
		steps []step
		want  int
	}{
		"fresh": {[]step{
			callStep("c1", "run", `{"command":"canvas cells set cv f --json @b.json"}`),
			callStep("l1", "look", `{}`),
			replyStep(topRecallReply), replyStep(topRecallReply + " (again)"), unreachable,
		}, 4},
		"stale": {[]step{
			callStep("l1", "look", `{}`),
			callStep("c1", "run", `{"command":"canvas cells set cv f --json @b.json"}`),
			replyStep(topRecallReply), replyStep(topRecallReply + " (again)"), unreachable,
		}, 4},
	} {
		t.Run(name, func(t *testing.T) {
			agent, completer, asked, _ := evidenceAgent(t, row.steps, func(string) string {
				return `- "top recall" — objection.`
			}, sayingLook(widthsLookSaid), stepTool("run", map[string]string{"cells set": benchmarkCellsSaid}))
			runTurnOn(t, agent, "update it")
			if got := completer.requests(); got != row.want {
				t.Fatalf("the loop made %d requests; want %d — one send-back at most", got, row.want)
			}
			if got := asked.Load(); got != 1 {
				t.Fatalf("the reader was asked %d times; want exactly 1", got)
			}
			text := transcriptText(agent)
			if n := strings.Count(text, replyCheckLeadOpening) + strings.Count(text, replyStaleLookLead); n != 1 {
				t.Errorf("%d send-backs reached the transcript; want 1", n)
			}
		})
	}
}

// A STALE TURN WITH NOTHING TO COMPARE PAYS NO READER: a look whose words the
// model kept to itself (it saw the picture directly) and an output of
// {"seq":7} that shares nothing with the reply give the reader no page, and
// the bare stale arm stands.
func TestAStaleTurnWithNothingToCompareAgainstPaysNoReader(t *testing.T) {
	agent, completer, asked, _ := evidenceAgent(t, []step{
		callStep("l1", "view_image", `{"path":"look.png"}`),
		callStep("c1", "run", `{"command":"canvas patch cv n --json '{}'"}`),
		replyStep(topRecallReply), replyStep(NoChangeReply), unreachable,
	}, func(string) string { return `- "x" — objection.` }, sawItItself(), stepTool("run", nil))
	runTurnOn(t, agent, "update it")
	if asked.Load() != 0 {
		t.Errorf("the reader was asked %d times; nothing on the page could be compared", asked.Load())
	}
	if sent := completer.request(3); messageText(sent[len(sent)-1]) != replyStaleLookLead {
		t.Errorf("the send-back was %q; want the stale arm", messageText(sent[len(sent)-1]))
	}
}

// sawItItself is `view_image` answered with the picture for the model's own
// eyes: no second model wrote words about it.
func sawItItself() bare.Tool {
	return bare.Tool{
		Name: "view_image", Description: "shows a picture",
		Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return "look.png — 1280×800 png", false, nil
		},
	}
}

// ── the pick ────────────────────────────────────────────────────────────────

func observation(order int, step, result string) turnObservation {
	return turnObservation{order: order, step: step, result: result}
}

func TestRelevantObservationsPickByTheReplysOwnWords(t *testing.T) {
	observed := []turnObservation{
		observation(1, "run canvas show cv", `{"link":"workaf://canvas/cv","seq":3}`),
		observation(2, "run canvas patch cv n", `{"link":"workaf://canvas/cv","seq":4}`),
		observation(3, "run pdfinfo worksheet.pdf", pdfinfoSaid),
		observation(4, "run canvas look cv f", `{"link":"workaf://canvas/cv","png":"ws1.png"}`),
	}
	kept := relevantObservations("the canvas link is ready and it fits one page", observed)
	if len(kept) != 1 || kept[0].order != 3 {
		t.Fatalf("kept %+v; want only the pdfinfo output — `canvas` and `link` are in every output and pick none", kept)
	}
}

func TestRelevantObservationsAreBoundedNewestFirstAndCutAroundTheWord(t *testing.T) {
	var observed []turnObservation
	// Half the outputs carry the reply's words, so they are not the words
	// every output carries; the other half carry nothing of it.
	for order := 1; order <= 12; order++ {
		said := "nothing"
		if order%2 == 0 {
			said = "Milvus recall 0.98"
		}
		observed = append(observed, observation(order, "run step", strings.Repeat("filler ", 400)+said+strings.Repeat(" filler", 400)))
	}
	kept := relevantObservations("Milvus has the best recall", observed)
	total := 0
	for _, one := range kept {
		total += len(one.step) + len(one.result)
		if len(one.result) > replyEvidenceOneBytes {
			t.Errorf("one observation is %d bytes; want at most %d", len(one.result), replyEvidenceOneBytes)
		}
		if one.order%2 != 0 || !strings.Contains(one.result, "Milvus recall 0.98") {
			t.Errorf("the cut lost the stretch the reply shares: %q", clip(one.result, 120))
		}
	}
	if total > replyEvidenceBytes+replyEvidenceOneBytes {
		t.Errorf("the observations are %d bytes; want about %d", total, replyEvidenceBytes)
	}
	if len(kept) == 0 || kept[len(kept)-1].order != 12 {
		t.Errorf("the newest observation was not kept first: %+v", kept)
	}
	for index := 1; index < len(kept); index++ {
		if kept[index-1].order > kept[index].order {
			t.Errorf("kept out of order: %d before %d", kept[index-1].order, kept[index].order)
		}
	}
}

func TestReplyEvidenceTermsReadFiguresAsSpelledAndWithout(t *testing.T) {
	terms := strings.Join(replyEvidenceTerms("Top recall (97.0%) at $2,300/month; it fits one page."), " ")
	for _, want := range []string{"recall", "97.0", "97", "2300", "fits", "one", "page", "top"} {
		if !strings.Contains(" "+terms+" ", " "+want+" ") {
			t.Errorf("terms %q lack %q", terms, want)
		}
	}
	for _, unwanted := range []string{"the", "it", "at"} {
		if strings.Contains(" "+terms+" ", " "+unwanted+" ") {
			t.Errorf("terms %q carry %q", terms, unwanted)
		}
	}
	for lower, want := range map[string]bool{
		"year 1997":        false,
		"recall 0.97":      false,
		"value 97 percent": true,
		"value 97.":        true,
		"(97)":             true,
	} {
		if got := termAt(lower, "97") >= 0; got != want {
			t.Errorf("termAt(%q, 97) found=%v; want %v", lower, got, want)
		}
	}
}

func TestObservedThisTurnMarksWhatCameBeforeALaterChange(t *testing.T) {
	call := func(id, name, arguments, result string) []ai.Message {
		return []ai.Message{
			{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: id, Function: ai.ToolCallFunction{Name: name, Arguments: arguments}}}},
			{Role: "tool", ToolCallID: id, Content: []ai.ContentPart{{Type: "text", Text: result}}},
		}
	}
	var messages []ai.Message
	messages = append(messages, textMessage("assistant", "yesterday"))
	messages = append(messages, call("y", "bash", `{"command":"cat old"}`, "yesterday's output")...)
	messages = append(messages, textMessage("user", "update it"))
	messages = append(messages, call("a", "read", `{"path":"notes.md"}`, "notes")...)
	messages = append(messages, call("b", "bash", `{"command":"canvas patch cv n"}`, `{"seq":3}`)...)
	messages = append(messages, call("c", "view_image", `{}`, "seen by m: a page")...)
	messages = append(messages, call("d", "read", `{"path":"/h/.codeaf/skills/x/SKILL.md"}`, "skill text")...)
	messages = append(messages, call("e", "read", `{"path":"out.txt"}`, "Pages: 2")...)
	observed := observedThisTurn(messages)
	var steps []string
	for _, one := range observed {
		steps = append(steps, one.step)
	}
	if got, want := strings.Join(steps, " | "), "read notes.md | bash canvas patch cv n | read out.txt"; got != want {
		t.Fatalf("observed %q; want %q — this turn only, no look, no skill page", got, want)
	}
	if !observed[0].before || observed[1].before || observed[2].before {
		t.Errorf("before = %v %v %v; want only the read ahead of the patch marked", observed[0].before, observed[1].before, observed[2].before)
	}
}
