package appapi

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type historyFilter struct {
	key, value string
	at         time.Time
	cents      int64
	op         string
}
type historyQuery struct{ filters []historyFilter }
type historyItem struct {
	kind, id, name, state, model, crew, crewName, said string
	pinned                                             bool
	at                                                 time.Time
	spent                                              *Money
}

// Quotes keep a phrase together without giving the app a second query grammar.
func parseHistoryQuery(raw string, now time.Time) (historyQuery, error) {
	q := historyQuery{}
	var token strings.Builder
	quote := rune(0)
	words := []string{}
	for _, r := range raw {
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				token.WriteRune(r)
			}
			continue
		}
		if (r == '"' || r == '\'') && (token.Len() == 0 || strings.HasSuffix(token.String(), ":")) {
			quote = r
			continue
		}
		if unicode.IsSpace(r) {
			if token.Len() > 0 {
				words = append(words, token.String())
				token.Reset()
			}
			continue
		}
		token.WriteRune(r)
	}
	if quote != 0 {
		return q, fmt.Errorf("query has an unfinished quote")
	}
	if token.Len() > 0 {
		words = append(words, token.String())
	}
	for _, word := range words {
		key, value, field := strings.Cut(word, ":")
		if !field {
			q.filters = append(q.filters, historyFilter{key: "text", value: strings.ToLower(word)})
			continue
		}
		key, value = strings.ToLower(key), strings.ToLower(value)
		if value == "" {
			return q, fmt.Errorf("%s needs a value", key)
		}
		f := historyFilter{key: key, value: value}
		switch key {
		case "state":
			if !agentState(value) && value != "open" && value != "pinned" && value != "active" && value != "wrapping_up" && value != "report_ready" && value != "queued" && value != "done" && value != "backlog" && value != "dropped" {
				return q, fmt.Errorf("unknown state: %s", value)
			}
		case "kind":
			if value != "agent" && value != "crew" && value != "job" && value != "file" {
				return q, fmt.Errorf("unknown kind: %s", value)
			}
		case "pinned":
			if value != "true" && value != "false" {
				return q, fmt.Errorf("pinned needs true or false")
			}
		case "said", "crew", "model":
		case "since":
			var err error
			f.at, err = time.Parse("2006-01-02", value)
			if err != nil && strings.HasSuffix(value, "d") {
				days, e := strconv.Atoi(strings.TrimSuffix(value, "d"))
				if e == nil && days > 0 && days <= 36500 {
					f.at = now.AddDate(0, 0, -days)
					err = nil
				}
			}
			if err != nil {
				return q, fmt.Errorf("since needs a UTC date (YYYY-MM-DD) or a positive day count such as 7d")
			}
		case "cost":
			f.op = "="
			for _, op := range []string{">=", "<=", ">", "<", "="} {
				if strings.HasPrefix(value, op) {
					f.op = op
					value = strings.TrimPrefix(value, op)
					break
				}
			}
			var err error
			f.cents, err = historyCents(strings.TrimPrefix(value, "$"))
			if err != nil {
				return q, fmt.Errorf("cost needs a nonnegative dollar amount with at most two decimals")
			}
		default:
			return q, fmt.Errorf("unsupported history filter: %s", key)
		}
		q.filters = append(q.filters, f)
	}
	return q, nil
}
func historyCents(raw string) (int64, error) {
	whole, fraction, dot := strings.Cut(raw, ".")
	if whole == "" || len(whole) > 9 || len(fraction) > 2 || (dot && fraction == "") {
		return 0, fmt.Errorf("invalid money")
	}
	for _, r := range whole + fraction {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid money")
		}
	}
	n, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, err
	}
	fraction += strings.Repeat("0", 2-len(fraction))
	cents, _ := strconv.ParseInt(fraction, 10, 64)
	return n*100 + cents, nil
}
func (q historyQuery) text() string {
	words := []string{}
	for _, f := range q.filters {
		if f.key == "text" {
			words = append(words, f.value)
		}
	}
	return strings.Join(words, " ")
}
func (q historyQuery) needsSaid() bool {
	for _, f := range q.filters {
		if f.key == "said" {
			return true
		}
	}
	return false
}
func (q historyQuery) match(item historyItem) bool {
	for _, f := range q.filters {
		switch f.key {
		case "text":
			if !strings.Contains(strings.ToLower(item.name), f.value) {
				return false
			}
		case "kind":
			if item.kind != f.value {
				return false
			}
		case "state":
			switch f.value {
			case "open":
				if searchClosed(item.state) {
					return false
				}
			case "pinned":
				if !item.pinned {
					return false
				}
			default:
				if item.state != f.value {
					return false
				}
			}
		case "pinned":
			if item.pinned != (f.value == "true") {
				return false
			}
		case "crew":
			if strings.ToLower(item.crew) != f.value && !strings.Contains(strings.ToLower(item.crewName), f.value) {
				return false
			}
		case "model":
			if !strings.Contains(strings.ToLower(item.model), f.value) {
				return false
			}
		case "said":
			if !strings.Contains(strings.ToLower(item.said), f.value) {
				return false
			}
		case "since":
			if item.at.IsZero() || item.at.Before(f.at) {
				return false
			}
		case "cost":
			// An unknown amount cannot satisfy a cost filter, including cost:0.
			if item.spent == nil {
				return false
			}
			n, err := historyCents(item.spent.USD)
			if err != nil {
				return false
			}
			switch f.op {
			case ">":
				if n <= f.cents {
					return false
				}
			case ">=":
				if n < f.cents {
					return false
				}
			case "<":
				if n >= f.cents {
					return false
				}
			case "<=":
				if n > f.cents {
					return false
				}
			case "=":
				if n != f.cents {
					return false
				}
			}
		}
	}
	return true
}

func (q historyQuery) needsCost() bool {
	for _, f := range q.filters {
		if f.key == "cost" {
			return true
		}
	}
	return false
}
