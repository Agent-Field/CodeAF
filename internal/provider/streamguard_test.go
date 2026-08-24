package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the corpus ──────────────────────────────────────────────────────────────
//
// THE TWO DEGENERATE TEXTS ARE REAL. They were streamed by
// deepseek/deepseek-v4-pro-0813 into one conversation on 2026-08-20, at about
// 150k input tokens, and are excerpted verbatim into testdata: the first
// collapses into multi-script soup ("стаthisada", "済", a run of six hundred
// letter s), and the second ends in several thousand repetitions of "    0\n".
// Both were recorded in the transcript, both went back into the next request,
// and the second was worse than the first.
//
// The innocents beside them are the shapes a legitimate reply takes that LOOK
// like those two from a distance: text that compresses to nothing because it is
// a matrix or a log, and text that mixes alphabets because the answer is about
// more than one language. Every one of them must survive.

func loadCorpus(t *testing.T, name string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read corpus %s: %v", name, err)
	}
	return string(text)
}

// feed streams text through the guard the way the read loop does — in deltas,
// not in one piece — and says whether it tripped and after how much.
func feed(text string, delta int) (bool, int) {
	watch := &babbleWatch{}
	for at := 0; at < len(text); at += delta {
		end := at + delta
		if end > len(text) {
			end = len(text)
		}
		if watch.write(text[at:end]) {
			return true, end
		}
	}
	return false, len(text)
}

func TestTheRealDegenerationsAreCut(t *testing.T) {
	for _, name := range []string{"babble-mixed-script.txt", "babble-repetition-loop.txt"} {
		text := loadCorpus(t, name)
		tripped, at := feed(text, 48)
		if !tripped {
			t.Fatalf("%s streamed to the end untouched — %d bytes of it", name, len(text))
		}
		t.Logf("%s cut after %d of %d bytes", name, at, len(text))
	}
}

// TestOneDegenerationIsCutAtEveryDeltaSize pins that the answer is a property of
// the TEXT and not of how the endpoint happened to chop it up. A guard that only
// worked at one chunk size would be a guard that worked in the test.
func TestOneDegenerationIsCutAtEveryDeltaSize(t *testing.T) {
	text := loadCorpus(t, "babble-repetition-loop.txt")
	for _, delta := range []int{1, 3, 17, 200, 4096} {
		if tripped, _ := feed(text, delta); !tripped {
			t.Fatalf("delta %d: the loop streamed to the end untouched", delta)
		}
	}
}

func TestLegitimateRepliesAreLeftAlone(t *testing.T) {
	innocents := map[string]string{
		"a zero matrix in a code fence": "Here is what the test printed:\n\n```\n" +
			strings.Repeat("    0\n", 900) + "```\n\nEvery entry is zero, as expected.\n",
		"a test log in a code fence": "The suite says:\n\n```\n" +
			strings.Repeat("ok  \tgithub.com/x/y\t0.001s\n", 400) + "```\n",
		"repetitive code in a code fence": "```go\n" + strings.Repeat(
			"func handle(w http.ResponseWriter, r *http.Request) {\n\tif err := do(r); err != nil {\n\t\thttp.Error(w, err.Error(), 500)\n\t\treturn\n\t}\n}\n", 80) + "```\n",
		"a big JSON dump in a code fence": "```json\n" + strings.Repeat(
			"  {\"id\": 1, \"name\": \"row\", \"value\": 0},\n", 500) + "```\n",
		"a long markdown table":             markdownTable(300),
		"ascii art with no fence":           asciiArt(200),
		"markdown rules between paragraphs": markdownRules(90),
		"a bilingual English and Chinese answer": strings.Repeat(
			"The Chinese word for computer is 电脑, literally 'electric brain'. "+
				"In a sentence: 我的电脑很快, meaning 'my computer is fast'. "+
				"The formal register prefers 计算机, which you see in academic writing. ", 30),
		"a four-language glossary": strings.Repeat(
			"In Russian that is компьютер, in Chinese 电脑, and in Greek υπολογιστής. "+
				"Each borrows differently: компьютер is a transliteration, "+
				"电脑 is a calque, and υπολογιστής is a native coinage. ", 30),
		"a four-script table": strings.Repeat(
			"| en | ru | zh | el |\n| hello | привет | 你好 | γεια |\n"+
				"| world | мир | 世界 | κόσμος |\n| thanks | спасибо | 谢谢 | ευχαριστώ |\n", 40),
		"Japanese prose with Latin nouns": strings.Repeat(
			"このAPIはHTTPリクエストを受け取り、JSONを返します。Goのnet/httpパッケージを使い、"+
				"ctxをWithTimeoutで包むのが基本です。エラーはerrors.Isで判定してください。", 40),
		"emoji-heavy prose": strings.Repeat(
			"Done! ✅ The build passed 🎉 and the tests are green 🟢. "+
				"Next up: the deploy 🚀 — I will ping you when it lands. ", 40),
		"loanwords and accents": strings.Repeat(
			"The naive approach — a soupçon of caching, plus a résumé of the schema — "+
				"handles the déjà vu case. See the München benchmark and the 日本語 note. ", 40),
		"mathematics in Greek letters": strings.Repeat(
			"The bound is σ ≤ ε·√n where ε is the tolerance and n the sample count. "+
				"Substituting μ for the mean gives Δ = |μ̂ − μ| ≤ σ/√n with probability 1−δ. ", 40),
		"plain English prose": plainProse(120),
	}
	for name, text := range innocents {
		if tripped, at := feed(text, 48); tripped {
			t.Errorf("%s was cut at %d of %d bytes — a legitimate reply must survive",
				name, at, len(text))
		}
	}
}

// TestAShortRepetitionIsNotADegeneration pins the full-window rule: somebody
// answering "no, no, no" is not a model that has come off the rails, and the
// compression test may not say anything until it has four kilobytes to read.
func TestAShortRepetitionIsNotADegeneration(t *testing.T) {
	if tripped, _ := feed(strings.Repeat("no. ", 400), 8); tripped {
		t.Fatal("a short repeated line was cut before the window was even full")
	}
}

// TestAFenceTheSoupInventedDoesNotBlindTheGuard is the reason fenceLine is
// strict. The real degeneration emitted "```ongoingSpark......" mid-soup, and a
// scanner that took that for a code fence would have stopped reading exactly
// when it mattered.
func TestAFenceTheSoupInventedDoesNotBlindTheGuard(t *testing.T) {
	text := "```ongoingSpark........................\n" + strings.Repeat("    0\n", 900)
	if tripped, _ := feed(text, 48); !tripped {
		t.Fatal("a loop behind an invented fence was not cut")
	}
	if _, ok := fenceLine([]byte("```ongoingSpark........................\n")); ok {
		t.Fatal("an info string full of dots was read as a language tag")
	}
	for _, opener := range []string{"```", "```go\n", "   ```json\n", "```c++\n"} {
		if _, ok := fenceLine([]byte(opener)); !ok {
			t.Fatalf("%q is a real fence and was not read as one", opener)
		}
	}
}

// TestOneUnbrokenLineCannotOutrunTheGuard pins that a model writing megabytes
// with no newline is still read: the pending line is settled once it is longer
// than the window, so nothing can hide in it.
func TestOneUnbrokenLineCannotOutrunTheGuard(t *testing.T) {
	if tripped, _ := feed(strings.Repeat("s", 40000), 64); !tripped {
		t.Fatal("forty thousand letters on one line were not cut")
	}
}

// asciiArt is a drawn table with real figures in it, which is what a model
// producing one actually sends.
func asciiArt(rows int) string {
	var out strings.Builder
	for i := 0; i < rows; i++ {
		out.WriteString("+------+------+------+\n")
		fmt.Fprintf(&out, "| %4d | %4d | %4d |\n", i, i*7%997, i*31%89)
	}
	return out.String()
}

func markdownTable(rows int) string {
	var out strings.Builder
	out.WriteString("| id | name | value |\n| --- | --- | --- |\n")
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&out, "| row-%d | %s | %d |\n", i, strings.Repeat(string(rune('a'+i%8)), 6), i*37)
	}
	return out.String()
}

func markdownRules(sections int) string {
	words := strings.Fields("alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau")
	var out strings.Builder
	for i := 0; i < sections; i++ {
		fmt.Fprintf(&out, "## Section %d\n\n%s\n\n", i, strings.Repeat("-", 78))
		for w := 0; w < 14; w++ {
			out.WriteString(words[(i*7+w*3)%len(words)] + " ")
		}
		out.WriteString(".\n\n")
	}
	return out.String()
}

func plainProse(sentences int) string {
	words := strings.Fields("the guard sits at the stream layer and watches two things at once it does not " +
		"read tool results because a tool that prints a million zeros is doing its job only what the " +
		"model itself is saying reaches this and nothing else ever will")
	var out strings.Builder
	for i := 0; i < sentences; i++ {
		for w := 0; w < 18; w++ {
			out.WriteString(words[(i*11+w*5)%len(words)] + " ")
		}
		out.WriteString(". ")
	}
	return out.String()
}

// ── the silence watchdog ────────────────────────────────────────────────────

// TestASilentEndpointIsCutAndNamed streams response headers and then nothing at
// all, with the watchdog's bounds shortened for the test, and pins that the call
// comes back as a cut rather than as a torn connection.
func TestASilentEndpointIsCutAndNamed(t *testing.T) {
	restore := shortenStallBounds(t, 60*time.Millisecond, 60*time.Millisecond)
	defer restore()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		// Keepalive comments and nothing else: bytes on the wire, no model
		// writing. This is exactly the shape transport.go's byte watchdog
		// cannot see. The comments buy the stream the buffered cap — and
		// nothing past it, which is what this test now proves: an endpoint
		// that speaks forever and answers never is still cut.
		for i := 0; i < 40; i++ {
			fmt.Fprint(w, ": keepalive\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	_, err := streamAgainst(t, server.URL, nil)
	cut, ok := CutFrom(err)
	if !ok {
		t.Fatalf("err = %v, want a stream cut", err)
	}
	if cut.Reason != CutSilent {
		t.Fatalf("reason = %d, want CutSilent", cut.Reason)
	}
	if !strings.Contains(cut.Error(), "nothing came back") {
		t.Fatalf("sentence = %q", cut.Error())
	}
}

// TestAStreamThatGoesQuietMidReplyIsCutAndNamed is the second bound: the model
// wrote, so the wait is a gap rather than a wait to be served, and the two are
// named differently because a person watching them sees two different things.
func TestAStreamThatGoesQuietMidReplyIsCutAndNamed(t *testing.T) {
	restore := shortenStallBounds(t, 5*time.Second, 60*time.Millisecond)
	defer restore()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: "+deltaChunk("the answer begins")+"\n\n")
		w.(http.Flusher).Flush()
		for i := 0; i < 40; i++ {
			fmt.Fprint(w, ": keepalive\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	_, err := streamAgainst(t, server.URL, nil)
	cut, ok := CutFrom(err)
	if !ok {
		t.Fatalf("err = %v, want a stream cut", err)
	}
	if cut.Reason != CutStalled {
		t.Fatalf("reason = %d, want CutStalled", cut.Reason)
	}
}

// TestTheWatchdogDoesNotEatAnInterrupt is the law the guard must never break: a
// person who stopped the turn gets their stop back, not a report about the
// provider.
func TestTheWatchdogDoesNotEatAnInterrupt(t *testing.T) {
	restore := shortenStallBounds(t, 40*time.Millisecond, 40*time.Millisecond)
	defer restore()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for i := 0; i < 40; i++ {
			fmt.Fprint(w, ": keepalive\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	ctx, stop := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, stop)
	_, err := streamAgainstCtx(ctx, t, server.URL, nil)
	if _, ok := CutFrom(err); ok {
		t.Fatalf("an interrupt came back as a stream cut: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("the test never actually interrupted")
	}
}

// TestAQuietStreamWhoseEndpointStillSpeaksIsGivenPatience is the buffering
// case measured on 2026-08-24: the model goes quiet past the gap bound while
// the endpoint assembles the answer server-side, keepalives flowing the whole
// time — and then the answer LANDS. Five of sixteen production endpoints
// deliver tool calls exactly this way; before the buffered cap existed, every
// one of those streams was cut on the verge of finishing.
func TestAQuietStreamWhoseEndpointStillSpeaksIsGivenPatience(t *testing.T) {
	restore := shortenStallBoundsCapped(t, 5*time.Second, 60*time.Millisecond, 800*time.Millisecond)
	defer restore()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: "+deltaChunk("the answer begins ")+"\n\n")
		w.(http.Flusher).Flush()
		// Quiet for three gap bounds — a cut under the old law — with the
		// endpoint speaking the whole time.
		for i := 0; i < 20; i++ {
			fmt.Fprint(w, ": keepalive\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Fprint(w, "data: "+deltaChunk("and lands whole")+"\n\n")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	response, err := streamAgainst(t, server.URL, nil)
	if err != nil {
		t.Fatalf("a buffered stream that finished was cut: %v", err)
	}
	if got := response.Text(); got != "the answer begins and lands whole" {
		t.Fatalf("text = %q", got)
	}
}

// TestPatienceEndsAtTheBufferedCap is the trickler the header always feared: an
// endpoint that speaks forever and answers never. The cap is the whole reason
// the extension is safe to grant, and the cut names the cap it waited.
func TestPatienceEndsAtTheBufferedCap(t *testing.T) {
	restore := shortenStallBoundsCapped(t, 60*time.Millisecond, 60*time.Millisecond, 200*time.Millisecond)
	defer restore()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: "+deltaChunk("started")+"\n\n")
		w.(http.Flusher).Flush()
		for i := 0; i < 60; i++ {
			fmt.Fprint(w, ": keepalive\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	_, err := streamAgainst(t, server.URL, nil)
	cut, ok := CutFrom(err)
	if !ok {
		t.Fatalf("err = %v, want a stream cut", err)
	}
	if cut.Reason != CutStalled {
		t.Fatalf("reason = %d, want CutStalled", cut.Reason)
	}
	if cut.Waited != stallBufferedBound {
		t.Fatalf("waited = %v, want the buffered cap %v — the sentence must name the wait the person actually watched", cut.Waited, stallBufferedBound)
	}
}

// TestDeadSilenceIsStillCutAtThePlainBound pins that the extension is only for
// an endpoint that is SPEAKING: a connection sending nothing at all gets the
// old bounds, because that one really is not coming back.
func TestDeadSilenceIsStillCutAtThePlainBound(t *testing.T) {
	restore := shortenStallBoundsCapped(t, 5*time.Second, 60*time.Millisecond, 10*time.Second)
	defer restore()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: "+deltaChunk("the answer begins")+"\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	began := time.Now()
	_, err := streamAgainst(t, server.URL, nil)
	cut, ok := CutFrom(err)
	if !ok {
		t.Fatalf("err = %v, want a stream cut", err)
	}
	if cut.Reason != CutStalled {
		t.Fatalf("reason = %d, want CutStalled", cut.Reason)
	}
	if cut.Waited != stallGapBound {
		t.Fatalf("waited = %v, want the plain gap bound %v", cut.Waited, stallGapBound)
	}
	if elapsed := time.Since(began); elapsed > 400*time.Millisecond {
		t.Fatalf("a dead connection was given %v — the buffered patience is only for an endpoint that is speaking", elapsed)
	}
}

// TestASlowButLivingStreamIsNeverCut pins the other side of it: a model writing
// steadily, more slowly than the gap bound, is a model that is working.
func TestASlowButLivingStreamIsNeverCut(t *testing.T) {
	restore := shortenStallBounds(t, 200*time.Millisecond, 80*time.Millisecond)
	defer restore()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for i := 0; i < 12; i++ {
			fmt.Fprint(w, "data: "+deltaChunk("word ")+"\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(30 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	response, err := streamAgainst(t, server.URL, nil)
	if err != nil {
		t.Fatalf("a healthy slow stream failed: %v", err)
	}
	if got := response.Text(); got != strings.Repeat("word ", 12) {
		t.Fatalf("text = %q", got)
	}
}

// ── the degeneration guard end to end ───────────────────────────────────────

func TestADegenerateStreamComesBackAsACutAndNothingElse(t *testing.T) {
	soup := loadCorpus(t, "babble-repetition-loop.txt")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for at := 0; at < len(soup); at += 200 {
			end := at + 200
			if end > len(soup) {
				end = len(soup)
			}
			fmt.Fprint(w, "data: "+deltaChunk(soup[at:end])+"\n\n")
			w.(http.Flusher).Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	response, err := streamAgainst(t, server.URL, nil)
	cut, ok := CutFrom(err)
	if !ok {
		t.Fatalf("err = %v, want a stream cut", err)
	}
	if cut.Reason != CutBabble {
		t.Fatalf("reason = %d, want CutBabble", cut.Reason)
	}
	// THE HIGHEST-VALUE PROPERTY: no response means nothing to record, so the
	// junk cannot reach the transcript and cannot be re-sent next turn.
	if response != nil {
		t.Fatalf("a cut stream returned a response of %d bytes", len(response.Text()))
	}
}

func TestTheGuardCanBeSwitchedOff(t *testing.T) {
	soup := loadCorpus(t, "babble-repetition-loop.txt")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: "+deltaChunk(soup)+"\n\n")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	response, err := streamAgainstCtx(WithoutBabbleGuard(context.Background()), t, server.URL, nil)
	if err != nil {
		t.Fatalf("with the guard off the stream must land: %v", err)
	}
	if response.Text() != soup {
		t.Fatal("with the guard off the reply must come back byte for byte")
	}
}

// ── plumbing ────────────────────────────────────────────────────────────────

// shortenStallBounds makes the silence bounds testable. They are constants in
// the shipping binary for the one-source-of-truth reason; this swaps the
// variables the watchdog actually reads and puts them back. The buffered cap
// travels with the two bounds because every keepalive-sending test server is
// now buying patience against it, and a test that shortened only the bounds
// would sit through the real two and a half minutes.
func shortenStallBounds(t *testing.T, first, gap time.Duration) func() {
	t.Helper()
	return shortenStallBoundsCapped(t, first, gap, 4*gap)
}

func shortenStallBoundsCapped(t *testing.T, first, gap, buffered time.Duration) func() {
	t.Helper()
	oldFirst, oldGap, oldBuffered := stallFirstBound, stallGapBound, stallBufferedBound
	stallFirstBound, stallGapBound, stallBufferedBound = first, gap, buffered
	return func() { stallFirstBound, stallGapBound, stallBufferedBound = oldFirst, oldGap, oldBuffered }
}

func deltaChunk(text string) string {
	payload, err := json.Marshal(streamChunk{
		Choices: []streamChoice{{Delta: streamDelta{Content: text}}},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func streamAgainst(t *testing.T, base string, observed *[]StreamEvent) (*ai.Response, error) {
	t.Helper()
	return streamAgainstCtx(context.Background(), t, base, observed)
}

func streamAgainstCtx(ctx context.Context, t *testing.T, base string, observed *[]StreamEvent) (*ai.Response, error) {
	t.Helper()
	client, err := NewClient(Config{APIKey: "k", BaseURL: base, Model: "sim/model"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx = WithStreamObserver(ctx, func(event StreamEvent) {
		if observed != nil {
			*observed = append(*observed, event)
		}
	})
	return client.CompleteWithMessages(ctx, []ai.Message{{
		Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}},
	}})
}
