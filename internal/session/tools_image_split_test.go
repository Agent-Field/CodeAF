package session

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// geminiCountRefusal is the router's answer, word for word, to n=4 on a model
// whose every provider takes exactly one — the text a person saw in the desk's
// picture panel on 2026-11-19.
const geminiCountRefusal = `No provider supports the requested parameters: aspect_ratio "1:1", output_format "png", n "4". ` +
	`Provider rejections: Google AI Studio: n: must be exactly 1 | Google: n: must be exactly 1`

// splitMedia answers each GenerateImage by its own script: answer gets the
// request and its 1-based call number and returns what that call returns. The
// calls are counted under a lock because a split call asks all at once.
type splitMedia struct {
	*scriptedMedia
	mu     sync.Mutex
	calls  int
	asked  []int
	answer func(request provider.ImageRequest, call int) (*provider.ImageResponse, error)
}

func (s *splitMedia) GenerateImage(_ context.Context, request provider.ImageRequest) (*provider.ImageResponse, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.asked = append(s.asked, request.N)
	s.mu.Unlock()
	return s.answer(request, call)
}

// pictures answers n pictures and a receipt of cost per picture.
func pictures(t *testing.T, n int, cost float64) *provider.ImageResponse {
	t.Helper()
	charged := cost * float64(n)
	response := &provider.ImageResponse{Usage: &ai.Usage{Cost: &charged}}
	for _, item := range encodedPictures(t, n) {
		response.Data = append(response.Data, provider.GeneratedImage{Base64: item, MediaType: "image/png"})
	}
	return response
}

func newSplitRig(t *testing.T, maxOutputs int, answer func(provider.ImageRequest, int) (*provider.ImageResponse, error)) (*Agent, *splitMedia, string) {
	t.Helper()
	media := &splitMedia{scriptedMedia: &scriptedMedia{}, answer: answer}
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	agent, _ := newMediaAgent(t, nil, func(config *Config) {
		config.Media = media
		config.ArtifactsIndex = index
		if maxOutputs > 0 {
			config.MediaOutputs = func(string) int { return maxOutputs }
		}
	})
	return agent, media, index
}

func runSplit(t *testing.T, agent *Agent, args string) (string, bool) {
	t.Helper()
	ctx := withCallID(withRunIdentity(context.Background()), "call_four")
	out, bad, err := beltTool(t, agent, "generate_image").Execute(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatalf("harness error: %v", err)
	}
	return out, bad
}

func splitFacts(index string) []*MediaFact {
	var out []*MediaFact
	for _, row := range ReadArtifacts(index) {
		if row.Media != nil {
			out = append(out, row.Media)
		}
	}
	return out
}

// A model whose catalog row says one picture per request is asked four times
// at once from the start: no refused request, four files, and every picture
// carries its own request's charge.
func TestAOnePerRequestModelIsAskedFourTimesForFour(t *testing.T) {
	agent, media, index := newSplitRig(t, 1, func(request provider.ImageRequest, _ int) (*provider.ImageResponse, error) {
		if request.N != 1 {
			return nil, &provider.APIError{Status: 400, Message: geminiCountRefusal}
		}
		return pictures(t, 1, 0.04), nil
	})
	out, bad := runSplit(t, agent, `{"prompt":"a marketing image","n":4,"aspect_ratio":"1:1"}`)
	if bad || media.calls != 4 || !reflect.DeepEqual(media.asked, []int{1, 1, 1, 1}) {
		t.Fatalf("bad=%v calls=%d asked=%v\n%s", bad, media.calls, media.asked, out)
	}
	for _, want := range []string{"Saved 4 of 4 requested images", "from 4 requests", "asked as 4 separate requests at once", "together they were charged $0.1600"} {
		if !strings.Contains(out, want) {
			t.Fatalf("result lacks %q:\n%s", want, out)
		}
	}
	if usage := agent.Usage(); math.Abs(usage.CostUSD-0.16) > 1e-9 {
		t.Fatalf("session cost = %v, want the four charges added", usage.CostUSD)
	}
	facts := splitFacts(index)
	if len(facts) != 4 {
		t.Fatalf("facts = %d", len(facts))
	}
	batches := map[string]bool{}
	for _, fact := range facts {
		if fact.CostUSD == nil || math.Abs(*fact.CostUSD-0.04) > 1e-9 || fact.CostScope != "" || fact.Requested != 4 || fact.Returned != 1 {
			t.Fatalf("a single-picture request's charge is that picture's: %+v", fact)
		}
		batches[fact.Batch] = true
	}
	if len(batches) != 4 {
		t.Fatalf("each request is its own batch, got %d", len(batches))
	}
}

// A model that published nothing is asked once; refused about the count, it is
// asked again ONCE as single pictures, and the result says why.
func TestARefusalAboutTheCountIsAskedAgainAsSingles(t *testing.T) {
	agent, media, _ := newSplitRig(t, 0, func(request provider.ImageRequest, _ int) (*provider.ImageResponse, error) {
		if request.N != 1 {
			return nil, &provider.APIError{Status: 400, Message: geminiCountRefusal}
		}
		return pictures(t, 1, 0.05), nil
	})
	out, bad := runSplit(t, agent, `{"prompt":"a marketing image","n":4}`)
	if bad || !reflect.DeepEqual(media.asked, []int{4, 1, 1, 1, 1}) {
		t.Fatalf("bad=%v asked=%v\n%s", bad, media.asked, out)
	}
	if !strings.Contains(out, "refused several pictures in one request, so they were asked as 4 separate requests") ||
		strings.Contains(out, "API error") {
		t.Fatalf("result:\n%s", out)
	}
	if usage := agent.Usage(); math.Abs(usage.CostUSD-0.2) > 1e-9 {
		t.Fatalf("session cost = %v", usage.CostUSD)
	}
}

// Three of four come back: the three are kept, the call is not a failure, and
// the one that did not is named with its reason.
func TestThreeOfFourKeepsTheThreeAndSaysOneFailed(t *testing.T) {
	agent, media, index := newSplitRig(t, 1, func(_ provider.ImageRequest, call int) (*provider.ImageResponse, error) {
		if call == 2 {
			return nil, &provider.APIError{Status: 502, Message: "upstream timed out"}
		}
		return pictures(t, 1, 0.04), nil
	})
	out, bad := runSplit(t, agent, `{"prompt":"a marketing image","n":4}`)
	if bad || media.calls != 4 {
		t.Fatalf("bad=%v calls=%d\n%s", bad, media.calls, out)
	}
	if !strings.Contains(out, "Saved 3 of 4 requested images") || !strings.Contains(out, "One request for one picture was not made") ||
		!strings.Contains(out, "upstream timed out") || !strings.Contains(out, "together they were charged $0.1200") {
		t.Fatalf("result:\n%s", out)
	}
	if got := len(splitFacts(index)); got != 3 {
		t.Fatalf("facts = %d", got)
	}
}

// Every request failing is a failure that says how it was asked.
func TestEveryRequestFailingIsOneFailure(t *testing.T) {
	agent, _, _ := newSplitRig(t, 1, func(provider.ImageRequest, int) (*provider.ImageResponse, error) {
		return nil, &provider.APIError{Status: 503, Message: "no capacity"}
	})
	out, bad := runSplit(t, agent, `{"prompt":"a marketing image","n":4}`)
	if !bad || !strings.Contains(out, "as 4 separate requests; none of them returned a picture") {
		t.Fatalf("bad=%v\n%s", bad, out)
	}
}

// A refusal about anything else is never retried, so money nobody approved is
// never spent on a guess.
func TestARefusalAboutSomethingElseIsNotRetried(t *testing.T) {
	agent, media, _ := newSplitRig(t, 0, func(provider.ImageRequest, int) (*provider.ImageResponse, error) {
		return nil, &provider.APIError{Status: 400, Message: `No provider supports the requested parameters: aspect_ratio "7:3", n "4". Provider rejections: Google: aspect_ratio: unsupported`}
	})
	out, bad := runSplit(t, agent, `{"prompt":"a marketing image","n":4}`)
	if !bad || media.calls != 1 || !strings.Contains(out, "nothing was retried") {
		t.Fatalf("bad=%v calls=%d\n%s", bad, media.calls, out)
	}
}

func TestImageRequestSizes(t *testing.T) {
	for _, c := range []struct {
		n, max int
		want   []int
	}{
		{4, 0, []int{4}}, {4, 1, []int{1, 1, 1, 1}}, {4, 4, []int{4}}, {4, 10, []int{4}},
		{5, 2, []int{2, 2, 1}}, {1, 1, []int{1}},
	} {
		if got := imageRequestSizes(c.n, c.max); !reflect.DeepEqual(got, c.want) {
			t.Errorf("sizes(%d, %d) = %v, want %v", c.n, c.max, got, c.want)
		}
	}
}

func TestImageCountRefused(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&provider.APIError{Status: 400, Message: geminiCountRefusal}, true},
		{errors.New(`invalid_request_error: n must be 1 for this model`), true},
		{errors.New(`Only n=1 is supported`), true},
		{&provider.APIError{Status: 400, Message: `No provider supports the requested parameters: aspect_ratio "7:3", n "4". Provider rejections: Google: aspect_ratio: unsupported`}, false},
		{os.ErrInvalid, false},
		{nil, false},
	} {
		if got := imageCountRefused(c.err); got != c.want {
			t.Errorf("imageCountRefused(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
