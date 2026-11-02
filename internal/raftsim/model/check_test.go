package model

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anishathalye/porcupine"
)

func TestR3R4UnknownTimeoutCanCommitAfterCallerStoppedWaiting(t *testing.T) {
	create := Call{Kind: "create", Actor: "a", Cell: "c", Head: "h"}
	won := Outcome{Code: "ok", Cell: Cell{Holder: "a", Fence: 1, Until: 90000, Head: "h"}}
	for _, known := range []bool{false, true} {
		history := []Operation{{ID: 0, Call: 1, Return: 2, Input: create, Unknown: true}, {ID: 1, Call: 3, Return: 4, Input: Call{Kind: "read", Actor: "a", Cell: "c"}, Output: Outcome{Code: "not_found"}}}
		if known {
			history[1].Output = won
		}
		if Check(history) != porcupine.Ok {
			t.Fatal("timeout was treated as a failed or completed-before-timeout operation", known)
		}
	}
	// Deleting the unknown operation would make this legitimate history illegal.
	if Check([]Operation{{Call: 1, Return: 2, Input: Call{Kind: "read", Cell: "c"}, Output: won}}) != porcupine.Illegal {
		t.Fatal("unexplained read was accepted")
	}
}
func TestR1R4WholeIdentityOrderingAndCheckerDeadline(t *testing.T) {
	s, create := Step(State{}, Call{Kind: "create", Actor: "a", Cell: "c", Head: "h"})
	_, take := Step(s, Call{Kind: "acquire", Actor: "b", Cell: "c", Expected: create.Cell, Force: true})
	history := []Operation{{Call: 1, Return: 2, Input: Call{Kind: "create", Actor: "a", Cell: "c", Head: "h"}, Output: create}, {Call: 3, Return: 4, Input: Call{Kind: "acquire", Actor: "b", Cell: "c", Expected: create.Cell, Force: true}, Output: take}, {Call: 5, Return: 6, Input: Call{Kind: "read", Actor: "a", Cell: "c"}, Output: create}}
	if Check(history) != porcupine.Illegal || len(Minimize(history)) == 0 {
		t.Fatal("stale completed read escaped")
	}
	for _, budget := range []time.Duration{0, -1, time.Second + 1} {
		if CheckWithin(history, budget) != porcupine.Unknown {
			t.Fatal("invalid/unbounded checker budget accepted")
		}
	}
	if Check(make([]Operation, 101)) != porcupine.Unknown {
		t.Fatal("unbounded history admitted")
	}
}
func TestR17SessionsKeepExactResultAndClosedTombstone(t *testing.T) {
	s := State{}
	for i := 0; i < 128; i++ {
		call := Call{Kind: "advance_time", Actor: "a", Session: strconv.Itoa(i), Sequence: 1, Digest: "d", At: int64(i)}
		var out Outcome
		s, out = Step(s, call)
		if out.Code != "ok" {
			t.Fatal(i, out)
		}
	}
	full := Call{Kind: "advance_time", Actor: "a", Session: "full", Sequence: 1, Digest: "d"}
	if _, out := Step(s, full); out.Code != "capacity" {
		t.Fatal(out)
	}
	retry := Call{Kind: "advance_time", Actor: "a", Session: "0", Sequence: 1, Digest: "d", At: 9000}
	original := s.Sessions["a/0"].Result
	next, out := Step(s, retry)
	if out != original || next.Now != s.Now {
		t.Fatal("retry was re-executed")
	}
	for _, call := range []Call{{Kind: "advance_time", Actor: "a", Session: "0", Sequence: 1, Digest: "changed"}, {Kind: "advance_time", Actor: "a", Session: "0", Sequence: 2, Digest: "new"}, {Kind: "advance_time", Actor: "a", Session: "0", Sequence: 3, Digest: "new", Ack: 1}} {
		if _, out := Step(s, call); out.Code != "conflict" {
			t.Fatal(out)
		}
	}
	s, out = Step(s, Call{Kind: "session_close", Actor: "a", Session: "0", Sequence: 2, Ack: 1, Digest: "close"})
	if out.Code != "ok" {
		t.Fatal(out)
	}
	if _, out := Step(s, retry); out.Code != "conflict" {
		t.Fatal("closed session ID reused")
	}
	if _, out := Step(s, full); out.Code != "ok" {
		t.Fatal("closing did not free a live-session slot")
	}
}

// R1/R4/R17 require an independent oracle. AST inspection keeps a future
// refactor from silently sharing the production rules with the checker.
func TestR1R4R17OracleImportsNoProductionRules(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.ImportSpec)
			if !ok {
				return true
			}
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(path, "codeaf/internal/") || strings.Contains(path, "etcd.io/") {
				t.Fatal("oracle imports production state", path)
			}
			return true
		})
	}
}
