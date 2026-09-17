// The doc command: the billed document parse the belt's read_document tool
// runs, printed straight.
//
// The tool exists because a scanned PDF is exactly what plain read cannot
// open; this command exists for the same reason `manual` does one door over —
// the belt has a hand a person scripting a fix had no way to reach. Both run
// the same road (internal/session's [session.ReadDocument]): the same guards,
// the same local rung for a PDF with a text layer, the same billed rungs on
// the profile's key, the same refusals. What this door adds is the shell
// grammar, --pages on the local rung, and a plain file printed as-is with no
// call and no bill.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func runDoc(args []string) error {
	flags := commandFlags("doc")
	question := flags.String("q", "", "what you need from it; shapes the billed rungs only, the way the tool's question does")
	pages := flags.String("pages", "", "pages A-B of a PDF the local rung reads, one-based and inclusive")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}

	positionals := flags.Args()
	if len(positionals) != 1 || strings.TrimSpace(positionals[0]) == "" {
		return errors.New("name one document to read")
	}
	pagesFrom, pagesTo, err := parsePageRange(*pages)
	if err != nil {
		return err
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}
	// A billed rung records its own usage row on the way through; this process
	// must outlive the ledger's write queue for the row to land.
	defer session.FlushUsage()

	answer, err := session.ReadDocument(context.Background(), session.DocumentRead{
		Path:      positionals[0],
		Question:  *question,
		Engine:    settings.DocumentEngine,
		Workspace: mustWorkspace(),
		PagesFrom: pagesFrom,
		PagesTo:   pagesTo,
		Parser: func() (session.DocumentParser, error) {
			// The belt builds its parser from the session's own account on
			// first use; this is the same construction with the profile's
			// account ([session.NewDocumentParser]).
			return session.NewDocumentParser(session.Config{
				Model:   settings.Model,
				APIKey:  settings.APIKey,
				BaseURL: settings.BaseURL,
				Sources: settings.Sources,
			})
		},
		ModelOf: docModelOf(&settings),
		Account: accountBilledUsage,
	})
	if err != nil {
		// A PLAIN FILE IS NOT A FAILURE and it is the one refusal this door
		// answers itself, because the road has already said what the file is:
		// print it the way read would, and leave with 0.
		if errors.Is(err, session.ErrPlainDocument) {
			return printPlainFile(positionals[0])
		}
		return err
	}
	// The one line above the text says which rung read it — the billed ones,
	// because that is the provenance a person acts on; the local rung is the
	// file itself and says nothing. A question asked of a rung that cannot
	// hear one is said, exactly as the tool says it.
	if answer.Rung != "local" {
		line := "[doc — " + answer.Rung + " rung]"
		if *question != "" && answer.Rung != "native" {
			line += " the question shapes only the native rung; this is the full extracted text"
		}
		fmt.Println(line)
	}
	fmt.Print(strings.TrimLeft(answer.Text, "\n"))
	if !strings.HasSuffix(answer.Text, "\n") {
		fmt.Println()
	}
	return nil
}

// docModelOf answers which model a billed rung sends the file to. A PDF, a
// docx and a spreadsheet are read by this profile's model; a PICTURE is read
// by a model that can see — the profile's model when the catalog says it
// looks, otherwise the looking slot. That is the belt's [documentModel] law,
// and the catalog and the roles source are built LAZILY inside the hook so a
// plain file — the commonest thing a shell points this command at — resolves
// neither.
func docModelOf(settings *config.Config) func(kind string) string {
	var once sync.Once
	var resolver func(string) string
	var sees func(string) bool
	prepare := func() {
		models := catalog.LoadLazy(context.Background(), catalog.Options{
			BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: settings.ProfileDir,
		})
		source, err := v3RolesSource(mustWorkspace(), settings.ProfileDir)
		if err != nil {
			// The same posture the chat's governance keeps — a malformed pins
			// row is worth stopping for — reached from a door that cannot stop
			// the launch. The resolver degrades to the catalog and the
			// curated fallback, and the refusal a failed rung then gives says
			// which rung broke.
			source = nil
		}
		resolver = v3MediaModel(models, settings.ProfileDir, source)
		sees = v3SeesImages(models)
	}
	return func(kind string) string {
		model := settings.Model
		if kind != "image" {
			return model
		}
		once.Do(prepare)
		if sees(model) {
			return model
		}
		if seer := strings.TrimSpace(resolver("vision")); seer != "" {
			return seer
		}
		return model
	}
}

// parsePageRange reads the --pages figure: A-B, one-based and inclusive, or a
// single page. Zero means the whole document.
func parsePageRange(asked string) (from, to int, err error) {
	asked = strings.TrimSpace(asked)
	if asked == "" {
		return 0, 0, nil
	}
	first, last, found := strings.Cut(asked, "-")
	from, err = strconv.Atoi(strings.TrimSpace(first))
	if err != nil || from < 1 {
		return 0, 0, fmt.Errorf("--pages wants A-B with A a page number, got %q", asked)
	}
	if !found {
		return from, from, nil
	}
	to, err = strconv.Atoi(strings.TrimSpace(last))
	if err != nil || to < from {
		return 0, 0, fmt.Errorf("--pages wants A-B with B at or after A, got %q", asked)
	}
	return from, to, nil
}

// printPlainFile streams a plain file the way read prints it: bytes out, no
// call, no bill. It is this door's own answer to [session.ErrPlainDocument],
// which on the belt points the model at read instead.
func printPlainFile(name string) error {
	raw, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(raw)
	return err
}

// mustWorkspace is the folder this command was run in, the figure every path
// on the belt resolves against (tools_pdf.go's resolveInWorkspace).
func mustWorkspace() string {
	here, err := os.Getwd()
	if err != nil {
		return "."
	}
	return here
}
