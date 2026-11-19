package session

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// A REQUEST FOR SEVERAL PICTURES IS SPLIT WHEN THE MODEL DRAWS FEWER PER CALL.
//
// generate_image's n is how many pictures the CALLER wants, and it used to be
// sent as one request whatever the model could do. Every Gemini image model the
// router lists publishes `n` with a maximum of 1, and its providers refuse
// anything else with a 400 ("n: must be exactly 1"), so "make four" on the most
// common picture model failed outright and showed a person the router's raw
// rejection. Asking four times for one picture is what the person meant, and it
// costs what four pictures cost — the price they already accepted by asking for
// four.
//
// Two ways in, in this order of trust:
//
//  1. The catalog's own fact ([ImageGen.MaxOutputs], read from the router's
//     published `n` knob). A model that publishes a maximum below n is asked in
//     requests of at most that many, all at once, before anything is refused.
//  2. Nothing published, and the one request is refused SPECIFICALLY about the
//     count ([imageCountRefused]). The call is asked again ONCE, as n
//     single-picture requests. A refusal about anything else is the caller's
//     to read and is never retried: asking again for a reason nobody can name
//     would spend money nobody approved.
//
// Each request is billed on its own and each bill is recorded on its own; the
// result says how many requests went out and what they cost together.

// imageRequestSizes is how many pictures each request asks for: one request of
// n when the model's maximum is unknown or big enough, otherwise requests of at
// most max, the last one carrying the remainder.
func imageRequestSizes(requested, max int) []int {
	if max <= 0 || max >= requested {
		return []int{requested}
	}
	var sizes []int
	for left := requested; left > 0; left -= max {
		sizes = append(sizes, min(left, max))
	}
	return sizes
}

// imageSingles is n requests of one picture each.
func imageSingles(requested int) []int {
	sizes := make([]int, requested)
	for i := range sizes {
		sizes[i] = 1
	}
	return sizes
}

// imageCountPattern matches the ways a provider says the COUNT was the problem:
// OpenRouter's per-provider list ("Google: n: must be exactly 1"), an OpenAI
// style "n must be 1" or "only n=1 is supported", and the words themselves. It
// deliberately does not match `n "4"` alone, which OpenRouter prints in its
// list of requested parameters whatever the refusal was actually about.
var imageCountPattern = regexp.MustCompile(`(?i)(\bn\s*:\s*must\b|\bn\s+must\b|\bn\s*=\s*1\b|['"]n['"]\s*(must|is|:)|number of (images|outputs)|\bmultiple (images|outputs)\b.*not supported)`)

// imageCountRefused says the provider refused this request because of how many
// pictures it asked for, and for nothing else this build can see. Only a
// client-side refusal (4xx) qualifies: a 5xx is the provider having a bad
// minute, which a second request of a different shape would not fix.
func imageCountRefused(err error) bool {
	if err == nil {
		return false
	}
	var refusal *provider.APIError
	if errors.As(err, &refusal) && (refusal.Status < 400 || refusal.Status >= 500) {
		return false
	}
	return imageCountPattern.MatchString(err.Error())
}

// imageReply is one request's answer: the provider's response, or the error.
type imageReply struct {
	response *provider.ImageResponse
	err      error
}

// sendImageRequests sends one request per entry of sizes, all at once, and
// waits for every one of them. The order of the replies is the order of sizes,
// never the order they finished in, so the files are numbered the same way
// however the network raced.
func sendImageRequests(ctx context.Context, client MediaGenerator, request provider.ImageRequest, sizes []int) []imageReply {
	replies := make([]imageReply, len(sizes))
	if len(sizes) == 1 {
		request.N = sizes[0]
		response, err := client.GenerateImage(ctx, request)
		replies[0] = imageReply{response, err}
		return replies
	}
	var wait sync.WaitGroup
	for i, n := range sizes {
		wait.Add(1)
		go func() {
			defer wait.Done()
			one := request
			one.N = n
			response, err := client.GenerateImage(ctx, one)
			replies[i] = imageReply{response, err}
		}()
	}
	wait.Wait()
	return replies
}

// sumImageUsage adds the receipts of every request that answered into the one
// figure the call's render record carries. A request that answered with no
// cost makes the total's cost UNKNOWN rather than a smaller number: a figure
// that leaves out a charge it cannot name would read as the whole bill. Nil
// when nothing answered.
func sumImageUsage(replies []imageReply) *ai.Usage {
	var total *ai.Usage
	cost, costKnown := 0.0, true
	for _, reply := range replies {
		if reply.response == nil {
			continue
		}
		if total == nil {
			total = &ai.Usage{}
		}
		usage := reply.response.Usage
		if usage == nil || usage.Cost == nil || math.IsNaN(*usage.Cost) || math.IsInf(*usage.Cost, 0) || *usage.Cost < 0 {
			costKnown = false
		} else {
			cost += *usage.Cost
		}
		if usage != nil {
			total.PromptTokens += usage.PromptTokens
			total.CompletionTokens += usage.CompletionTokens
			total.TotalTokens += usage.TotalTokens
		}
	}
	if total != nil && costKnown {
		total.Cost = &cost
	}
	return total
}

// describeImageSplit is the closing line of a call that went out as several
// requests: how many, why, and what they cost together when every one of them
// said. The why is the one the road acted on, so a caller reading it learns
// what this model does and does not have to ask for one at a time next time.
func describeImageSplit(model string, requests int, retried bool, total *ai.Usage) string {
	var out strings.Builder
	if retried {
		fmt.Fprintf(&out, "%s refused several pictures in one request, so they were asked as %d separate requests at once", model, requests)
	} else {
		fmt.Fprintf(&out, "%s returns fewer pictures per request than were asked for, so they were asked as %d separate requests at once", model, requests)
	}
	if total != nil && total.Cost != nil && *total.Cost > 0 {
		fmt.Fprintf(&out, "; together they were charged $%.4f", *total.Cost)
	}
	out.WriteString(".")
	return out.String()
}
