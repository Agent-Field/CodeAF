package placegraph

// Suggestion names an existing place the person may choose to tidy.
// It never changes the graph or requests a model response.
type Suggestion struct {
	Kind    string `json:"kind"`
	PlaceID string `json:"placeId"`
}

// Suggestions uses the same activity dates and durable snoozes as the existing
// untouched-place offer, so the two wire surfaces cannot disagree about its age.
func Suggestions(in StaleInput) []Suggestion {
	out := []Suggestion{}
	for _, place := range StalePlaces(in) {
		out = append(out, Suggestion{Kind: "mergeOrArchive", PlaceID: place.ID})
	}
	return out
}
