package session

// Pictures ride the transcript as references and become bytes only at the request
// that carries them, under that request's budget. These tests count the reads.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// countFiles swaps the request's disk door for one that counts, for one test.
func countFiles(t *testing.T) (stats, reads *int) {
	t.Helper()
	before := requestImageFiles
	var s, r int
	requestImageFiles = imageFiles{
		stat: func(path string) (int64, error) { s++; return before.stat(path) },
		read: func(path string, size int64) ([]byte, error) { r++; return before.read(path, size) },
	}
	t.Cleanup(func() { requestImageFiles = before })
	return &s, &r
}

// refMessages is n user messages, each carrying one picture of the given size as
// a reference, and the files behind them. Content is distinct per file.
func refMessages(t *testing.T, directory string, n int, size int64) []ai.Message {
	t.Helper()
	messages := make([]ai.Message, 0, n)
	for i := 0; i < n; i++ {
		path := filepath.Join(directory, "p"+strconv.Itoa(i)+".png")
		data := make([]byte, size)
		copy(data, strconv.Itoa(i))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		ref, _ := imageRefFromFile(path, "image/png", data)
		messages = append(messages, ai.Message{Role: "user", Content: []ai.ContentPart{
			{Type: "text", Text: "picture " + strconv.Itoa(i)}, ref.part(),
		}})
	}
	return messages
}

func countKinds(messages []ai.Message) (data, refs, placeholders int) {
	for _, message := range messages {
		for _, part := range message.Content {
			switch {
			case isImageRefPart(part):
				refs++
			case part.Type == "image_url" && part.ImageURL != nil:
				data++
			case part.Type == "text" && strings.HasPrefix(part.Text, "[image "):
				placeholders++
			}
		}
	}
	return
}

func TestReplayOfTwoHundredPicturesOpensNoFileAndHoldsOnlyReferences(t *testing.T) {
	directory := t.TempDir()
	var parts []journalPart
	for i := 0; i < 200; i++ {
		// The files do not exist: a replay that tried to open one would show it.
		parts = append(parts, journalPart{Type: journalPartImage, Path: filepath.Join(directory, "gone"+strconv.Itoa(i)+".png"), SHA256: strings.Repeat("a", 63) + strconv.Itoa(i%10), MIME: "image/png", Bytes: 8 << 20})
	}
	stats, reads := countFiles(t)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	message := replayedMessage(sessionEntry{Role: "user", Content: "two hundred", Parts: parts}, true)
	runtime.ReadMemStats(&after)
	if *stats != 0 || *reads != 0 {
		t.Fatalf("a replay touched the disk: %d stats, %d reads", *stats, *reads)
	}
	if _, refs, _ := countKinds([]ai.Message{message}); refs != 200 {
		t.Fatalf("replayed %d references, want 200", refs)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 1<<20 {
		t.Fatalf("replaying 200 pictures allocated %d bytes", grown)
	}
	// And a display read opens nothing either.
	if got := len(imagePartURLs(replayedMessage(sessionEntry{Role: "user", Content: "x", Parts: parts}, false))); got != 0 {
		t.Fatalf("a display read rebuilt %d pictures", got)
	}
}

func TestARequestCarriesTheNewestPicturesUnderItsByteBudgetAndKeepsEveryReference(t *testing.T) {
	directory := t.TempDir()
	transcript := refMessages(t, directory, 200, 1<<20)
	snapshot := make([]ai.Message, len(transcript))
	copy(snapshot, transcript)
	_, reads := countFiles(t)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	request := materializeImageRefs(transcript)
	runtime.ReadMemStats(&after)

	data, refs, placeholders := countKinds(request)
	if data != 40 || refs != 0 || placeholders != 160 || *reads != 40 {
		t.Fatalf("request carries %d pictures, %d references, %d placeholders after %d reads; want 40/0/160/40", data, refs, placeholders, *reads)
	}
	// The newest forty, in place, and the older ones named rather than missing.
	if len(imagePartURLs(request[199])) != 1 || len(imagePartURLs(request[160])) != 1 || len(imagePartURLs(request[159])) != 0 {
		t.Fatal("the budget was not spent on the newest pictures")
	}
	older := request[0].Content[1].Text
	if !strings.Contains(older, "p0.png") || !strings.Contains(older, "older than the pictures one request carries") {
		t.Fatalf("an older picture is %q, want it named and explained", older)
	}
	// The transcript is untouched: still two hundred references, none encoded.
	if _, refs, _ := countKinds(transcript); refs != 200 {
		t.Fatalf("the transcript holds %d references after a request, want 200", refs)
	}
	for i := range transcript {
		if &transcript[i].Content[0] != &snapshot[i].Content[0] {
			t.Fatal("the request wrote into the transcript")
		}
	}
	// 40 pictures of 1MiB are 40MiB decoded and about 54MiB encoded: the budget
	// is the bound, not the transcript.
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 160<<20 {
		t.Fatalf("one request allocated %d bytes for a 40MiB budget", grown)
	}
}

func TestARequestCarriesAtMostSixtyFourPictures(t *testing.T) {
	transcript := refMessages(t, t.TempDir(), 200, 100)
	_, reads := countFiles(t)
	data, _, placeholders := countKinds(materializeImageRefs(transcript))
	if data != maxRequestImages || *reads != maxRequestImages || placeholders != 200-maxRequestImages {
		t.Fatalf("%d pictures, %d reads, %d placeholders", data, *reads, placeholders)
	}
}

// THE LATEST PAIR IS NEVER THE ONE DROPPED, however much older history there is.
func TestTheLatestOriginalAndMarkedCopyAlwaysTravel(t *testing.T) {
	directory := t.TempDir()
	transcript := refMessages(t, directory, 100, 1<<20)
	pair := ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "mark-up"}}}
	for _, name := range []string{"original.png", "original.marked.png"} {
		path := filepath.Join(directory, name)
		data := make([]byte, 9<<20)
		copy(data, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		ref, _ := imageRefFromFile(path, "image/png", data)
		pair.Content = append(pair.Content, ref.part())
	}
	request := materializeImageRefs(append(transcript, pair))
	last := request[len(request)-1]
	if len(imagePartURLs(last)) != 2 || last.Content[0].Text != "mark-up" {
		t.Fatalf("the latest pair did not travel whole: %+v", last.Content)
	}
	// 18MiB of pair leaves room for twenty-two older 1MiB pictures.
	if data, _, _ := countKinds(request); data != 2+22 {
		t.Fatalf("request carries %d pictures, want the pair and the 22 newest older ones", data)
	}
}

func TestAMovedEditedOrUnreadableReferenceIsAPlaceholderAndNeverLeaks(t *testing.T) {
	directory := t.TempDir()
	transcript := refMessages(t, directory, 4, 64)
	if err := os.Remove(filepath.Join(directory, "p0.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "p1.png"), []byte("something else entirely"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "p2.png.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "image_url", ImageURL: &ai.ImageURLData{URL: imageRefScheme + "not-a-reference"}}}}
	request := materializeImageRefs(append(transcript, bad))
	data, refs, placeholders := countKinds(request)
	if data != 2 || refs != 0 || placeholders != 3 {
		t.Fatalf("%d pictures, %d references, %d placeholders; want 2/0/3", data, refs, placeholders)
	}
	for _, message := range request {
		for _, part := range message.Content {
			if part.ImageURL != nil && strings.HasPrefix(part.ImageURL.URL, imageRefScheme) {
				t.Fatal("a reference reached the request")
			}
		}
	}
	if text := request[0].Content[1].Text; !strings.Contains(text, "p0.png") || !strings.Contains(text, "file changed or gone") {
		t.Fatalf("gone file = %q", text)
	}
	raw, _ := json.Marshal(request)
	if strings.Contains(string(raw), imageRefScheme) {
		t.Fatal("the private scheme is in the serialized request")
	}
}

func TestATranscriptWithNoPicturesIsReturnedUntouched(t *testing.T) {
	messages := []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hi"}}}}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	out := materializeImageRefs(messages)
	runtime.ReadMemStats(&after)
	if &out[0] != &messages[0] || after.Mallocs-before.Mallocs > 2 {
		t.Fatal("a picture-free request was copied")
	}
}

// THE REAL SDK AND A FAKE PROVIDER: two marked messages in a row, a request
// budget of two pictures. The second request carries the second pair as image
// parts with the person's words, and the first pair is named, not missing.
func TestTheProviderRequestCarriesTheLatestPairAndNamesTheOlderOnes(t *testing.T) {
	oldImages := maxRequestImages
	maxRequestImages = 2
	t.Cleanup(func() { maxRequestImages = oldImages })

	var mu sync.Mutex
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !answersChatOnly(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, raw)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	workspace := t.TempDir()
	agent, err := New(Config{
		Workspace: workspace, Model: "vendor/vision-model", APIKey: "test", BaseURL: server.URL, System: "SYSTEM",
		SupportsImages: func(string) bool { return true },
		SessionFile:    filepath.Join(t.TempDir(), "session.jsonl"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()

	send := func(words string, shade uint8) {
		original := writePNG(t, workspace, words+".png", 8, 8, shade)
		marked := writePNG(t, workspace, words+".marked.png", 8, 8, shade+1)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, events, err := agent.SubmitComposed(ctx, Submission{
			Text:  words,
			Media: []SubmissionMedia{{ID: "o", Path: original}, {ID: "m", Path: marked}},
			Annotation: &Annotation{Original: "o", Marked: "m", ChangeOnlyMarked: true,
				Marks: []AnnotationMark{{Kind: "pin", At: &Point{X: 1, Y: 1}}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		for event := range events {
			if event.Kind == EventError {
				t.Fatalf("turn errored: %v", event.Err)
			}
		}
	}
	send("first", 10)
	send("second", 100)

	mu.Lock()
	defer mu.Unlock()
	// The requests are the turns' and the session's own errands (a title); the
	// second turn is the one that carries both messages and the system prompt.
	var turn []byte
	for _, body := range bodies {
		if strings.Contains(string(body), `"SYSTEM"`) && strings.Contains(string(body), "second") {
			turn = body
		}
	}
	if turn == nil {
		t.Fatalf("the provider saw %d requests and none was the second turn", len(bodies))
	}
	var wire struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(turn, &wire); err != nil {
		t.Fatal(err)
	}
	var users [][]struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	for _, message := range wire.Messages {
		if message.Role != "user" {
			continue
		}
		var content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL *struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if err := json.Unmarshal(message.Content, &content); err != nil {
			t.Fatalf("a picture message was flattened: %s", message.Content)
		}
		users = append(users, content)
	}
	if len(users) != 2 {
		t.Fatalf("the second request holds %d user messages, want both", len(users))
	}
	first, second := users[0], users[1]
	if first[0].Text != "first" || second[0].Text != "second" {
		t.Fatalf("the person's words are %q and %q", first[0].Text, second[0].Text)
	}
	if len(second) != 3 || second[1].ImageURL == nil || second[2].ImageURL == nil ||
		!strings.HasPrefix(second[1].ImageURL.URL, "data:image/png;base64,") || second[1].ImageURL.URL == second[2].ImageURL.URL {
		t.Fatalf("the latest pair did not travel as two distinct image parts: %+v", second)
	}
	if len(first) != 3 || first[1].ImageURL != nil || first[2].ImageURL != nil ||
		!strings.Contains(first[1].Text, "first.png") || !strings.Contains(first[2].Text, "first.marked.png") {
		t.Fatalf("the older pair is not named in the request: %+v", first)
	}
	if strings.Contains(string(turn), imageRefScheme) {
		t.Fatal("a reference reached the provider")
	}
}

// attemptLog records every disk door a request opens and the bytes it asked for.
func attemptLog(t *testing.T) (stats, reads *int, asked *int64) {
	t.Helper()
	before := requestImageFiles
	var s, r int
	var bytes int64
	requestImageFiles = imageFiles{
		stat: func(path string) (int64, error) { s++; return before.stat(path) },
		read: func(path string, size int64) ([]byte, error) { r++; bytes += size; return before.read(path, size) },
	}
	t.Cleanup(func() { requestImageFiles = before })
	return &s, &r, &bytes
}

// refsFor builds n reference messages whose recorded digest never matches (or
// whose files do not exist), with the size the journal recorded.
func badRefMessages(t *testing.T, directory string, n int, recorded int64, exist bool) []ai.Message {
	t.Helper()
	messages := make([]ai.Message, 0, n)
	for i := 0; i < n; i++ {
		path := filepath.Join(directory, "bad"+strconv.Itoa(i)+".png")
		if exist {
			if err := os.WriteFile(path, make([]byte, recorded), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		ref := imageRef{Path: path, MIME: "image/png", Bytes: recorded, SHA256: strings.Repeat("ab", 32)}
		messages = append(messages, ai.Message{Role: "user", Content: []ai.ContentPart{ref.part()}})
	}
	return messages
}

// FAILURES SPEND THE BUDGET TOO: a transcript of pictures that are gone or no
// longer match their digest cannot cost the filesystem more than a request that
// carries good ones.
func TestFailedPicturesSpendTheAttemptBudget(t *testing.T) {
	for name, build := range map[string]func(*testing.T, string) []ai.Message{
		"missing files":      func(t *testing.T, d string) []ai.Message { return badRefMessages(t, d, 200, 1<<20, false) },
		"changed 1MiB files": func(t *testing.T, d string) []ai.Message { return badRefMessages(t, d, 200, 1<<20, true) },
		"changed 8MiB files": func(t *testing.T, d string) []ai.Message { return badRefMessages(t, d, 200, 8<<20, false) },
		"sizes not recorded": func(t *testing.T, d string) []ai.Message { return badRefMessages(t, d, 10000, 0, false) },
	} {
		t.Run(name, func(t *testing.T) {
			transcript := build(t, t.TempDir())
			stats, reads, asked := attemptLog(t)
			request := materializeImageRefs(transcript)
			if *reads > maxRequestImages || *stats > maxRequestImages || *stats+*reads > maxRequestImages {
				t.Fatalf("%d stats and %d reads for %d bad pictures; a request may attempt %d", *stats, *reads, len(transcript), maxRequestImages)
			}
			if *asked > maxRequestImageBytes {
				t.Fatalf("a request asked the disk for %d bytes, over its %d", *asked, maxRequestImageBytes)
			}
			data, refs, placeholders := countKinds(request)
			if data != 0 || refs != 0 || placeholders != len(transcript) {
				t.Fatalf("%d pictures, %d references, %d placeholders for %d bad ones", data, refs, placeholders, len(transcript))
			}
		})
	}
}

// A sizes-recorded transcript that cannot load costs no stat at all: the walk is
// metadata only.
func TestARecordedSizeTranscriptTouchesNoDiskBeyondItsAttempts(t *testing.T) {
	transcript := badRefMessages(t, t.TempDir(), 10000, 1<<20, false)
	stats, reads, asked := attemptLog(t)
	materializeImageRefs(transcript)
	if *stats != 0 || *reads > 40 || *asked > maxRequestImageBytes {
		t.Fatalf("%d stats, %d reads, %d bytes for 10000 pictures of 1MiB", *stats, *reads, *asked)
	}
}

// THE LATEST PAIR SURVIVES a history of ruin behind it, because it is walked first.
func TestTheLatestPairTravelsPastAHistoryOfBadPictures(t *testing.T) {
	directory := t.TempDir()
	transcript := badRefMessages(t, directory, 200, 1<<20, false)
	pair := ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "mark-up"}}}
	for _, name := range []string{"o.png", "m.png"} {
		path := filepath.Join(directory, name)
		data := make([]byte, 4<<20)
		copy(data, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		ref, _ := imageRefFromFile(path, "image/png", data)
		pair.Content = append(pair.Content, ref.part())
	}
	_, reads, asked := attemptLog(t)
	request := materializeImageRefs(append(transcript, pair))
	if last := request[len(request)-1]; len(imagePartURLs(last)) != 2 {
		t.Fatalf("the latest pair did not travel: %+v", last.Content)
	}
	if *reads > maxRequestImages || *asked > maxRequestImageBytes {
		t.Fatalf("%d reads asking %d bytes", *reads, *asked)
	}
}

// A recorded size is the journal's word, and the journal can be edited: sizes at
// the edge of the integers must neither wrap the budget nor reach the disk.
func TestRecordedSizesCannotOverflowTheBudget(t *testing.T) {
	for _, size := range []int64{1<<63 - 1, 1<<63 - 2, maxImageBytes + 1, maxRequestImageBytes} {
		ref := imageRef{Path: "/x/a.png", MIME: "image/png", Bytes: size, SHA256: strings.Repeat("cd", 32)}
		other := imageRef{Path: "/x/b.png", MIME: "image/png", Bytes: 1 << 20, SHA256: strings.Repeat("ef", 32)}
		transcript := []ai.Message{
			{Role: "user", Content: []ai.ContentPart{ref.part()}},
			{Role: "user", Content: []ai.ContentPart{other.part()}},
		}
		stats, reads, asked := attemptLog(t)
		request := materializeImageRefs(transcript)
		if *asked > maxRequestImageBytes || *stats != 0 {
			t.Fatalf("size %d: %d stats, %d bytes asked", size, *stats, *asked)
		}
		if _, refs, _ := countKinds(request); refs != 0 {
			t.Fatalf("size %d left a reference in the request", size)
		}
		_ = reads
	}
}
