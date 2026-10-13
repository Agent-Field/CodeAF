package runs

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

func eval(expr string, env map[string]any) (any, error) {
	p := &parser{s: strings.TrimSpace(expr), env: env}
	if p.s == "" {
		return nil, nil
	}
	value, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("trailing %q", p.s[p.i:])
	}
	return value, nil
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != "" && typed != "false"
	case float64:
		return typed != 0
	case int:
		return typed != 0
	default:
		return true
	}
}

type parser struct {
	s   string
	i   int
	env map[string]any
}

func (p *parser) parseOr() (any, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.consume("||") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		if truthy(left) || truthy(right) {
			left = true
			continue
		}
		left = false
	}
	return left, nil
}

func (p *parser) parseAnd() (any, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.consume("&&") {
		if !truthy(left) {
			if _, err := p.parseNot(); err != nil {
				return nil, err
			}
			left = false
			continue
		}
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = truthy(right)
	}
	return left, nil
}

func (p *parser) parseNot() (any, error) {
	p.skip()
	if p.consume("!") {
		value, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return !truthy(value), nil
	}
	return p.parseCmp()
}

func (p *parser) parseCmp() (any, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	op := p.operator()
	if op == "" {
		return left, nil
	}
	right, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	return compare(left, op, right), nil
}

func (p *parser) parsePrimary() (any, error) {
	p.skip()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("unexpected end")
	}
	switch p.s[p.i] {
	case '(':
		p.i++
		value, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		p.skip()
		if p.i >= len(p.s) || p.s[p.i] != ')' {
			return nil, fmt.Errorf("missing )")
		}
		p.i++
		return value, nil
	case '\'', '"':
		return p.parseString()
	case '$':
		p.i++
		return lookup(p.readPath(), p.env), nil
	}
	if p.s[p.i] == '-' || unicode.IsDigit(rune(p.s[p.i])) {
		if value, ok := p.parseNumber(); ok {
			return value, nil
		}
	}
	path := p.readPath()
	if path == "true" {
		return true, nil
	}
	if path == "false" {
		return false, nil
	}
	p.skip()
	if p.i < len(p.s) && p.s[p.i] == '(' {
		return p.parseCall(path)
	}
	return lookup(path, p.env), nil
}

func (p *parser) parseCall(name string) (any, error) {
	p.i++ // (
	var args []any
	for {
		p.skip()
		if p.i < len(p.s) && p.s[p.i] == ')' {
			p.i++
			break
		}
		arg, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		p.skip()
		if p.consume(",") {
			continue
		}
		if p.i < len(p.s) && p.s[p.i] == ')' {
			p.i++
			break
		}
		return nil, fmt.Errorf("missing )")
	}
	if name != "changed" {
		return nil, fmt.Errorf("unknown function %s", name)
	}
	changed, _ := p.env["_changed"].(map[string]bool)
	if len(args) == 0 || changed == nil {
		return false, nil
	}
	field, _ := args[0].(string)
	return changed[field], nil
}

func (p *parser) parseString() (any, error) {
	quote := p.s[p.i]
	p.i++
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != quote {
		p.i++
	}
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("unterminated string")
	}
	text := p.s[start:p.i]
	p.i++
	return text, nil
}

func (p *parser) parseNumber() (float64, bool) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	saw := false
	for p.i < len(p.s) && (unicode.IsDigit(rune(p.s[p.i])) || p.s[p.i] == '.') {
		saw = true
		p.i++
	}
	if !saw {
		p.i = start
		return 0, false
	}
	value, err := parseFloat(p.s[start:p.i])
	if err != nil {
		p.i = start
		return 0, false
	}
	return value, true
}

func (p *parser) readPath() string {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c)) || c == '_' || c == '.' {
			p.i++
			continue
		}
		break
	}
	return p.s[start:p.i]
}

func (p *parser) operator() string {
	p.skip()
	for _, op := range []string{"==", "!=", "<=", ">=", "<", ">"} {
		if strings.HasPrefix(p.s[p.i:], op) {
			p.i += len(op)
			return op
		}
	}
	return ""
}

func (p *parser) consume(token string) bool {
	p.skip()
	if strings.HasPrefix(p.s[p.i:], token) {
		p.i += len(token)
		return true
	}
	return false
}

func (p *parser) skip() {
	for p.i < len(p.s) && unicode.IsSpace(rune(p.s[p.i])) {
		p.i++
	}
}

func lookup(path string, env map[string]any) any {
	path = strings.TrimPrefix(path, "$")
	if path == "" {
		return nil
	}
	var cur any = env
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[part]
	}
	return cur
}

func render(text string, env map[string]any) string {
	var b strings.Builder
	for {
		start := strings.Index(text, "{{")
		if start < 0 {
			b.WriteString(text)
			return b.String()
		}
		end := strings.Index(text[start:], "}}")
		if end < 0 {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:start])
		b.WriteString(asText(lookup(strings.TrimSpace(text[start+2:start+end]), env)))
		text = text[start+end+2:]
	}
}

func resolveValue(value any, env map[string]any) any {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, "$") && !strings.ContainsAny(typed, " \t") {
			return lookup(typed, env)
		}
		if strings.Contains(typed, "{{") {
			return render(typed, env)
		}
		return typed
	case map[string]any:
		return resolveMap(typed, env)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = resolveValue(item, env)
		}
		return out
	default:
		return value
	}
}

func resolveMap(in map[string]any, env map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = resolveValue(value, env)
	}
	return out
}

func compare(left any, op string, right any) bool {
	if lf, ok := asFloat(left); ok {
		if rf, ok := asFloat(right); ok {
			return cmpFloat(lf, op, rf)
		}
	}
	ls, rs := asText(left), asText(right)
	switch op {
	case "==":
		return ls == rs
	case "!=":
		return ls != rs
	case "<":
		return ls < rs
	case ">":
		return ls > rs
	case "<=":
		return ls <= rs
	case ">=":
		return ls >= rs
	default:
		return false
	}
}

func cmpFloat(left float64, op string, right float64) bool {
	switch op {
	case "==":
		return left == right
	case "!=":
		return left != right
	case "<":
		return left < right
	case ">":
		return left > right
	case "<=":
		return left <= right
	case ">=":
		return left >= right
	default:
		return false
	}
}

func asText(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func asFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		n, err := parseFloat(typed)
		return n, err == nil
	default:
		return 0, false
	}
}

func parseFloat(text string) (float64, error) {
	var n float64
	_, err := fmt.Sscan(text, &n)
	return n, err
}

func parseDur(text string) (time.Duration, bool) {
	if text == "" {
		return 0, false
	}
	n := 0
	i := 0
	for i < len(text) && text[i] >= '0' && text[i] <= '9' {
		n = n*10 + int(text[i]-'0')
		i++
	}
	if i == 0 || i != len(text)-1 {
		return 0, false
	}
	switch text[i] {
	case 's':
		return time.Duration(n) * time.Second, true
	case 'm':
		return time.Duration(n) * time.Minute, true
	case 'h':
		return time.Duration(n) * time.Hour, true
	case 'd':
		return time.Duration(n) * 24 * time.Hour, true
	default:
		return 0, false
	}
}
