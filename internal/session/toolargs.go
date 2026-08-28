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
		return nil, &toolArgumentError{field: path, repair: objectRepair(path)}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, &toolArgumentError{field: path, repair: objectRepair(path)}
	}
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
		return nil, &toolArgumentError{field: path, repair: objectRepair(path)}
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
		fixed, err := coerceArgument(entries[name], t.Elem(), joinArgPath(path, name))
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
		return nil, &toolArgumentError{field: path, repair: listRepair(path)}
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
// wanted gets the quoted form back as the corrected call.
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
