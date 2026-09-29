package prose

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestInlineAngleBracketPlaceholdersStayInModelProse(t *testing.T) {
	rows := Render("GET /tasks/<id> and DELETE /tasks/<id>", Options{Width: 80})
	var got strings.Builder
	for _, row := range rows {
		got.WriteString(ansi.Strip(row))
	}
	if got.String() != "GET /tasks/<id> and DELETE /tasks/<id>" {
		t.Fatalf("angle brackets rendered as %q", got.String())
	}
}
