package provider

import "testing"

// The shapes a model actually answers in when the router lands it on a
// provider without structured-output support. Every one of these was a paid
// call that returned nothing usable: `aforge do` lost its working method on
// 100% of runs against deepseek-flash, with the leaf then running silently
// degraded and nobody told.
func TestDecodeJSONObjectAcceptsWhatModelsActuallySend(t *testing.T) {
	for _, shape := range []struct {
		name string
		text string
	}{
		{"bare", `{"contract":"Read the changelog first."}`},
		{"fenced", "```json\n{\"contract\":\"Read the changelog first.\"}\n```"},
		{"fenced without a language", "```\n{\"contract\":\"Read the changelog first.\"}\n```"},
		{"prose before", `Based on the task, here is the working method: {"contract":"Read the changelog first."}`},
		{"prose either side", "Here it is:\n\n{\"contract\":\"Read the changelog first.\"}\n\nLet me know if you want it shorter."},
		{"fenced with prose around it", "Sure — here you go.\n\n```json\n{\"contract\":\"Read the changelog first.\"}\n```\n\nThat should do it."},
	} {
		t.Run(shape.name, func(t *testing.T) {
			var decoded struct {
				Contract string `json:"contract"`
			}
			if err := DecodeJSONObject(shape.text, &decoded); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded.Contract != "Read the changelog first." {
				t.Fatalf("contract = %q", decoded.Contract)
			}
		})
	}
}

// A brace inside a string must not end the object early, or a method that
// mentions one would be truncated into invalid JSON.
func TestDecodeJSONObjectKeepsBracesInsideStrings(t *testing.T) {
	var decoded struct {
		Contract string `json:"contract"`
	}
	text := `Here: {"contract":"write {\"ok\": true} to the file"} — done.`
	if err := DecodeJSONObject(text, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Contract != `write {"ok": true} to the file` {
		t.Fatalf("contract = %q", decoded.Contract)
	}
}

// Tolerance is not credulity. An answer with no object in it is still a
// failure, and it has to be one the caller can report as a format failure
// rather than silently accept as an empty result.
func TestDecodeJSONObjectStillRefusesWhatIsNotThere(t *testing.T) {
	var decoded struct {
		Contract string `json:"contract"`
	}
	for _, text := range []string{"", "   ", "I cannot help with that.", "```\n\n```"} {
		if err := DecodeJSONObject(text, &decoded); err == nil {
			t.Fatalf("decoded %q as if it were an object", text)
		}
	}
}
