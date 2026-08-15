package head

import "github.com/Agent-Field/aforge-v2/internal/provider"

// decodeJSONObject reads a structured reply from a model. The tolerance it
// applies — a code fence, a sentence wrapped around the object — belongs to the
// provider boundary rather than to the head, so it lives there and every
// structured call in the system gets the same one.
func decodeJSONObject(text string, destination any) error {
	return provider.DecodeJSONObject(text, destination)
}
