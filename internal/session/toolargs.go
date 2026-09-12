package session

// ONE DECODER FOR EVERY TOOL'S ARGUMENTS, AND WHY IT IS NOT encoding/json.
//
// A model called `tasks` with `{"limit":10.0}` eleven times in one turn. Every
// call came back with Go's own words — "json: cannot unmarshal number 10.0 into
// Go struct field tasksArguments.limit of type int" — and the model, which has
// never seen a Go struct and cannot see this one, read a sentence naming a type
// it could not spell, a field it had spelled correctly and a value it had every
// reason to think was a number. It could not act on it, so it sent the same call
// again. The turn ended on the loop guard.
//
// Two things were wrong and both are class faults:
//
//   - THE SCHEMA INVITED THE FORM THE DECODER REFUSED. `limit` was declared
//     `"type":"number"` and decoded into an `int`, and JSON has no integers —
//     several providers render every whole number as a float, so `10.0` is a
//     correct answer to the schema that was asked. Every whole-number argument
//     is declared `"type":"integer"` now (a structural test pins it), and this
//     decoder ACCEPTS a whole-valued float wherever an integer is wanted. 10.0
//     is ten. 10.5 is still refused, because a model that asked for half a row
//     meant something it did not say.
//
//   - THE REFUSAL WAS NOT ACTIONABLE. Go's message describes this program's
//     insides; the model needs the argument's own name, what arrived, what is
//     taken, and the call that would have worked. So every refusal here is one
//     short sentence in the model's own vocabulary — `limit takes a whole
//     number: send {"limit":10}, not 10.0` — and nothing in it names a Go type,
//     a struct or a package.
//
// EVERY TOOL DECODES THROUGH HERE. A structural test (toolargs_test.go) fails
// the build when a tool reaches for json.Unmarshal on its own arguments, because
// the value of one decoder is that a model learns ONE grammar of refusal and it
// holds on every hand.
//
// WHAT IS DELIBERATELY NOT DONE: a numeric string ("10") where a number is
// wanted is REFUSED, not coerced. No provider on this build has been observed
// sending one, and a decoder that quietly accepts a form nothing sends is a
// decoder inventing a dialect. The refusal names the corrected call, which is
// all a model that did send one would need.
//
// WHAT IS DONE, BECAUSE IT WAS OBSERVED — every loose form below was sent by a
// model on this build, and each is taken only because the value it carries is
// the value that was meant, spelled another way. A number in a cell of a text
// table is that number's spelling (coerceMap) — and only there: a number where a
// named text argument was wanted is still refused. A list or an object that
// arrives AS A JSON STRING holding its own JSON text — `"options":"[{\"key\":\"1\",…}]"` — is
// unwrapped once and read as the list it holds. deepseek-v4-flash sent `ask` that
// way three calls running on 2026-09-10, every one refused with "options takes a
// list", and the turn ended on the loop guard with the person never shown a
// question. The text inside was the right list; only its wrapping was wrong, and
// a decoder that can see the right list and refuses it anyway is a decoder that
// prefers its grammar to the person's question. The same model, more often,
// opens that quote and never closes it until the end of the arguments, so the
// list AND every field after it arrive inside one string (unswallowTail); that
// is read back as the object it was. A string whose contents are neither is
// still refused, in a sentence that says it arrived as text.
//
// Unknown fields stay ignored, exactly as they were: DisallowUnknownFields is
// off here as it was at all thirty-nine sites this replaced, and a key with no
// field behind it is passed through untouched rather than refused.

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// invalidArgumentsPrefix is how EVERY tool on the belt opens a refusal it wrote
// about its own arguments. It is a constant because two readers depend on the
// exact bytes: the model, which learns one shape of bad news, and the loop
// guard (looped.go), which tells an argument refusal apart from a failure out in
// the world and answers the two differently.
const invalidArgumentsPrefix = "Invalid arguments: "

// argumentRepairLimit bounds the sentence carried into a [nudge]. A repair is
// one line by construction, but a tool is free to write a paragraph, and a
// nudge that quoted a paragraph back would bury the correction it exists to
// deliver.
const argumentRepairLimit = 240

// toolArgumentError is a refusal a model can act on: the argument's name as the
// schema spells it, what arrived, and the corrected call.
//
// It carries no Go type, no struct name and no package. The [Error] text IS the
// repair sentence, so the thirty-nine call sites that already write
// "Invalid arguments: " + err.Error() print exactly the right thing without
// knowing this type exists.
type toolArgumentError struct {
	// field is the argument's dotted path as the schema spells it, or "" when
	// the fault is with the whole object rather than one argument.
	field string
	// repair is the whole sentence, and it is what [Error] answers.
	repair string
}

// A command that cannot be decoded is a model input error, not a permission
// question. Reject it without executing anything, before approval can ask a
// person to approve an absent command. Valid commands still use the full gate.
func invalidBashArguments(name string, args json.RawMessage) string {
	if name != "bash" {
		return ""
	}
	var parsed struct {
		Command string `json:"command"`
	}
	if err := decodeToolArguments(args, &parsed); err != nil {
		return invalidArgumentsPrefix + err.Error()
	}
	if strings.TrimSpace(parsed.Command) == "" {
		return invalidArgumentsPrefix + `command is required — send a JSON object with a non-empty "command" string`
	}
	return ""
}

func (e *toolArgumentError) Error() string { return e.repair }

// decodeToolArguments reads one tool call's arguments into a Go value, accepting
// the loose forms a provider legitimately sends and refusing the rest in words
// the model can act on.
//
// THE COMMON CASE COSTS NOTHING NEW. A well-formed call decodes on the first
// line, with no reflection and no second pass; everything below it runs only
// once encoding/json has already said no.
func decodeToolArguments(args json.RawMessage, into any) error {
	if err := json.Unmarshal(args, into); err == nil {
		return nil
	} else if syntax := (*json.SyntaxError)(nil); errors.As(err, &syntax) {
		// Bytes that are not JSON at all cannot be repaired by naming a field:
		// there are no fields yet.
		return &toolArgumentError{repair: "the arguments are not valid JSON — send one JSON object"}
	}
	// THE TARGET IS EMPTIED BEFORE THE SECOND PASS. encoding/json fills what it
	// can before it reports a type fault, so `into` is now half-written; a
	// second decode over the top of that would leave any field the repaired
	// bytes no longer mention holding the first pass's leavings.
	resetTarget(into)
	fixed, err := coerceArgument(args, reflect.TypeOf(into), "")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(fixed, into); err != nil {
		// A type that decodes itself may have already written the refusal in
		// this decoder's own words (Pick does); it is passed through whole.
		var own *toolArgumentError
		if errors.As(err, &own) {
			return own
		}
		return &toolArgumentError{field: mismatchField(err), repair: mismatchRepair(err)}
	}
	return nil
}

// resetTarget zeroes a settable pointer target. Anything else — a nil pointer, a
// non-pointer somebody passed by mistake — is left alone, because a decoder that
// panicked on a bad call site would take the turn with it.
func resetTarget(into any) {
	value := reflect.ValueOf(into)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return
	}
	target := value.Elem()
	if target.CanSet() {
		target.Set(reflect.Zero(target.Type()))
	}
}

// mismatchField names the argument encoding/json fell over on, when it named
// one. It is the schema's own spelling — encoding/json reports the JSON key —
// so it is safe to hand to a model.
func mismatchField(err error) string {
	mismatch := (*json.UnmarshalTypeError)(nil)
	if errors.As(err, &mismatch) {
		return mismatch.Field
	}
	return ""
}

// mismatchRepair is the last resort: a fault the walk below did not anticipate,
// said without Go's vocabulary. It names the argument when encoding/json named
// one, and otherwise says the only true thing left.
func mismatchRepair(err error) string {
	mismatch := (*json.UnmarshalTypeError)(nil)
	if errors.As(err, &mismatch) && mismatch.Field != "" {
		return mismatch.Field + " was sent as " + mismatch.Value + ", which it does not take"
	}
	return "the arguments could not be read — send one JSON object with this tool's arguments"
}

// ── the walk ────────────────────────────────────────────────────────────────

// coerceArgument rewrites one JSON value so that it fits the Go type waiting for
// it, or refuses it by name.
//
// It walks the TYPE and the BYTES together, which is what buys the field name in
// every refusal: encoding/json knows the name too but spells it into a sentence
// about a struct, and by the time an error reaches a caller the shape it came
// from is gone. path is the dotted route to here ("rails.max_per_day"), empty at
// the top.
func coerceArgument(raw json.RawMessage, t reflect.Type, path string) (json.RawMessage, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" || t == nil {
		// A JSON null leaves a Go value at its zero and always has; that is not a
		// fault and it is not this decoder's business to make it one.
		return raw, nil
	}
	// A type that decodes itself decides its own grammar. json.RawMessage is the
	// one that matters here — `tasks` takes its id raw on purpose, so that both
	// `7` and `"7"` mean task seven — and rewriting its bytes would be this
	// decoder overruling a tool's stated intent.
	if decodesItself(t) {
		return raw, nil
	}
	switch t.Kind() {
	case reflect.Pointer:
		return coerceArgument(raw, t.Elem(), path)
	case reflect.Struct:
		return coerceObject(raw, text, t, path)
	case reflect.Map:
		return coerceMap(raw, text, t, path)
	case reflect.Slice, reflect.Array:
		return coerceList(raw, text, t, path)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return coerceWholeNumber(raw, text, path)
	case reflect.Float32, reflect.Float64:
		return coerceFraction(raw, text, path)
	case reflect.String:
		return coerceText(raw, text, path)
	case reflect.Bool:
		return coerceTruth(raw, text, path)
	default:
		// An `any`, an interface, something exotic: it takes whatever arrives, so
		// there is nothing here to correct.
		return raw, nil
	}
}

// decodesItself reports whether this type (or a pointer to it) unmarshals
// itself.
func decodesItself(t reflect.Type) bool {
	unmarshaler := reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	return t.Implements(unmarshaler) || reflect.PointerTo(t).Implements(unmarshaler)
}

// coerceObject walks a JSON object against a struct, field by field. A key with
// no field behind it is copied through untouched, which is how unknown fields
// stay ignored.
func coerceObject(raw json.RawMessage, text string, t reflect.Type, path string) (json.RawMessage, error) {
	if !strings.HasPrefix(text, "{") {
		if inner, held := unwrapEncoded(raw, text, '{'); held {
			return coerceObject(inner, string(inner), t, path)
		}
		return nil, &toolArgumentError{field: path, repair: objectRepair(path) + arrivedAsText(text)}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, &toolArgumentError{field: path, repair: objectRepair(path)}
	}
	unswallowTail(fields, t)
	// The keys are walked in sorted order so that a rebuilt object is the same
	// bytes every time; nothing downstream reads the order, and a decoder whose
	// output moved would be a decoder nobody could write a test against.
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sortStrings(names)
	rebuilt := make(map[string]json.RawMessage, len(fields))
	for _, name := range names {
		field, known := structField(t, name)
		if !known {
			rebuilt[name] = fields[name]
			continue
		}
		fixed, err := coerceArgument(fields[name], field.Type, joinArgPath(path, name))
		if err != nil {
			return nil, err
		}
		rebuilt[name] = fixed
	}
	return remarshal(rebuilt, raw)
}

// coerceMap walks a JSON object against a Go map: every value takes the same
// type, and the keys are the model's own.
func coerceMap(raw json.RawMessage, text string, t reflect.Type, path string) (json.RawMessage, error) {
	if !strings.HasPrefix(text, "{") {
		if inner, held := unwrapEncoded(raw, text, '{'); held {
			return coerceMap(inner, string(inner), t, path)
		}
		return nil, &toolArgumentError{field: path, repair: objectRepair(path) + arrivedAsText(text)}
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, &toolArgumentError{field: path, repair: objectRepair(path)}
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sortStrings(names)
	rebuilt := make(map[string]json.RawMessage, len(entries))
	for _, name := range names {
		value := entries[name]
		// A CELL OF A TEXT TABLE TAKES A NUMBER AS ITS OWN SPELLING. `ask`'s
		// comparison axes are a map of text values, and a model that writes
		// `"complexity": 1` there has answered in the only way a number can be
		// written (2026-09-10, deepseek-v4-flash, `Complexity takes text … not
		// 1`); "1" is exactly what it meant, and the keys are its own, so there
		// is no named argument here for a number to be the wrong type of.
		if t.Elem().Kind() == reflect.String {
			if cell := strings.TrimSpace(string(value)); cell != "" && shape(cell) == shapeNumber {
				value = json.RawMessage(strconv.Quote(cell))
			}
		}
		fixed, err := coerceArgument(value, t.Elem(), joinArgPath(path, name))
		if err != nil {
			return nil, err
		}
		rebuilt[name] = fixed
	}
	return remarshal(rebuilt, raw)
}

// coerceList walks a JSON array against a slice or array. `depends_on` is the
// one that matters: a list of task ids, which a provider is as free to render
// `[7.0]` as `[7]`.
func coerceList(raw json.RawMessage, text string, t reflect.Type, path string) (json.RawMessage, error) {
	// A []byte is base64 text on the wire and never a list; leave it alone.
	if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		return raw, nil
	}
	if !strings.HasPrefix(text, "[") {
		if inner, held := unwrapEncoded(raw, text, '['); held {
			return coerceList(inner, string(inner), t, path)
		}
		return nil, &toolArgumentError{field: path, repair: listRepair(path) + arrivedAsText(text)}
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, &toolArgumentError{field: path, repair: listRepair(path)}
	}
	rebuilt := make([]json.RawMessage, len(items))
	for index, item := range items {
		fixed, err := coerceArgument(item, t.Elem(), path)
		if err != nil {
			return nil, err
		}
		rebuilt[index] = fixed
	}
	return remarshal(rebuilt, raw)
}

// coerceWholeNumber is THE FIX the owner's screen was asking for. A whole-valued
// float becomes the integer it already is; anything else is refused by name.
func coerceWholeNumber(raw json.RawMessage, text, path string) (json.RawMessage, error) {
	switch shape(text) {
	case shapeNumber:
		if _, err := strconv.ParseInt(text, 10, 64); err == nil {
			return raw, nil
		}
		if _, err := strconv.ParseUint(text, 10, 64); err == nil {
			return raw, nil
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsInf(value, 0) || math.IsNaN(value) || math.Abs(value) >= 1<<62 {
			return nil, &toolArgumentError{field: path, repair: notAWholeNumber(path, text)}
		}
		if value == math.Trunc(value) {
			// 10.0 is ten, 1e2 is a hundred. This is the whole of the coercion.
			return json.RawMessage(strconv.FormatInt(int64(value), 10)), nil
		}
		return nil, &toolArgumentError{
			field:  path,
			repair: wholeNumberRepair(path, strconv.FormatInt(int64(math.Trunc(value)), 10), text),
		}
	case shapeString:
		var sent string
		if err := json.Unmarshal(raw, &sent); err == nil {
			if _, err := strconv.ParseInt(strings.TrimSpace(sent), 10, 64); err == nil {
				return nil, &toolArgumentError{
					field:  path,
					repair: wholeNumberRepair(path, strings.TrimSpace(sent), text),
				}
			}
		}
		return nil, &toolArgumentError{field: path, repair: notAWholeNumber(path, text)}
	default:
		return nil, &toolArgumentError{field: path, repair: notAWholeNumber(path, text)}
	}
}

// coerceFraction guards the arguments that really are fractional — a spend
// ceiling, a length in seconds. Numbers pass; nothing else does.
func coerceFraction(raw json.RawMessage, text, path string) (json.RawMessage, error) {
	if shape(text) == shapeNumber {
		return raw, nil
	}
	if shape(text) == shapeString {
		var sent string
		if err := json.Unmarshal(raw, &sent); err == nil {
			if _, err := strconv.ParseFloat(strings.TrimSpace(sent), 64); err == nil {
				return nil, &toolArgumentError{
					field:  path,
					repair: leafName(path) + ` takes a number: send {"` + leafName(path) + `":` + strings.TrimSpace(sent) + `}, not ` + text,
				}
			}
		}
	}
	return nil, &toolArgumentError{field: path, repair: leafName(path) + " takes a number; " + text + " is not one"}
}

// coerceText guards a string argument. A number or a bell sent where words were
// wanted gets the quoted form back as the corrected call — and that stays a
// refusal for a NAMED text argument, because `bash {"command":17}` is not a
// command spelled oddly, it is a call that meant nothing, and running "17" after
// asking the person's permission for it would be worse than saying so
// (invalid_shell_consent_test.go). The one place a number IS its own spelling
// is a cell of a text table, and coerceMap takes it there.
func coerceText(raw json.RawMessage, text, path string) (json.RawMessage, error) {
	switch shape(text) {
	case shapeString:
		return raw, nil
	case shapeNumber, shapeBool:
		return nil, &toolArgumentError{
			field:  path,
			repair: leafName(path) + ` takes text: send {"` + leafName(path) + `":"` + text + `"}, not ` + text,
		}
	default:
		return nil, &toolArgumentError{field: path, repair: leafName(path) + " takes text"}
	}
}

// coerceTruth guards a boolean argument.
func coerceTruth(raw json.RawMessage, text, path string) (json.RawMessage, error) {
	if shape(text) == shapeBool {
		return raw, nil
	}
	return nil, &toolArgumentError{field: path, repair: leafName(path) + " takes true or false; " + text + " is not one"}
}

// ── the sentences ───────────────────────────────────────────────────────────

// wholeNumberRepair is the sentence the owner's screen should have shown:
//
//	limit takes a whole number: send {"limit":10}, not 10.0
//
// The argument by the name the schema gives it, the corrected call whole enough
// to copy, and what arrived — in that order, because the correction is the part
// the model has to act on.
func wholeNumberRepair(path, suggest, sent string) string {
	leaf := leafName(path)
	return leaf + ` takes a whole number: send {"` + leaf + `":` + suggest + `}, not ` + sent
}

// notAWholeNumber is what is said when there is no corrected call to offer:
// nothing here can be turned into the number that was wanted.
func notAWholeNumber(path, sent string) string {
	return leafName(path) + " takes a whole number; " + sent + " is not one"
}

func objectRepair(path string) string {
	if path == "" {
		return "the arguments must be one JSON object"
	}
	return leafName(path) + " takes an object"
}

func listRepair(path string) string {
	if path == "" {
		return "the arguments must be one JSON object"
	}
	return leafName(path) + " takes a list"
}

// unswallowTail repairs the one malformation a model has been seen produce
// again and again on a large nested call: it opens a QUOTE where a list or an
// object should begin and then writes the rest of the arguments — that value,
// every field after it and the closing brace — as one JSON-escaped string.
// What arrives is a valid object with three fields where nine were meant:
//
//	{"head":"…","kind":"choice","options":"[{…}], \"pick\": {…}, \"stakes\": \"costly\"}"}
//
// deepseek-v4-flash did this on eleven of eleven refused `ask` calls measured on
// 2026-09-10, and the fields inside the string were the person's whole question.
// The tell is exact: the string is not JSON on its own, but `{"options":` put in
// front of it closes into a well-formed object whose first field is the one the
// string sat in. When that is so, the object it was is read in place of the
// string: the field takes its real value and every other field it carried is
// added, unless the outer object already had one by that name — what the model
// wrote at the top level is never overwritten by what was inside the string.
// Only a field wanting a list, an object or a map is tried, because a text field
// can legitimately hold anything at all.
func unswallowTail(fields map[string]json.RawMessage, t reflect.Type) {
	for name, raw := range fields {
		text := strings.TrimSpace(string(raw))
		if !strings.HasPrefix(text, `"`) {
			continue
		}
		field, known := structField(t, name)
		if !known || !wantsAContainer(field.Type) {
			continue
		}
		var held string
		if err := json.Unmarshal(raw, &held); err != nil || json.Valid([]byte(held)) {
			// Empty, not a string, or a string that IS a JSON value on its own —
			// that last one is unwrapEncoded's case, not this one.
			continue
		}
		// The object is read with a decoder rather than Unmarshal because what
		// follows it is the model's spill and not the value: the same model,
		// having lost its place, closes the tail with `}]` (a bracket after the
		// brace, closing a list it was no longer in) or runs straight on into a
		// leaked control token and prose. Once the object has closed it is
		// whole, and nothing after it can add to it (the spill law, below).
		var tail map[string]json.RawMessage
		if err := json.NewDecoder(strings.NewReader(`{"` + name + `":` + heldText(held))).Decode(&tail); err != nil {
			continue
		}
		own, has := tail[name]
		if !has {
			continue
		}
		fields[name] = own
		for other, value := range tail {
			if _, present := fields[other]; !present {
				fields[other] = value
			}
		}
		// One string can only ever be the tail once.
		return
	}
}

// wantsAContainer reports whether a field's type is a list, an object or a
// map — the shapes a model has been seen open a quote in front of.
func wantsAContainer(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct, reflect.Map:
		return true
	case reflect.Slice, reflect.Array:
		return t.Elem().Kind() != reflect.Uint8
	}
	return false
}

// unwrapEncoded reads a JSON string that holds JSON text of the wanted shape —
// `"[{...}]"` where a list was asked for — and hands back the value it holds,
// so the caller can read it as the list or object it always was. It unwraps
// ONCE: the value inside is read by the same coercion that asked, so a string
// inside a string is refused there in the ordinary way, and a string holding
// anything but the wanted opener is not touched at all. What follows the first
// complete value is the model's spill (the spill law, below) and is dropped.
func unwrapEncoded(raw json.RawMessage, text string, opener byte) (json.RawMessage, bool) {
	if !strings.HasPrefix(text, `"`) {
		return nil, false
	}
	var held string
	if err := json.Unmarshal(raw, &held); err != nil {
		return nil, false
	}
	held = strings.TrimSpace(held)
	if held == "" || held[0] != opener {
		return nil, false
	}
	var first json.RawMessage
	if err := json.NewDecoder(strings.NewReader(heldText(held))).Decode(&first); err != nil {
		return nil, false
	}
	return first, true
}

// heldText is the one escape a model has been seen get wrong inside a string
// it wrapped by mistake: a backslash followed by a REAL newline, where a
// diagram's line break should have been `\n`. Inside JSON text a backslash
// before a newline is not an escape at all, and there is nothing else it could
// have meant, so it is read as the newline. No other bad escape is touched — a
// `\q` is still a refusal, because guessing at it would be inventing.
func heldText(held string) string {
	return strings.ReplaceAll(held, "\\\n", "\\n")
}

// THE SPILL LAW, which both repairs above share and which is stated once
// because it is a claim about a model and not about JSON: INSIDE A STRING A
// MODEL WRAPPED BY MISTAKE, WHAT FOLLOWS THE FIRST COMPLETE VALUE IS NOT PART
// OF THE VALUE. Measured 2026-09-10 on deepseek-v4-flash: a stray `]` after the
// closing brace; a leaked `<｜…｜>` control token and a paragraph of prose after
// a complete list; and, most often, nothing at all. A value that has closed is
// whole, and a decoder that refused it for what came after would be refusing
// the person's question over the model's stutter. Nothing is ever read OUT of
// the spill — a pick that fell into it is a pick the asker did not make.

// arrivedAsText is the clause added to a list or object refusal when what
// arrived was a JSON string, because "takes a list" alone reads as a lie to a
// model that can see a list right there inside the quotes. It shows the model
// the opening of what it sent and says the one thing to change.
func arrivedAsText(text string) string {
	if !strings.HasPrefix(text, `"`) {
		return ""
	}
	const glimpse = 24
	shown := text
	if runes := []rune(shown); len(runes) > glimpse {
		shown = string(runes[:glimpse]) + "…"
	}
	return "; it arrived as text, " + shown + " — send the value itself, not a string holding it"
}

// argumentRepair reads a tool result back and answers the repair sentence the
// tool wrote about its own arguments, if that is what the result is.
//
// IT IS THE LOOP GUARD'S EYES (looped.go). "Invalid arguments: " is the belt's
// one opening for a refusal a model can fix by sending different bytes, and a
// call refused that way twice is a different kind of stuck from a call that
// keeps failing out in the world — the correction is already in hand, so the
// guard can say it rather than counting to nine.
func argumentRepair(text string) (string, bool) {
	line := strings.TrimSpace(stripJobFooter(text))
	if !strings.HasPrefix(line, invalidArgumentsPrefix) {
		return "", false
	}
	repair := strings.TrimSpace(strings.TrimPrefix(line, invalidArgumentsPrefix))
	// One line: a tool that wrote a paragraph gets its first sentence carried,
	// because what follows a nudge is the model reading the correction, not an
	// essay about it.
	if cut := strings.IndexByte(repair, '\n'); cut >= 0 {
		repair = strings.TrimSpace(repair[:cut])
	}
	if len(repair) > argumentRepairLimit {
		repair = strings.TrimSpace(repair[:argumentRepairLimit]) + "…"
	}
	if repair == "" {
		return "", false
	}
	return repair, true
}

// ArgumentRefusal reports whether a tool result's text is a refusal the belt
// wrote about the call's own arguments: a sentence addressed to the model, one
// repair away from a working call, about a call that never reached the tool's
// work.
//
// IT ANSWERS WITH THE LOOP GUARD'S OWN EYES ([argumentRepair], looped.go), so
// the guard and a surface can never disagree about which failures are the
// schema talking to the model: a bash that failed out in the world is the
// person's business and opens itself, while a call refused on its arguments is
// a repair the model is already making, and the person reads the row's own
// words rather than the repair instruction.
func ArgumentRefusal(text string) bool {
	_, ok := argumentRepair(text)
	return ok
}

// ── small parts ─────────────────────────────────────────────────────────────

// The five shapes a JSON value can take, told apart by their first byte, which
// is all JSON needs.
const (
	shapeObject = iota
	shapeList
	shapeString
	shapeBool
	shapeNumber
)

func shape(text string) int {
	switch text[0] {
	case '{':
		return shapeObject
	case '[':
		return shapeList
	case '"':
		return shapeString
	case 't', 'f':
		return shapeBool
	default:
		return shapeNumber
	}
}

// leafName is the last segment of a dotted path — the name the model typed —
// and the whole path when there is nothing to cut.
func leafName(path string) string {
	if cut := strings.LastIndexByte(path, '.'); cut >= 0 {
		return path[cut+1:]
	}
	if path == "" {
		return "the argument"
	}
	return path
}

func joinArgPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// structField finds the field a JSON key belongs to, by the same rules
// encoding/json uses: the tag name first, then the field's own name, then a
// case-insensitive match. Embedded structs are searched through.
func structField(t reflect.Type, name string) (reflect.StructField, bool) {
	var fallback reflect.StructField
	found := false
	for index := 0; index < t.NumField(); index++ {
		field := t.Field(index)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if tag == "-" {
			continue
		}
		if field.Anonymous && tag == "" {
			inner := field.Type
			if inner.Kind() == reflect.Pointer {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct {
				if embedded, ok := structField(inner, name); ok {
					return embedded, true
				}
			}
			continue
		}
		spelling := tag
		if spelling == "" {
			spelling = field.Name
		}
		if spelling == name {
			return field, true
		}
		if !found && strings.EqualFold(spelling, name) {
			fallback, found = field, true
		}
	}
	return fallback, found
}

// remarshal re-encodes a rebuilt value, and falls back to the bytes it started
// from if that somehow fails — a decoder that could lose a call's arguments to
// its own repair would be worse than the fault it repairs.
func remarshal(value any, original json.RawMessage) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return original, nil
	}
	return encoded, nil
}

// sortStrings is an insertion sort, because these lists are a tool call's
// argument names — five of them, ten at the outside — and pulling in sort for
// that is a dependency bigger than the work.
func sortStrings(names []string) {
	for index := 1; index < len(names); index++ {
		for back := index; back > 0 && names[back] < names[back-1]; back-- {
			names[back], names[back-1] = names[back-1], names[back]
		}
	}
}
