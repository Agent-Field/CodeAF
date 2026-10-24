package executor

import (
	"strings"
	"testing"
)

func TestProfileGrantsOnlyWorkspaceAndTmp(t *testing.T) {
	p := scope{root: "/w/root", tmp: "/t/private", net: NetPolicy{}}.profile()
	for _, want := range []string{"(deny default)", `(subpath "/w/root")`, `(subpath "/t/private")`, "(deny network*)"} {
		if !strings.Contains(p, want) {
			t.Errorf("profile lacks %s:\n%s", want, p)
		}
	}
	if strings.Contains(p, "(allow network*)") || strings.Contains(p, "deny file-read* file-write*") {
		t.Errorf("profile grants or hides too much:\n%s", p)
	}
}

func TestProfileOpensNetworkOnPolicy(t *testing.T) {
	p := scope{root: "/w", tmp: "/t", net: openNet}.profile()
	if !strings.Contains(p, "(allow network*)") || strings.Contains(p, "(deny network*)") {
		t.Fatalf("open policy did not open the network:\n%s", p)
	}
}

func TestProfileHidesLast(t *testing.T) {
	p := scope{root: "/w", tmp: "/t", hidden: []string{"/w/.cell", "/store"}}.profile()
	hide := strings.Index(p, "(deny file-read* file-write*")
	if hide < strings.LastIndex(p, "(allow ") || !strings.Contains(p, `(subpath "/store")`) {
		t.Fatalf("hidden paths must be the last rules:\n%s", p)
	}
}

func TestProfileQuotesPaths(t *testing.T) {
	p := scope{root: `/w/a"b\c`, tmp: "/t"}.profile()
	if !strings.Contains(p, `(subpath "/w/a\"b\\c")`) {
		t.Fatalf("path not escaped:\n%s", p)
	}
}
